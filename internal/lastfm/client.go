// Package lastfm asks the Last.fm web API which tracks are similar to a
// given one. It powers smart shuffle: Last.fm's scrobble data does the
// "what sounds like this" part, so Termix needs no model of its own.
package lastfm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBase = "https://ws.audioscrobbler.com/2.0/"

// errTrackNotFound is Last.fm's error code for an unknown track or artist.
// It means "no data", not "request failed", so callers fall through to the
// next lookup instead of giving up.
const errTrackNotFound = 6

// Track is one recommendation: just names, not yet playable. The
// recommend package turns it into a queue.Item through a search provider.
type Track struct {
	Artist string
	Title  string
	Match  float64 // 0..1 similarity as reported by Last.fm; 0 when unknown
}

// Client talks to the Last.fm API with a single API key.
type Client struct {
	apiKey string
	http   *http.Client
	base   string
}

// NewClient takes the Last.fm API key from config.toml. Without a key smart
// shuffle is unavailable, so the caller shows a hint instead.
func NewClient(key string) (*Client, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("lastfm.api_key not set in config.toml")
	}
	return newClient(key, defaultBase), nil
}

func newClient(key, base string) *Client {
	return &Client{
		apiKey: key,
		http:   &http.Client{Timeout: 15 * time.Second},
		base:   base,
	}
}

// Recommend returns up to limit tracks similar to artist/title. It tries,
// in order, until one gives results:
//  1. track.getSimilar on the names as given (autocorrected by Last.fm)
//  2. track.search to find the canonical names, then track.getSimilar again
//  3. artist.getSimilar, then the top tracks of each similar artist
//
// An empty result with a nil error means Last.fm knows nothing useful.
func (c *Client) Recommend(ctx context.Context, artist, title string, limit int) ([]Track, error) {
	artist = CleanArtist(artist)
	if limit <= 0 {
		limit = 20
	}

	tracks, err := c.similarTracks(ctx, artist, title, limit)
	if err != nil || len(tracks) > 0 {
		return tracks, err
	}

	if a, t, ok, err := c.searchTrack(ctx, artist, title); err != nil {
		return nil, err
	} else if ok && (a != artist || t != title) {
		tracks, err = c.similarTracks(ctx, a, t, limit)
		if err != nil || len(tracks) > 0 {
			return tracks, err
		}
		artist = a
	}

	return c.similarArtistTracks(ctx, artist, limit)
}

func (c *Client) similarTracks(ctx context.Context, artist, title string, limit int) ([]Track, error) {
	var resp struct {
		SimilarTracks struct {
			Track []struct {
				Name   string `json:"name"`
				Match  any    `json:"match"`
				Artist struct {
					Name string `json:"name"`
				} `json:"artist"`
			} `json:"track"`
		} `json:"similartracks"`
	}
	err := c.call(ctx, url.Values{
		"method":      {"track.getsimilar"},
		"artist":      {artist},
		"track":       {title},
		"autocorrect": {"1"},
		"limit":       {strconv.Itoa(limit)},
	}, &resp)
	if err != nil {
		return nil, err
	}
	out := make([]Track, 0, len(resp.SimilarTracks.Track))
	for _, t := range resp.SimilarTracks.Track {
		if t.Name == "" || t.Artist.Name == "" {
			continue
		}
		out = append(out, Track{Artist: t.Artist.Name, Title: t.Name, Match: toFloat(t.Match)})
	}
	return out, nil
}

// searchTrack returns the canonical artist/title of the best text match.
func (c *Client) searchTrack(ctx context.Context, artist, title string) (string, string, bool, error) {
	var resp struct {
		Results struct {
			TrackMatches struct {
				Track []struct {
					Name   string `json:"name"`
					Artist string `json:"artist"` // plain string here, unlike getSimilar
				} `json:"track"`
			} `json:"trackmatches"`
		} `json:"results"`
	}
	err := c.call(ctx, url.Values{
		"method": {"track.search"},
		"track":  {strings.TrimSpace(artist + " " + title)},
		"limit":  {"1"},
	}, &resp)
	if err != nil {
		return "", "", false, err
	}
	hits := resp.Results.TrackMatches.Track
	if len(hits) == 0 || hits[0].Name == "" || hits[0].Artist == "" {
		return "", "", false, nil
	}
	return hits[0].Artist, hits[0].Name, true, nil
}

// similarArtistTracks is the last resort: top tracks of similar artists.
func (c *Client) similarArtistTracks(ctx context.Context, artist string, limit int) ([]Track, error) {
	var similar struct {
		SimilarArtists struct {
			Artist []struct {
				Name  string `json:"name"`
				Match any    `json:"match"`
			} `json:"artist"`
		} `json:"similarartists"`
	}
	err := c.call(ctx, url.Values{
		"method":      {"artist.getsimilar"},
		"artist":      {artist},
		"autocorrect": {"1"},
		"limit":       {"5"},
	}, &similar)
	if err != nil {
		return nil, err
	}

	var out []Track
	for _, a := range similar.SimilarArtists.Artist {
		if a.Name == "" {
			continue
		}
		var top struct {
			TopTracks struct {
				Track []struct {
					Name   string `json:"name"`
					Artist struct {
						Name string `json:"name"`
					} `json:"artist"`
				} `json:"track"`
			} `json:"toptracks"`
		}
		err := c.call(ctx, url.Values{
			"method":      {"artist.gettoptracks"},
			"artist":      {a.Name},
			"autocorrect": {"1"},
			"limit":       {"5"},
		}, &top)
		if err != nil {
			return nil, err
		}
		for _, t := range top.TopTracks.Track {
			if t.Name == "" {
				continue
			}
			name := t.Artist.Name
			if name == "" {
				name = a.Name
			}
			out = append(out, Track{Artist: name, Title: t.Name, Match: toFloat(a.Match)})
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// call performs one API request and decodes the JSON body into v. A
// "not found" error from Last.fm leaves v untouched and returns nil so the
// caller sees an empty result.
func (c *Client) call(ctx context.Context, params url.Values, v any) error {
	params.Set("api_key", c.apiKey)
	params.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "termix (https://github.com/pseud039/termix)")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("last.fm: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("last.fm: %w", err)
	}

	// Errors come back as {"error":N,"message":"..."} with a 4xx status,
	// so check the body before the status code.
	var apiErr struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != 0 {
		if apiErr.Error == errTrackNotFound {
			return nil
		}
		return fmt.Errorf("last.fm: %s", apiErr.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("last.fm: HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("last.fm: parsing response: %w", err)
	}
	return nil
}

// toFloat reads Last.fm's "match" field, which is sometimes a number and
// sometimes a string.
func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	}
	return 0
}

// artistNoise lists suffixes YouTube channels carry that Last.fm doesn't
// know about, checked case-insensitively and repeatedly.
var artistNoise = []string{" - topic", "vevo", " official", " music"}

// CleanArtist strips YouTube channel decoration ("Artist - Topic",
// "ArtistVEVO", "Artist Official") so the name matches Last.fm's.
func CleanArtist(s string) string {
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
