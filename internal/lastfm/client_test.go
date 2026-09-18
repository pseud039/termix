package lastfm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeServer answers each Last.fm method with a canned body and records
// the order methods were called in.
func fakeServer(t *testing.T, bodies map[string]string) (*Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Query().Get("method")
		calls = append(calls, method)
		if r.URL.Query().Get("api_key") != "k" {
			t.Errorf("missing api_key in %s", r.URL.RawQuery)
		}
		body, ok := bodies[method]
		if !ok {
			t.Errorf("unexpected method %q", method)
			http.Error(w, `{"error":10,"message":"Invalid API key"}`, http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newClient("k", srv.URL), &calls
}

const similarBody = `{"similartracks":{"track":[
  {"name":"Song B","match":0.9,"artist":{"name":"Artist B"}},
  {"name":"Song C","match":"0.5","artist":{"name":"Artist C"}}
]}}`

func TestRecommendUsesSimilarTracksFirst(t *testing.T) {
	c, calls := fakeServer(t, map[string]string{"track.getsimilar": similarBody})
	got, err := c.Recommend(context.Background(), "Artist A", "Song A", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Artist != "Artist B" || got[0].Title != "Song B" || got[0].Match != 0.9 {
		t.Fatalf("got %+v", got)
	}
	if got[1].Match != 0.5 {
		t.Errorf("string match not parsed: %+v", got[1])
	}
	if len(*calls) != 1 {
		t.Errorf("expected one call, got %v", *calls)
	}
}

func TestRecommendFallsBackToSearchThenSimilar(t *testing.T) {
	first := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("method") {
		case "track.getsimilar":
			if first {
				first = false
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":6,"message":"Track not found"}`))
				return
			}
			if q.Get("artist") != "Canonical Artist" || q.Get("track") != "Canonical Song" {
				t.Errorf("second getsimilar used %q / %q", q.Get("artist"), q.Get("track"))
			}
			w.Write([]byte(similarBody))
		case "track.search":
			w.Write([]byte(`{"results":{"trackmatches":{"track":[{"name":"Canonical Song","artist":"Canonical Artist"}]}}}`))
		default:
			t.Errorf("unexpected method %s", q.Get("method"))
		}
	}))
	defer srv.Close()
	c := newClient("k", srv.URL)

	got, err := c.Recommend(context.Background(), "Artist A - Topic", "Song A", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestRecommendFallsBackToSimilarArtists(t *testing.T) {
	c, calls := fakeServer(t, map[string]string{
		"track.getsimilar":    `{"similartracks":{"track":[]}}`,
		"track.search":        `{"results":{"trackmatches":{"track":[]}}}`,
		"artist.getsimilar":   `{"similarartists":{"artist":[{"name":"Artist X","match":"0.7"},{"name":"Artist Y","match":"0.6"}]}}`,
		"artist.gettoptracks": `{"toptracks":{"track":[{"name":"Hit 1","artist":{"name":"Whoever"}},{"name":"Hit 2","artist":{"name":""}}]}}`,
	})
	got, err := c.Recommend(context.Background(), "Artist A", "Song A", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("limit not honoured: %+v", got)
	}
	if got[0].Artist != "Whoever" || got[0].Title != "Hit 1" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Artist != "Artist X" {
		t.Errorf("missing artist name should fall back to the similar artist, got %+v", got[1])
	}
	want := []string{"track.getsimilar", "track.search", "artist.getsimilar", "artist.gettoptracks", "artist.gettoptracks"}
	if len(*calls) != len(want) {
		t.Fatalf("calls = %v", *calls)
	}
	for i := range want {
		if (*calls)[i] != want[i] {
			t.Fatalf("calls = %v, want %v", *calls, want)
		}
	}
}

func TestRecommendReportsRealErrors(t *testing.T) {
	c, _ := fakeServer(t, map[string]string{
		"track.getsimilar": `{"error":10,"message":"Invalid API key"}`,
	})
	_, err := c.Recommend(context.Background(), "A", "B", 5)
	if err == nil || err.Error() != "last.fm: Invalid API key" {
		t.Fatalf("err = %v", err)
	}
}

func TestNewClientNeedsKey(t *testing.T) {
	if _, err := NewClient(""); err == nil {
		t.Fatal("expected error without key")
	}
	c, err := NewClient(" abc ")
	if err != nil || c.apiKey != "abc" {
		t.Fatalf("client = %+v, err = %v", c, err)
	}
}

func TestCleanArtist(t *testing.T) {
	cases := map[string]string{
		"Daft Punk - Topic":  "Daft Punk",
		"TaylorSwiftVEVO":    "TaylorSwift",
		"Foo Official":       "Foo",
		"Bar Official Music": "Bar",
		"  Plain Name ":      "Plain Name",
		"VEVO":               "VEVO", // never strip to nothing
		"Music":              "Music",
	}
	for in, want := range cases {
		if got := CleanArtist(in); got != want {
			t.Errorf("CleanArtist(%q) = %q, want %q", in, got, want)
		}
	}
}
