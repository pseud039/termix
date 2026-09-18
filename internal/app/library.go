package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pseud039/termix/internal/queue"
	"github.com/pseud039/termix/internal/spotify"
)

// Library lists the user's Spotify Liked Songs and playlists for the
// Library tab. spotify.Library satisfies it; nil means Spotify isn't
// connected.
type Library interface {
	Playlists(ctx context.Context) ([]spotify.Playlist, error)
	Tracks(ctx context.Context, p spotify.Playlist, offset int) (spotify.TrackPage, error)
}

// The Library tab has two levels: the list of playlists, and the tracks of
// the one that was opened.
const (
	libLevelPlaylists = iota
	libLevelTracks
)

// libTrackList is the session cache for one playlist's tracks. Pages
// stream in one at a time, so a list can be shown while it is still
// loading.
type libTrackList struct {
	items   []queue.Item
	total   int  // entries Spotify reports, including unplayable ones
	next    int  // offset of the next page to fetch
	done    bool // every page has arrived
	loading bool // a page chain is running
	err     error
}

// libPlaylistsMsg carries the playlist list. seq works like
// searchResultsMsg.seq: an answer for an older request is ignored.
type libPlaylistsMsg struct {
	seq       int
	playlists []spotify.Playlist
	err       error
}

// libTracksMsg carries one page of a playlist's tracks. Update appends it
// to the cache and asks for the next page until Done.
type libTracksMsg struct {
	seq      int
	playlist spotify.Playlist
	page     spotify.TrackPage
	err      error
}

// libraryTimeout bounds one Library request (one page).
const libraryTimeout = 30 * time.Second

// openLibraryTab switches to the tab and loads the playlists the first
// time it is opened.
func (m Model) openLibraryTab() (tea.Model, tea.Cmd) {
	m.activeTab = tabLibrary
	if m.library == nil || m.libLoaded || m.libLoading {
		return m, nil
	}
	return m.loadPlaylists()
}

func (m Model) loadPlaylists() (Model, tea.Cmd) {
	m.libLoading = true
	m.libErr = nil
	return m, m.fetchPlaylistsCmd(m.libSeq)
}

func (m Model) fetchPlaylistsCmd(seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, libraryTimeout)
		defer cancel()
		playlists, err := m.library.Playlists(ctx)
		return libPlaylistsMsg{seq: seq, playlists: playlists, err: err}
	}
}

func (m Model) fetchTracksCmd(seq int, p spotify.Playlist, offset int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, libraryTimeout)
		defer cancel()
		page, err := m.library.Tracks(ctx, p, offset)
		return libTracksMsg{seq: seq, playlist: p, page: page, err: err}
	}
}

func (m Model) handleLibPlaylistsMsg(msg libPlaylistsMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.libSeq {
		return m, nil
	}
	m.libLoading = false
	m.libErr = msg.err
	if msg.err != nil {
		m.statusMsg = libraryErrorText(msg.err)
		m.statusIsErr = true
		return m, nil
	}
	m.libLoaded = true
	m.libPlaylists = msg.playlists
	if m.libCursor >= len(m.libPlaylists) {
		m.libCursor = 0
	}
	return m, nil
}

func (m Model) handleLibTracksMsg(msg libTracksMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.libSeq {
		return m, nil
	}
	key := msg.playlist.Key()
	list := m.libTracks[key]
	if list == nil {
		return m, nil
	}
	if msg.err != nil {
		list.loading = false
		list.err = msg.err
		if m.libAddAllKey == key {
			m.libAddAllKey = ""
		}
		m.statusMsg = libraryErrorText(msg.err)
		m.statusIsErr = true
		return m, nil
	}

	list.items = append(list.items, msg.page.Items...)
	list.total = msg.page.Total
	list.next = msg.page.Next

	var cmds []tea.Cmd
	if m.libAddAllKey == key {
		var cmd tea.Cmd
		m, cmd = m.queueItems(msg.page.Items)
		cmds = append(cmds, cmd)
		m.libAddAllCount += len(msg.page.Items)
	}

	if msg.page.Done {
		list.loading = false
		list.done = true
		if m.libAddAllKey == key {
			m.libAddAllKey = ""
			m.statusIsErr = false
			m.statusMsg = addedFromText(m.libAddAllCount, msg.playlist.Name)
		}
	} else {
		cmds = append(cmds, m.fetchTracksCmd(msg.seq, msg.playlist, msg.page.Next))
	}
	return m, tea.Batch(cmds...)
}

// libMoveCursor steps the cursor of whichever level is showing.
func (m *Model) libMoveCursor(step int) {
	n := 0
	cur := &m.libCursor
	if m.libLevel == libLevelTracks {
		cur = &m.libTrackCursor
		if list := m.libTracks[m.libOpen.Key()]; list != nil {
			n = len(list.items)
		}
	} else {
		n = len(m.libPlaylists)
	}
	*cur += step
	if *cur > n-1 {
		*cur = n - 1
	}
	if *cur < 0 {
		*cur = 0
	}
}

