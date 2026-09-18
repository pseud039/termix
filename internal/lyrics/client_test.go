package lyrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/pseud039/termix/internal/queue"
)

const syncedRecord = `{"id":1,"trackName":"Song","artistName":"Artist","albumName":"Album","duration":200,
"instrumental":false,"plainLyrics":"one\ntwo","syncedLyrics":"[00:01.00]one\n[00:02.00]two"}`

// fakeServer answers /get and /search with the given handler and records
// every request's path and query.
func fakeServer(t *testing.T, cache *DiskCache, handle func(w http.ResponseWriter, path string, q url.Values)) (*Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != userAgent {
			t.Errorf("missing User-Agent, got %q", r.Header.Get("User-Agent"))
		}
		calls = append(calls, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		handle(w, r.URL.Path, r.URL.Query())
	}))
	t.Cleanup(srv.Close)
	return newClient(srv.URL, cache), &calls
}

func notFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"statusCode":404,"name":"TrackNotFound","message":"Failed to find specified track"}`))
}

var spotifyItem = queue.Item{
	ID: "1", Title: "Song", Artist: "Artist", Album: "Album",
	Duration: 200 * time.Second, Source: queue.SourceSpotify,
}

func TestFetchExactHit(t *testing.T) {
	c, calls := fakeServer(t, nil, func(w http.ResponseWriter, path string, q url.Values) {
		if path != "/get" {
			t.Errorf("unexpected path %s", path)
		}
		if q.Get("artist_name") != "Artist" || q.Get("track_name") != "Song" || q.Get("album_name") != "Album" || q.Get("duration") != "200" {
			t.Errorf("bad query %v", q)
		}
		w.Write([]byte(syncedRecord))
	})
	l, err := c.Fetch(context.Background(), spotifyItem)
	if err != nil {
		t.Fatal(err)
	}
	if !l.IsSynced() || len(l.Synced) != 2 || l.Synced[1].Text != "two" || l.Plain != "one\ntwo" {
		t.Errorf("got %+v", l)
	}
	if len(*calls) != 1 {
		t.Errorf("expected one call, got %v", *calls)
	}
}

func TestFetchFallsBackToSearchAndPicksClosestSynced(t *testing.T) {
	c, calls := fakeServer(t, nil, func(w http.ResponseWriter, path string, q url.Values) {
		switch path {
		case "/get":
			notFound(w)
		case "/search":
			if q.Get("track_name") != "Song" || q.Get("artist_name") != "Artist" {
				t.Errorf("bad search query %v", q)
			}
			w.Write([]byte(`[
			  {"id":1,"duration":201,"plainLyrics":"plain only","syncedLyrics":""},
			  {"id":2,"duration":260,"plainLyrics":"far","syncedLyrics":"[00:01.00]far"},
			  {"id":3,"duration":203,"plainLyrics":"near","syncedLyrics":"[00:01.00]near"}
			]`))
		}
	})
	l, err := c.Fetch(context.Background(), spotifyItem)
	if err != nil {
		t.Fatal(err)
	}
	if l.Plain != "near" {
		t.Errorf("picked %q, want the synced result closest in length", l.Plain)
	}
	if len(*calls) != 2 {
		t.Errorf("expected get then search, got %v", *calls)
	}
}

func TestFetchNotFoundIsRememberedForSession(t *testing.T) {
	c, calls := fakeServer(t, nil, func(w http.ResponseWriter, path string, q url.Values) {
		if path == "/get" {
			notFound(w)
			return
		}
		w.Write([]byte(`[]`))
	})
	for i := 0; i < 2; i++ {
		_, err := c.Fetch(context.Background(), spotifyItem)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("fetch %d: got %v, want ErrNotFound", i, err)
		}
	}
	if len(*calls) != 2 {
		t.Errorf("second fetch should not hit the server, calls: %v", *calls)
	}
}

func TestFetchServerErrorIsAnError(t *testing.T) {
	c, _ := fakeServer(t, nil, func(w http.ResponseWriter, path string, q url.Values) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := c.Fetch(context.Background(), spotifyItem)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want a server error", err)
	}
}

func TestFetchCleansYouTubeNames(t *testing.T) {
	item := queue.Item{
		ID: "yt-1", Title: "Artist - Song (Official Video) [4K]", Artist: "ArtistVEVO",
		Duration: 200 * time.Second, Source: queue.SourceYouTube,
	}
	var searchQs []url.Values
	c, _ := fakeServer(t, nil, func(w http.ResponseWriter, path string, q url.Values) {
		switch path {
		case "/get":
			if q.Get("artist_name") != "Artist" || q.Get("track_name") != "Song" {
				t.Errorf("bad get query %v", q)
			}
			if q.Has("album_name") {
				t.Errorf("empty album should be omitted: %v", q)
			}
			notFound(w)
		case "/search":
			searchQs = append(searchQs, q)
			if q.Has("q") {
				w.Write([]byte(`[{"id":9,"duration":200,"plainLyrics":"found by title"}]`))
				return
			}
			w.Write([]byte(`[]`))
		}
	})
	l, err := c.Fetch(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	if l.Plain != "found by title" {
		t.Errorf("got %+v", l)
	}
	if len(searchQs) != 2 || searchQs[1].Get("q") != "Song" {
		t.Errorf("expected a title-only search retry, got %v", searchQs)
	}
}

func TestFetchUsesDiskCache(t *testing.T) {
	cache := NewDiskCache(t.TempDir())
	c, calls := fakeServer(t, cache, func(w http.ResponseWriter, path string, q url.Values) {
		w.Write([]byte(syncedRecord))
	})
	for i := 0; i < 2; i++ {
		l, err := c.Fetch(context.Background(), spotifyItem)
		if err != nil || !l.IsSynced() {
			t.Fatalf("fetch %d: %v %+v", i, err, l)
		}
	}
	if len(*calls) != 1 {
		t.Errorf("second fetch should come from the cache, calls: %v", *calls)
	}
	// A fresh client with the same cache dir doesn't hit the server at all.
	c2, calls2 := fakeServer(t, cache, func(w http.ResponseWriter, path string, q url.Values) {
		t.Error("server should not be called")
	})
	if _, err := c2.Fetch(context.Background(), spotifyItem); err != nil {
		t.Fatal(err)
	}
	if len(*calls2) != 0 {
		t.Errorf("unexpected calls %v", *calls2)
	}
}

func TestCleanNames(t *testing.T) {
	cases := []struct {
		item         queue.Item
		artist, want string
	}{
		{queue.Item{Source: queue.SourceYouTube, Artist: "Some Channel - Topic", Title: "Artist - Song (Official Music Video)"}, "Artist", "Song"},
		{queue.Item{Source: queue.SourceYouTube, Artist: "ArtistVEVO", Title: "Song | Lyric Video"}, "Artist", "Song"},
		{queue.Item{Source: queue.SourceYouTube, Artist: "Chan", Title: "Song [Official Audio] (Lyrics)"}, "Chan", "Song"},
		{queue.Item{Source: queue.SourceSpotify, Artist: "A - Topic", Title: "X - Y (Live)"}, "A - Topic", "X - Y (Live)"},
	}
	for _, c := range cases {
		a, tt := cleanNames(c.item)
		if a != c.artist || tt != c.want {
			t.Errorf("cleanNames(%q / %q) = %q / %q, want %q / %q", c.item.Artist, c.item.Title, a, tt, c.artist, c.want)
		}
	}
}
