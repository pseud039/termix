// Package recommend turns "tracks similar to this one" (names from
// Last.fm) into playable queue.Items by looking each one up through the
// same search providers the Search tab uses.
package recommend

import (
	"context"
	"math/rand"
	"strings"
	"time"

	"github.com/pseud039/termix/internal/lastfm"
	"github.com/pseud039/termix/internal/queue"
)

// Searcher is the search provider shape (Spotify, YouTube, local). It
// matches app.Searcher; it is declared here so app can import this package.
type Searcher interface {
	Search(ctx context.Context, q string) ([]queue.Item, error)
}

// Similar yields tracks similar to the given one. *lastfm.Client
// satisfies it; tests use a fake.
type Similar interface {
	Recommend(ctx context.Context, artist, title string, limit int) ([]lastfm.Track, error)
}

// candidateLimit is how many names to ask Last.fm for; only a few of
// them get resolved, the rest are spares for ones that don't match.
const candidateLimit = 25

// resolveOrder is the fallback order after the seed's own source.
var resolveOrder = []queue.SourceType{queue.SourceSpotify, queue.SourceYouTube, queue.SourceLocal}

// Recommender finds similar tracks and resolves them to playable items.
type Recommender struct {
	similar   Similar
	searchers map[queue.SourceType]Searcher
	rng       *rand.Rand
}

func New(similar Similar, searchers map[queue.SourceType]Searcher) *Recommender {
	return &Recommender{
		similar:   similar,
		searchers: searchers,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Key normalises an artist/title pair so the same song from different
// sources dedupes: lower-cased, whitespace collapsed, joined with "|".
func Key(artist, title string) string {
	norm := func(s string) string {
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}
	return norm(artist) + "|" + norm(title)
}

// ForSeed returns up to n playable items similar to seed, skipping any
// whose Key is in exclude (typically everything already queued). Each
// name is looked up on the seed's source first, then the other available
// sources, so playback stays on the backend already in use. Lookups run
// one at a time because yt-dlp is slow and heavy.
//
// A nil, nil result means Last.fm had suggestions but none were playable
// (or it had none at all).
func (r *Recommender) ForSeed(ctx context.Context, seed queue.Item, exclude map[string]bool, n int) ([]queue.Item, error) {
	if n <= 0 {
		return nil, nil
	}
	cands, err := r.similar.Recommend(ctx, seed.Artist, seed.Title, candidateLimit)
	if err != nil {
		return nil, err
	}

	// Drop excluded and duplicate names, then shuffle so repeated fetches
	// on the same seed don't always inject the same top matches.
	seen := map[string]bool{}
	filtered := cands[:0]
	for _, c := range cands {
		k := Key(c.Artist, c.Title)
		if exclude[k] || seen[k] {
			continue
		}
		seen[k] = true
		filtered = append(filtered, c)
	}
	r.rng.Shuffle(len(filtered), func(i, j int) { filtered[i], filtered[j] = filtered[j], filtered[i] })

	order := r.sourceOrder(seed.Source)
	var out []queue.Item
	for _, c := range filtered {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		item, ok := r.resolve(ctx, c, order)
		if !ok {
			continue
		}
		item.Recommended = true
		out = append(out, item)
		if len(out) >= n {
			break
		}
	}
	return out, nil
}

// sourceOrder puts the seed's source first, then the rest that exist.
func (r *Recommender) sourceOrder(first queue.SourceType) []queue.SourceType {
	var order []queue.SourceType
	if _, ok := r.searchers[first]; ok {
		order = append(order, first)
	}
	for _, s := range resolveOrder {
		if _, ok := r.searchers[s]; ok && s != first {
			order = append(order, s)
		}
	}
	return order
}

// resolve searches each source in order for the track and returns the
// first result that looks like it.
func (r *Recommender) resolve(ctx context.Context, c lastfm.Track, order []queue.SourceType) (queue.Item, bool) {
	query := strings.TrimSpace(c.Artist + " " + c.Title)
	for _, src := range order {
		items, err := r.searchers[src].Search(ctx, query)
		if err != nil {
			continue // a broken source shouldn't block the others
		}
		for _, it := range items {
			if src == queue.SourceLocal || titleMatches(it.Title, c.Title) {
				return it, true
			}
		}
	}
	return queue.Item{}, false
}

// titleMatches is deliberately loose: search results carry suffixes like
// "(Official Video)" and Last.fm titles sometimes carry "(Remastered)".
func titleMatches(got, want string) bool {
	g := strings.ToLower(strings.TrimSpace(got))
	w := strings.ToLower(strings.TrimSpace(want))
	if g == "" || w == "" {
		return false
	}
	return strings.Contains(g, w) || strings.Contains(w, g)
}