// libEnter opens the playlist under the cursor, or queues the track under
// it once inside a playlist.
func (m Model) libEnter() (tea.Model, tea.Cmd) {
	if m.libLevel == libLevelTracks {
		list := m.libTracks[m.libOpen.Key()]
		if list == nil || m.libTrackCursor >= len(list.items) {
			return m, nil
		}
		return m.addToQueue(list.items[m.libTrackCursor])
	}
	if m.libCursor >= len(m.libPlaylists) {
		return m, nil
	}
	return m.openPlaylist(m.libPlaylists[m.libCursor])
}

func (m Model) openPlaylist(p spotify.Playlist) (Model, tea.Cmd) {
	m.libOpen = p
	m.libLevel = libLevelTracks
	m.libTrackCursor = 0
	return m.ensureTracks(p)
}

// ensureTracks starts loading p's tracks unless they are cached or already
// on their way. A list whose chain failed is restarted from where it stopped.
func (m Model) ensureTracks(p spotify.Playlist) (Model, tea.Cmd) {
	key := p.Key()
	list := m.libTracks[key]
	if list == nil {
		list = &libTrackList{}
		m.libTracks[key] = list
	}
	if list.done || list.loading {
		return m, nil
	}
	list.loading = true
	list.err = nil
	return m, m.fetchTracksCmd(m.libSeq, p, list.next)
}

// libAddAll queues every track of the playlist under the cursor (playlist
// level) or of the open playlist (track level). Pages that haven't arrived
// yet are queued as they land.
func (m Model) libAddAll() (tea.Model, tea.Cmd) {
	var p spotify.Playlist
	if m.libLevel == libLevelTracks {
		p = m.libOpen
	} else {
		if m.libCursor >= len(m.libPlaylists) {
			return m, nil
		}
		p = m.libPlaylists[m.libCursor]
	}
	key := p.Key()
	if m.libAddAllKey != "" && m.libAddAllKey != key {
		m.statusMsg = "library: still adding another playlist, try again in a moment"
		m.statusIsErr = true
		return m, nil
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m, cmd = m.ensureTracks(p)
	cmds = append(cmds, cmd)
	list := m.libTracks[key]

	if m.libAddAllKey != key {
		// Queue what is here already; the page chain delivers the rest.
		m, cmd = m.queueItems(list.items)
		cmds = append(cmds, cmd)
		m.libAddAllCount = len(list.items)
	}
	m.statusIsErr = false
	if list.done {
		m.statusMsg = addedFromText(m.libAddAllCount, p.Name)
	} else {
		m.libAddAllKey = key
		m.statusMsg = fmt.Sprintf("adding %s to the queue…", p.Name)
	}
	return m, tea.Batch(cmds...)
}

// libBack returns from a playlist's tracks to the playlist list.
func (m Model) libBack() Model {
	m.libLevel = libLevelPlaylists
	return m
}

// libRefresh refetches the level that is showing. Any page chain still
// running is abandoned, since its answers would land in a stale cache.
func (m Model) libRefresh() (tea.Model, tea.Cmd) {
	if m.library == nil {
		return m, nil
	}
	m = m.invalidateLibraryFetches()
	if m.libLevel == libLevelTracks {
		delete(m.libTracks, m.libOpen.Key())
		m.libTrackCursor = 0
		return m.ensureTracks(m.libOpen)
	}
	m.libTracks = map[string]*libTrackList{}
	return m.loadPlaylists()
}

// invalidateLibraryFetches drops the answers of every request in flight.
// Lists that were still loading are removed rather than left half full
// with no chain to finish them.
func (m Model) invalidateLibraryFetches() Model {
	m.libSeq++
	m.libLoading = false
	m.libAddAllKey = ""
	for key, list := range m.libTracks {
		if !list.done {
			delete(m.libTracks, key)
		}
	}
	return m
}

// addToQueue appends one item and reports it in the status line. The
// Search tab's Enter and the Library tab's Enter both go through here.
func (m Model) addToQueue(item queue.Item) (Model, tea.Cmd) {
	m, cmd := m.queueItems([]queue.Item{item})
	m.statusIsErr = false
	m.statusMsg = fmt.Sprintf("added to queue: %s — %s", item.Artist, item.Title)
	return m, cmd
}

// queueItems appends items in order. With shuffle on each lands somewhere
// in the upcoming part of the queue rather than at the end. When nothing
// is playing (empty queue, or the queue ran out) the first item starts
// right away instead of waiting for [n].
func (m Model) queueItems(items []queue.Item) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, item := range items {
		idx := m.queue.Add(item)
		if cmd == nil && !m.hasTrack && !m.autoStarting {
			m.autoStarting = true
			m.failStreak = 0
			m.queue.JumpTo(idx)
			cmd = m.playCurrentQueueItemCmd()
		}
	}
	return m, cmd
}

func addedFromText(n int, name string) string {
	return fmt.Sprintf("added %d track%s from %s", n, plural(n), name)
}

// libraryErrorText turns a Library error into a status line, with a fix
// for the two cases the user can do something about.
func libraryErrorText(err error) string {
	switch {
	case spotify.IsScopeError(err):
		return "Library needs new Spotify permissions — run `termix auth` again"
	case spotify.IsForbiddenPlaylist(err):
		return "Spotify doesn't let apps read this playlist (Spotify-made playlists are blocked)"
	}
	return "library: " + err.Error()
}
