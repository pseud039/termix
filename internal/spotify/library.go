package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	zspotify "github.com/zmb3/spotify/v2"

	"github.com/pseud039/termix/internal/queue"
)

// Playlist is one entry in the Library tab: the user's Liked Songs or a
// playlist they own or follow.
type Playlist struct {
	ID    string // Spotify playlist ID; empty for Liked Songs
	Name  string
	Owner string // display name of the owner, or "you"
	Total int    // number of tracks Spotify reports
	Liked bool   // the special Liked Songs collection
}

// Key identifies the playlist in caches. Liked Songs has no ID.
func (p Playlist) Key() string {
	if p.Liked {
		return "liked"
	}
	return p.ID
}

// TrackPage is one page of a playlist's tracks.
type TrackPage struct {
	Items []queue.Item
	Total int  // number of entries Spotify reports (before skipping unplayable ones)
	Next  int  // offset to fetch next; meaningful only when Done is false
	Done  bool // no more pages after this one
}

// Library reads Liked Songs and playlists from the user's Spotify account.
// It needs the user-library-read and playlist-read-* scopes; a token issued
// without them makes every call fail with a 403 (see IsScopeError).
type Library struct {
	client *zspotify.Client

	// Page size. Development Mode apps reject limits above 10 with a 400
	// "Invalid limit" (search hit the same cap), so the first such error
	// drops it from defaultPageSize to smallPageSize for the session.
	mu    sync.Mutex
	limit int
}

const (
	defaultPageSize = 50
	smallPageSize   = 10
)

func NewLibrary(client *zspotify.Client) *Library {
	return &Library{client: client, limit: defaultPageSize}
}

func (l *Library) pageSize() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limit
}

// shrinkPageSize reacts to an "Invalid limit" error by switching to the
// small page size. It reports whether a retry is worth it, which is only
// the case when the size actually changed.
func (l *Library) shrinkPageSize(err error) bool {
	if !isLimitError(err) {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limit == smallPageSize {
		return false
	}
	l.limit = smallPageSize
	return true
}

func isLimitError(err error) bool {
	var zErr zspotify.Error
	return errors.As(err, &zErr) && zErr.Status == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(zErr.Message), "limit")
}

// IsScopeError reports whether err is Spotify refusing the call because
// the token lacks the needed scopes (403). The fix is running `termix auth`
// again so the new scopes are granted.
func IsScopeError(err error) bool {
	var zErr zspotify.Error
	return errors.As(err, &zErr) && zErr.Status == http.StatusForbidden
}

// IsForbiddenPlaylist reports whether err is the 404 Spotify returns when
// an app in Development Mode reads a Spotify-made playlist (Discover
// Weekly, Release Radar, editorial mixes), which has been blocked since
// November 2024.
func IsForbiddenPlaylist(err error) bool {
	var zErr zspotify.Error
	return errors.As(err, &zErr) && zErr.Status == http.StatusNotFound
}

// Playlists lists Liked Songs first, then every playlist in the user's
// library (owned and followed), across all pages.
func (l *Library) Playlists(ctx context.Context) ([]Playlist, error) {
	liked, err := l.likedTotal(ctx)
	if err != nil {
		return nil, err
	}
	out := []Playlist{{Name: "Liked Songs", Owner: "you", Total: liked, Liked: true}}

	// Best effort: knowing our own user ID lets the list say "you" instead
	// of repeating the account name on every playlist.
	me := ""
	if u, err := l.client.CurrentUser(ctx); err == nil && u != nil {
		me = u.ID
	}

	offset := 0
	for {
		page, err := l.playlistsPage(ctx, offset)
		if err != nil {
			return nil, err
		}
		for _, p := range page.Playlists {
			owner := p.Owner.DisplayName
			if owner == "" {
				owner = p.Owner.ID
			}
			if me != "" && p.Owner.ID == me {
				owner = "you"
			}
			out = append(out, Playlist{
				ID:    string(p.ID),
				Name:  p.Name,
				Owner: owner,
				Total: int(p.Tracks.Total),
			})
		}
		offset += len(page.Playlists)
		if len(page.Playlists) == 0 || offset >= int(page.Total) {
			return out, nil
		}
	}
}

func (l *Library) likedTotal(ctx context.Context) (int, error) {
	page, err := l.client.CurrentUsersTracks(ctx, zspotify.Limit(1))
	if err != nil {
		return 0, fmt.Errorf("listing liked songs: %w", err)
	}
	return int(page.Total), nil
}

func (l *Library) playlistsPage(ctx context.Context, offset int) (*zspotify.SimplePlaylistPage, error) {
	for {
		page, err := l.client.CurrentUsersPlaylists(ctx, zspotify.Limit(l.pageSize()), zspotify.Offset(offset))
		if err == nil {
			return page, nil
		}
		if !l.shrinkPageSize(err) {
			return nil, fmt.Errorf("listing playlists: %w", err)
		}
	}
}

// Tracks fetches one page of p's tracks starting at offset. Entries that
// can't be played (podcast episodes, tracks unavailable in the user's
// market, local files) are skipped, so a page may hold fewer items than
// the page size even when more remain; check Done, not len(Items).
func (l *Library) Tracks(ctx context.Context, p Playlist, offset int) (TrackPage, error) {
	for {
		var page TrackPage
		var err error
		if p.Liked {
			page, err = l.likedPage(ctx, offset)
		} else {
			page, err = l.playlistPage(ctx, p, offset)
		}
		if err == nil {
			return page, nil
		}
		if !l.shrinkPageSize(err) {
			return TrackPage{}, err
		}
	}
}

func (l *Library) likedPage(ctx context.Context, offset int) (TrackPage, error) {
	page, err := l.client.CurrentUsersTracks(ctx, zspotify.Limit(l.pageSize()), zspotify.Offset(offset))
	if err != nil {
		return TrackPage{}, fmt.Errorf("reading liked songs: %w", err)
	}
	out := TrackPage{Total: int(page.Total)}
	for i := range page.Tracks {
		t := &page.Tracks[i].FullTrack
		if t.ID == "" {
			continue
		}
		out.Items = append(out.Items, itemFromTrack(t))
	}
	return finishPage(out, offset, len(page.Tracks)), nil
}

func (l *Library) playlistPage(ctx context.Context, p Playlist, offset int) (TrackPage, error) {
	page, err := l.client.GetPlaylistItems(ctx, zspotify.ID(p.ID), zspotify.Limit(l.pageSize()), zspotify.Offset(offset))
	if err != nil {
		return TrackPage{}, fmt.Errorf("reading playlist %q: %w", p.Name, err)
	}
	out := TrackPage{Total: int(page.Total)}
	for _, it := range page.Items {
		t := it.Track.Track
		if t == nil || it.IsLocal || t.ID == "" {
			continue
		}
		out.Items = append(out.Items, itemFromTrack(t))
	}
	return finishPage(out, offset, len(page.Items)), nil
}

// finishPage fills in Next and Done from how many raw entries the page
// carried (including skipped ones), so paging stays aligned with Spotify's
// offsets.
func finishPage(page TrackPage, offset, raw int) TrackPage {
	page.Next = offset + raw
	page.Done = raw == 0 || page.Next >= page.Total
	return page
}
