package app

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pseud039/termix/internal/queue"
	"github.com/pseud039/termix/internal/spotify"
)

// fakeLibrary serves canned playlists and pages; the tests feed the
// resulting messages to Update by hand rather than running the Cmds.
type fakeLibrary struct{}

func (fakeLibrary) Playlists(context.Context) ([]spotify.Playlist, error) { return nil, nil }
func (fakeLibrary) Tracks(context.Context, spotify.Playlist, int) (spotify.TrackPage, error) {
	return spotify.TrackPage{}, nil
}

func item(id string) queue.Item {
	return queue.Item{ID: id, Title: id, Source: queue.SourceSpotify, URI: "spotify:track:" + id}
}

func newLibraryModel() Model {
	m := New(context.Background(), queue.New(), nil, nil, nil, nil, nil, nil, fakeLibrary{})
	m.hasTrack = true // so queueing never tries to start playback through the nil router
	return m
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestVisibleRange(t *testing.T) {
	cases := []struct {
		n, height, cursor int
		start, end        int
	}{
		{3, 10, 0, 0, 3},  // everything fits
		{10, 4, 0, 0, 4},  // top of a long list
		{10, 4, 5, 2, 6},  // scrolled so the cursor is the last visible row
		{10, 4, 9, 6, 10}, // bottom
		{0, 4, 0, 0, 0},   // empty
		{10, 0, 0, 0, 1},  // no room still shows one row
	}
	for _, c := range cases {
		s, e := visibleRange(c.n, c.height, c.cursor)
		if s != c.start || e != c.end {
			t.Errorf("visibleRange(%d,%d,%d) = %d,%d want %d,%d", c.n, c.height, c.cursor, s, e, c.start, c.end)
		}
	}
}

func TestLibraryPagesAppendAndStaleAnswersDrop(t *testing.T) {
	m := newLibraryModel()
	p := spotify.Playlist{ID: "p1", Name: "Gym", Total: 3}
	m, _ = m.openPlaylist(p)
	list := m.libTracks["p1"]
	if list == nil || !list.loading {
		t.Fatalf("opening a playlist should start loading it: %+v", list)
	}

	m = update(t, m, libTracksMsg{seq: m.libSeq, playlist: p,
		page: spotify.TrackPage{Items: []queue.Item{item("a"), item("b")}, Total: 3, Next: 2}})
	if got := len(m.libTracks["p1"].items); got != 2 || m.libTracks["p1"].done {
		t.Fatalf("after page 1: %d items, done=%v", got, m.libTracks["p1"].done)
	}

	// An answer from before a refresh must not land in the new cache.
	stale := m.libSeq
	refreshed, _ := m.libRefresh()
	m = refreshed.(Model)
	m = update(t, m, libTracksMsg{seq: stale, playlist: p,
		page: spotify.TrackPage{Items: []queue.Item{item("zzz")}, Total: 3, Next: 3, Done: true}})
	for _, it := range m.libTracks["p1"].items {
		if it.ID == "zzz" {
			t.Fatal("stale page was appended after refresh")
		}
	}

	m = update(t, m, libTracksMsg{seq: m.libSeq, playlist: p,
		page: spotify.TrackPage{Items: []queue.Item{item("a"), item("b"), item("c")}, Total: 3, Next: 3, Done: true}})
	list = m.libTracks["p1"]
	if !list.done || list.loading || len(list.items) != 3 {
		t.Fatalf("after the last page: %+v", list)
	}
	if m.queue.Len() != 0 {
		t.Errorf("opening a playlist queued %d tracks", m.queue.Len())
	}
}

func TestLibraryAddAllQueuesPagesAsTheyLand(t *testing.T) {
	m := newLibraryModel()
	p := spotify.Playlist{ID: "p1", Name: "Gym", Total: 3}
	m.libPlaylists = []spotify.Playlist{p}
	m.libLoaded = true

	next, _ := m.libAddAll()
	m = next.(Model)
	if m.libAddAllKey != "p1" || m.libLevel != libLevelPlaylists {
		t.Fatalf("add-all should flag the playlist and stay on the list: key=%q level=%d", m.libAddAllKey, m.libLevel)
	}

	m = update(t, m, libTracksMsg{seq: m.libSeq, playlist: p,
		page: spotify.TrackPage{Items: []queue.Item{item("a"), item("b")}, Total: 3, Next: 2}})
	if m.queue.Len() != 2 {
		t.Fatalf("queue has %d items after page 1, want 2", m.queue.Len())
	}
	m = update(t, m, libTracksMsg{seq: m.libSeq, playlist: p,
		page: spotify.TrackPage{Items: []queue.Item{item("c")}, Total: 3, Next: 3, Done: true}})
	if m.queue.Len() != 3 {
		t.Fatalf("queue has %d items after page 2, want 3", m.queue.Len())
	}
	if m.libAddAllKey != "" || m.statusMsg != "added 3 tracks from Gym" || m.statusIsErr {
		t.Errorf("after the last page: key=%q status=%q err=%v", m.libAddAllKey, m.statusMsg, m.statusIsErr)
	}
	ids := []string{}
	for _, it := range m.queue.Items() {
		ids = append(ids, it.ID)
	}
	if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Errorf("queue order = %v", ids)
	}

	// A cached, complete list is queued in one go.
	next, _ = m.libAddAll()
	m = next.(Model)
	if m.queue.Len() != 6 || m.statusMsg != "added 3 tracks from Gym" {
		t.Errorf("second add-all: len=%d status=%q", m.queue.Len(), m.statusMsg)
	}
}

func TestLibraryErrorsExplainTheFix(t *testing.T) {
	m := newLibraryModel()
	m = update(t, m, libPlaylistsMsg{seq: m.libSeq, err: errors.New("boom")})
	if !m.statusIsErr || m.statusMsg != "library: boom" || m.libLoaded {
		t.Errorf("status=%q err=%v loaded=%v", m.statusMsg, m.statusIsErr, m.libLoaded)
	}

	p := spotify.Playlist{ID: "p1", Name: "Gym"}
	m, _ = m.openPlaylist(p)
	m.libAddAllKey = "p1"
	m = update(t, m, libTracksMsg{seq: m.libSeq, playlist: p, err: errors.New("nope")})
	list := m.libTracks["p1"]
	if list.loading || list.err == nil || m.libAddAllKey != "" {
		t.Errorf("a failed page should stop the chain and the add-all: %+v key=%q", list, m.libAddAllKey)
	}
}

func TestLibraryCursorStaysInRange(t *testing.T) {
	m := newLibraryModel()
	m.libPlaylists = []spotify.Playlist{{Name: "a"}, {Name: "b"}}
	m.libMoveCursor(-1)
	if m.libCursor != 0 {
		t.Errorf("cursor went above the top: %d", m.libCursor)
	}
	m.libMoveCursor(+5)
	if m.libCursor != 1 {
		t.Errorf("cursor went past the end: %d", m.libCursor)
	}
	m.libLevel = libLevelTracks
	m.libMoveCursor(+1)
	if m.libTrackCursor != 0 {
		t.Errorf("track cursor moved with no tracks loaded: %d", m.libTrackCursor)
	}
}
