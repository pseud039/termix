package spotify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	zspotify "github.com/zmb3/spotify/v2"
)

// fakeAPI is a tiny stand-in for api.spotify.com: a handler per path that
// gets the parsed limit and offset and returns the JSON body to send.
type fakeAPI struct {
	t        *testing.T
	routes   map[string]func(limit, offset int) (int, any)
	requests []string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests = append(f.requests, r.URL.Path+"?"+r.URL.RawQuery)
	h, ok := f.routes[r.URL.Path]
	if !ok {
		f.t.Errorf("unexpected request %s", r.URL)
		http.NotFound(w, r)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	status, body := h(limit, offset)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func newTestLibrary(t *testing.T, routes map[string]func(limit, offset int) (int, any)) (*Library, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{t: t, routes: routes}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	client := zspotify.New(srv.Client(), zspotify.WithBaseURL(srv.URL+"/"))
	return NewLibrary(client), api
}

func apiError(status int, msg string) (int, any) {
	return status, map[string]any{"error": map[string]any{"status": status, "message": msg}}
}

func track(id, name, artist string) map[string]any {
	return map[string]any{
		"type":        "track",
		"id":          id,
		"name":        name,
		"uri":         "spotify:track:" + id,
		"duration_ms": 1000,
		"artists":     []map[string]any{{"name": artist}},
		"album":       map[string]any{"name": "Album", "images": []any{}},
	}
}

func playlist(id, name, ownerID, ownerName string, total int) map[string]any {
	return map[string]any{
		"id":     id,
		"name":   name,
		"owner":  map[string]any{"id": ownerID, "display_name": ownerName},
		"tracks": map[string]any{"total": total},
	}
}

func TestPlaylistsLikedFirstAndPaged(t *testing.T) {
	lib, _ := newTestLibrary(t, map[string]func(limit, offset int) (int, any){
		"/me": func(int, int) (int, any) { return 200, map[string]any{"id": "me"} },
		"/me/tracks": func(int, int) (int, any) {
			return 200, map[string]any{"total": 123, "items": []any{}}
		},
		"/me/playlists": func(limit, offset int) (int, any) {
			all := []map[string]any{
				playlist("p1", "Gym", "me", "Me", 5),
				playlist("p2", "Indie Mix", "spotify", "Spotify", 50),
				playlist("p3", "Shared", "friend", "", 7),
			}
			end := offset + 2 // two per page regardless of limit
			if end > len(all) {
				end = len(all)
			}
			return 200, map[string]any{"total": len(all), "items": all[offset:end]}
		},
	})

	got, err := lib.Playlists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Playlist{
		{Name: "Liked Songs", Owner: "you", Total: 123, Liked: true},
		{ID: "p1", Name: "Gym", Owner: "you", Total: 5},
		{ID: "p2", Name: "Indie Mix", Owner: "Spotify", Total: 50},
		{ID: "p3", Name: "Shared", Owner: "friend", Total: 7},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d playlists, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("playlist %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[0].Key() != "liked" || got[1].Key() != "p1" {
		t.Errorf("keys = %q, %q", got[0].Key(), got[1].Key())
	}
}

func TestTracksSkipsUnplayableEntries(t *testing.T) {
	lib, _ := newTestLibrary(t, map[string]func(limit, offset int) (int, any){
		"/playlists/p1/tracks": func(limit, offset int) (int, any) {
			return 200, map[string]any{"total": 4, "items": []map[string]any{
				{"track": track("t1", "One", "A")},
				{"track": nil},                                        // unavailable in market
				{"is_local": true, "track": track("l", "Local", "B")}, // local file
				{"track": track("t2", "Two", "C")},
			}}
		},
	})

	page, err := lib.Tracks(context.Background(), Playlist{ID: "p1", Name: "P"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != "t1" || page.Items[1].ID != "t2" {
		t.Errorf("items = %+v, want t1 and t2", page.Items)
	}
	if page.Items[0].URI != "spotify:track:t1" || page.Items[0].Artist != "A" {
		t.Errorf("item = %+v", page.Items[0])
	}
	if !page.Done || page.Total != 4 || page.Next != 4 {
		t.Errorf("page = %+v, want Done with Total 4 and Next 4", page)
	}
}

func TestLikedTracksPageChain(t *testing.T) {
	lib, _ := newTestLibrary(t, map[string]func(limit, offset int) (int, any){
		"/me/tracks": func(limit, offset int) (int, any) {
			items := []map[string]any{}
			for i := offset; i < offset+limit && i < 3; i++ {
				id := "t" + strconv.Itoa(i)
				items = append(items, map[string]any{"track": track(id, id, "A")})
			}
			return 200, map[string]any{"total": 3, "items": items}
		},
	})
	lib.limit = 2

	liked := Playlist{Liked: true, Name: "Liked Songs"}
	first, err := lib.Tracks(context.Background(), liked, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Done || first.Next != 2 {
		t.Fatalf("first page = %+v", first)
	}
	second, err := lib.Tracks(context.Background(), liked, first.Next)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || !second.Done || second.Items[0].ID != "t2" {
		t.Fatalf("second page = %+v", second)
	}
}

func TestLimitFallsBackTo10(t *testing.T) {
	lib, api := newTestLibrary(t, map[string]func(limit, offset int) (int, any){
		"/me/tracks": func(limit, offset int) (int, any) {
			if limit > 10 {
				return apiError(400, "Invalid limit")
			}
			return 200, map[string]any{"total": 1, "items": []map[string]any{{"track": track("t1", "One", "A")}}}
		},
	})

	page, err := lib.Tracks(context.Background(), Playlist{Liked: true}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Errorf("items = %+v", page.Items)
	}
	if lib.pageSize() != smallPageSize {
		t.Errorf("page size = %d, want %d", lib.pageSize(), smallPageSize)
	}
	if len(api.requests) != 2 {
		t.Errorf("requests = %v, want the failed call and one retry", api.requests)
	}
}

func TestScopeAndForbiddenErrors(t *testing.T) {
	lib, _ := newTestLibrary(t, map[string]func(limit, offset int) (int, any){
		"/me/tracks":           func(int, int) (int, any) { return apiError(403, "Insufficient client scope") },
		"/playlists/dw/tracks": func(int, int) (int, any) { return apiError(404, "Resource not found") },
	})

	_, err := lib.Playlists(context.Background())
	if !IsScopeError(err) {
		t.Errorf("Playlists error %v, want a scope error", err)
	}
	if IsForbiddenPlaylist(err) {
		t.Errorf("403 reported as a forbidden playlist")
	}

	_, err = lib.Tracks(context.Background(), Playlist{ID: "dw", Name: "Discover Weekly"}, 0)
	if !IsForbiddenPlaylist(err) {
		t.Errorf("Tracks error %v, want a forbidden-playlist error", err)
	}
	if IsScopeError(err) {
		t.Errorf("404 reported as a scope error")
	}
}
