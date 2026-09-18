package recommend

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"testing"

	"github.com/pseud039/termix/internal/lastfm"
	"github.com/pseud039/termix/internal/queue"
)

type fakeSimilar struct {
	tracks []lastfm.Track
	err    error
}

func (f fakeSimilar) Recommend(context.Context, string, string, int) ([]lastfm.Track, error) {
	return f.tracks, f.err
}

// fakeSearcher returns one item per query it knows and logs every query.
type fakeSearcher struct {
	src     queue.SourceType
	results map[string][]queue.Item // query → results
	calls   *[]string
}

func (f fakeSearcher) Search(_ context.Context, q string) ([]queue.Item, error) {
	*f.calls = append(*f.calls, f.src.String()+":"+q)
	if f.results == nil {
		return nil, errors.New("source down")
	}
	return f.results[q], nil
}

func item(src queue.SourceType, artist, title string) queue.Item {
	return queue.Item{ID: src.String() + "-" + title, Artist: artist, Title: title, Source: src}
}

func newRecommender(sim Similar, searchers map[queue.SourceType]Searcher) *Recommender {
	r := New(sim, searchers)
	r.rng = rand.New(rand.NewSource(1))
	return r
}

func TestForSeedResolvesOnSeedSourceFirst(t *testing.T) {
	var calls []string
	sp := fakeSearcher{src: queue.SourceSpotify, calls: &calls, results: map[string][]queue.Item{
		"B Song B": {item(queue.SourceSpotify, "B", "Song B (Remastered)")},
	}}
	yt := fakeSearcher{src: queue.SourceYouTube, calls: &calls, results: map[string][]queue.Item{
		"B Song B": {item(queue.SourceYouTube, "B", "Song B")},
	}}
	r := newRecommender(
		fakeSimilar{tracks: []lastfm.Track{{Artist: "B", Title: "Song B"}}},
		map[queue.SourceType]Searcher{queue.SourceSpotify: sp, queue.SourceYouTube: yt},
	)

	got, err := r.ForSeed(context.Background(), item(queue.SourceYouTube, "A", "Song A"), nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Source != queue.SourceYouTube {
		t.Fatalf("got %+v", got)
	}
	if !got[0].Recommended {
		t.Error("Recommended flag not set")
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "youtube:") {
		t.Errorf("seed source should be tried first and suffice: %v", calls)
	}
}

func TestForSeedFallsBackToOtherSourcesAndMatchesLoosely(t *testing.T) {
	var calls []string
	sp := fakeSearcher{src: queue.SourceSpotify, calls: &calls, results: map[string][]queue.Item{
		"B Song B": {item(queue.SourceSpotify, "Someone", "Unrelated"), item(queue.SourceSpotify, "B", "Song B (Official Video)")},
	}}
	yt := fakeSearcher{src: queue.SourceYouTube, calls: &calls, results: map[string][]queue.Item{}}
	r := newRecommender(
		fakeSimilar{tracks: []lastfm.Track{{Artist: "B", Title: "Song B"}}},
		map[queue.SourceType]Searcher{queue.SourceSpotify: sp, queue.SourceYouTube: yt},
	)

	got, err := r.ForSeed(context.Background(), item(queue.SourceYouTube, "A", "Song A"), nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "spotify-Song B (Official Video)" {
		t.Fatalf("got %+v", got)
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "youtube:") || !strings.HasPrefix(calls[1], "spotify:") {
		t.Errorf("calls = %v", calls)
	}
}

func TestForSeedExcludesAndCaps(t *testing.T) {
	var calls []string
	results := map[string][]queue.Item{}
	var tracks []lastfm.Track
	for _, name := range []string{"S1", "S2", "S3", "S4", "S5"} {
		tracks = append(tracks, lastfm.Track{Artist: "X", Title: name})
		results["X "+name] = []queue.Item{item(queue.SourceSpotify, "X", name)}
	}
	// Duplicate name from Last.fm should be ignored too.
	tracks = append(tracks, lastfm.Track{Artist: "x", Title: "s1"})
	sp := fakeSearcher{src: queue.SourceSpotify, calls: &calls, results: results}
	r := newRecommender(fakeSimilar{tracks: tracks}, map[queue.SourceType]Searcher{queue.SourceSpotify: sp})

	exclude := map[string]bool{Key("X", "S1"): true, Key("x ", " s2"): true}
	got, err := r.ForSeed(context.Background(), item(queue.SourceSpotify, "A", "Song A"), exclude, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 items, got %+v", got)
	}
	for _, it := range got {
		if it.Title == "S1" || it.Title == "S2" {
			t.Errorf("excluded track returned: %+v", it)
		}
	}
	if len(calls) != 2 {
		t.Errorf("should stop searching once n resolved: %v", calls)
	}
}

func TestForSeedLocalAcceptsFirstHitAndSkipsBrokenSources(t *testing.T) {
	var calls []string
	down := fakeSearcher{src: queue.SourceSpotify, calls: &calls} // errors
	local := fakeSearcher{src: queue.SourceLocal, calls: &calls, results: map[string][]queue.Item{
		"B Song B": {item(queue.SourceLocal, "b", "song b remastered mix")},
	}}
	r := newRecommender(
		fakeSimilar{tracks: []lastfm.Track{{Artist: "B", Title: "Song B"}}},
		map[queue.SourceType]Searcher{queue.SourceSpotify: down, queue.SourceLocal: local},
	)
	got, err := r.ForSeed(context.Background(), item(queue.SourceSpotify, "A", "Song A"), nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Source != queue.SourceLocal {
		t.Fatalf("got %+v", got)
	}
}

func TestForSeedNothingPlayableAndErrors(t *testing.T) {
	var calls []string
	sp := fakeSearcher{src: queue.SourceSpotify, calls: &calls, results: map[string][]queue.Item{}}
	r := newRecommender(
		fakeSimilar{tracks: []lastfm.Track{{Artist: "B", Title: "Song B"}}},
		map[queue.SourceType]Searcher{queue.SourceSpotify: sp},
	)
	got, err := r.ForSeed(context.Background(), item(queue.SourceSpotify, "A", "Song A"), nil, 2)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, err %v", got, err)
	}

	r = newRecommender(fakeSimilar{err: errors.New("boom")}, map[queue.SourceType]Searcher{queue.SourceSpotify: sp})
	if _, err := r.ForSeed(context.Background(), item(queue.SourceSpotify, "A", "Song A"), nil, 2); err == nil {
		t.Fatal("expected Last.fm error to propagate")
	}
}

func TestKey(t *testing.T) {
	if Key("  The   Band ", "Song\tName") != "the band|song name" {
		t.Errorf("Key = %q", Key("  The   Band ", "Song\tName"))
	}
}
