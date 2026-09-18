package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pseud039/termix/internal/queue"
)

const defaultBase = "https://lrclib.net/api"

// userAgent identifies Termix to lrclib, which asks clients to do so.
const userAgent = "termix (https://github.com/pseud039/termix)"

// record is one lrclib result, from both /get and /search.
type record struct {
	ID           int     `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// Client looks lyrics up on lrclib.net.
type Client struct {
	http  *http.Client
	base  string
	cache *DiskCache

	mu      sync.Mutex
	missing map[string]struct{} // keys lrclib had nothing for this session
}

// NewClient returns a client backed by lrclib.net. cache may be nil.
func NewClient(cache *DiskCache) *Client {
	return newClient(defaultBase, cache)
}

func newClient(base string, cache *DiskCache) *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		base:    strings.TrimRight(base, "/"),
		cache:   cache,
		missing: map[string]struct{}{},
	}
}

// Fetch returns lyrics for item. It checks the cache, then asks lrclib for
// an exact match (artist, title, album, length) and falls back to a search.
// ErrNotFound means lrclib has nothing; the miss is remembered for the
// session so replaying the track doesn't ask again.
func (c *Client) Fetch(ctx context.Context, item queue.Item) (*Lyrics, error) {
	artist, title := cleanNames(item)
	if title == "" {
		return nil, ErrNotFound
	}
	key := cacheKey(artist, title, item.Duration)

	if l, ok := c.cache.Get(key); ok {
		return l, nil
	}
	c.mu.Lock()
	_, miss := c.missing[key]
	c.mu.Unlock()
	if miss {
		return nil, ErrNotFound
	}

	rec, err := c.lookup(ctx, artist, title, item)
	var l *Lyrics
	if err == nil {
		l = fromRecord(rec)
		if l == nil {
			err = ErrNotFound
		}
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.mu.Lock()
			c.missing[key] = struct{}{}
			c.mu.Unlock()
		}
		return nil, err
	}
	_ = c.cache.Put(key, l) // best effort
	return l, nil
}

// lookup tries /get first, then /search. A nil record with a nil error
// never happens: no result is ErrNotFound.
func (c *Client) lookup(ctx context.Context, artist, title string, item queue.Item) (*record, error) {
	params := url.Values{
		"artist_name": {artist},
		"track_name":  {title},
	}
	if item.Album != "" {
		params.Set("album_name", item.Album)
	}
	if item.Duration > 0 {
		params.Set("duration", strconv.Itoa(int(math.Round(item.Duration.Seconds()))))
	}
	var rec record
	found, err := c.call(ctx, "/get", params, &rec)
	if err != nil {
		return nil, err
	}
	if found {
		return &rec, nil
	}

	var results []record
	if _, err := c.call(ctx, "/search", url.Values{
		"track_name":  {title},
		"artist_name": {artist},
	}, &results); err != nil {
		return nil, err
	}
	if len(results) == 0 && item.Source == queue.SourceYouTube {
		// The channel name is often not the artist; let lrclib match the
		// title alone.
		if _, err := c.call(ctx, "/search", url.Values{"q": {title}}, &results); err != nil {
			return nil, err
		}
	}
	if best := pickBest(results, item.Duration); best != nil {
		return best, nil
	}
	return nil, ErrNotFound
}

// pickBest prefers synced results and, when the track length is known,
// the one closest to it (within 10s). Nil when results is empty.
func pickBest(results []record, dur time.Duration) *record {
	var best *record
	bestScore := math.Inf(1)
	for i := range results {
		r := &results[i]
		if r.PlainLyrics == "" && r.SyncedLyrics == "" && !r.Instrumental {
			continue
		}
		score := 0.0
		if r.SyncedLyrics == "" {
			score += 100 // any synced result beats any plain one
		}
		if dur > 0 {
			diff := math.Abs(r.Duration - dur.Seconds())
			if diff > 10 {
				score += 50
			}
			score += diff
		}
		if score < bestScore {
			best, bestScore = r, score
		}
	}
	return best
}

func fromRecord(r *record) *Lyrics {
	l := &Lyrics{
		Plain:        strings.TrimSpace(r.PlainLyrics),
		Instrumental: r.Instrumental,
	}
	if r.SyncedLyrics != "" {
		l.Synced = ParseLRC(r.SyncedLyrics)
	}
	if l.Plain == "" && len(l.Synced) == 0 && !l.Instrumental {
		return nil
	}
	return l
}

// call performs one GET and decodes the body into v. It reports false
// (and no error) on a 404, which lrclib uses for "no such track".
func (c *Client) call(ctx context.Context, path string, params url.Values, v any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+params.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("lrclib: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("lrclib: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return false, fmt.Errorf("lrclib: %w", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return false, fmt.Errorf("lrclib: parsing response: %w", err)
	}
	return true, nil
}

// cacheKey normalises the lookup inputs so the same song from different
// sources shares an entry. The length is part of the key because a live
// version and the album cut have different timings.
func cacheKey(artist, title string, dur time.Duration) string {
	norm := func(s string) string {
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}
	return norm(artist) + "|" + norm(title) + "|" + strconv.Itoa(int(math.Round(dur.Seconds())))
}

// titleNoise matches decorations YouTube uploads carry that aren't part of
// the song title: "(Official Video)", "[Lyrics]", "| Some Channel", etc.
var titleNoise = regexp.MustCompile(`(?i)\s*(\((official|lyric|lyrics|audio|video|visuali[sz]er|hd|hq|4k|live|remaster(ed)?)[^)]*\)|\[(official|lyric|lyrics|audio|video|visuali[sz]er|hd|hq|4k)[^\]]*\]|\|.*$|\bofficial (music )?video\b|\bofficial audio\b|\blyric video\b|\blyrics\b)\s*`)

// artistNoise lists channel-name suffixes to strip, checked
// case-insensitively and repeatedly. It duplicates lastfm.CleanArtist's
// list on purpose so this package doesn't depend on the Last.fm one.
var artistNoise = []string{" - topic", "vevo", " official", " music"}

// cleanNames returns the artist/title to look up. YouTube titles are often
// "Artist - Song (Official Video)" with the channel as the artist, so those
// are split and stripped of decorations; other sources are used as is.
func cleanNames(item queue.Item) (artist, title string) {
	artist, title = strings.TrimSpace(item.Artist), strings.TrimSpace(item.Title)
	if item.Source != queue.SourceYouTube {
		return artist, title
	}
	if a, t, ok := strings.Cut(title, " - "); ok && strings.TrimSpace(a) != "" && strings.TrimSpace(t) != "" {
		artist, title = strings.TrimSpace(a), strings.TrimSpace(t)
	}
	title = strings.TrimSpace(titleNoise.ReplaceAllString(title, " "))
	title = strings.Join(strings.Fields(title), " ")
	return cleanArtist(artist), title
}

func cleanArtist(s string) string {
	s = strings.TrimSpace(s)
	for {
		trimmed := s
		lower := strings.ToLower(s)
		for _, suffix := range artistNoise {
			if strings.HasSuffix(lower, suffix) && len(s) > len(suffix) {
				trimmed = strings.TrimSpace(s[:len(s)-len(suffix)])
				break
			}
		}
		if trimmed == s {
			return s
		}
		s = trimmed
	}
}
