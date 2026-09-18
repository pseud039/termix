package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
)

// TickMsg fires every second from the tea.Tick loop.
type TickMsg time.Time

// PlaybackStateMsg carries a fresh position snapshot polled from the player.
type PlaybackStateMsg player.State

// ErrorMsg shows an error in the status bar.
type ErrorMsg struct{ Err error }

// TrackStartMsg is dispatched whenever a new track begins (play cmd or auto-advance).
type TrackStartMsg struct{ Item queue.Item }

// QueueAdvanceMsg moves to the next queue item after mpv reports that the
// current track ended ("eof") or failed ("error"). Failed is true for the
// latter so repeat-one doesn't replay a broken track.
type QueueAdvanceMsg struct{ Failed bool }

// queueEndedMsg is sent when the last track finishes and there is nothing
// left to play. Reason, when set, replaces the default status line.
type queueEndedMsg struct{ Reason string }

// mpvEventMsg wraps an event read from the mpv events channel.
type mpvEventMsg struct{ ev player.MpvEvent }

// Searcher finds tracks for the Search tab. The Spotify, YouTube and local
// search providers all satisfy it.
type Searcher interface {
	Search(ctx context.Context, q string) ([]queue.Item, error)
}

// searchResultsMsg carries the outcome of a search. seq identifies the
// search it answers so results from an older query are ignored.
type searchResultsMsg struct {
	seq   int
	items []queue.Item
	err   error
}

type tab int

const (
	tabQueue tab = iota
	tabSearch
	tabLyrics
)

var tabNames = []string{"Queue", "Search", "Lyrics"}

// Model holds all application state.
type Model struct {
	// Injected subsystems
	queue     *queue.Queue
	router    *player.Router
	mpv       *player.MpvPlayer
	searchers map[queue.SourceType]Searcher // a source is missing when it isn't available
	ctx       context.Context

	// TUI layout
	activeTab   tab
	width       int
	height      int
	statusMsg   string
	statusIsErr bool

	// Playback state — updated every second by pollPlaybackCmd
	playing      bool
	position     time.Duration
	duration     time.Duration // total length of current track; 0 = unknown
	volume       int           // -1 until the first poll reads it from the player
	volumeSetAt  time.Time     // last +/- press; polls just after it are ignored
	currentTrack queue.Item
	hasTrack     bool
	autoStarting bool // a track added to an idle queue is being started
	failStreak   int  // consecutive tracks mpv failed to play; reset on success or a manual play

	// Whether mpv started successfully — gates playback-related UI hints
	mpvReady bool

	// Search pane
	searchQuery   string
	searchMode    bool
	searchSource  queue.SourceType
	lastQuery     string
	searching     bool
	searchSeq     int
	searchResults []queue.Item
	searchCursor  int
	searchErr     error
}

// New builds the Model. mpvErr is the result of mpv.Start; when non-nil it is
// shown in the status line so the user sees why local/YouTube playback is off.
// searchers holds one Searcher per available source; the Search tab explains
// how to enable any that are missing.
func New(ctx context.Context, q *queue.Queue, r *player.Router, mpv *player.MpvPlayer, mpvErr error, searchers map[queue.SourceType]Searcher) Model {
	m := Model{
		queue:        q,
		router:       r,
		mpv:          mpv,
		searchers:    searchers,
		searchSource: queue.SourceSpotify,
		ctx:          ctx,
		volume:       -1,
		mpvReady:     mpvErr == nil,
	}
	if mpvErr != nil {
		m.statusMsg = mpvErr.Error()
		m.statusIsErr = true
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}

	// Without a running mpv the events channel never receives, so listening
	// would just block forever.
	if m.mpvReady {
		cmds = append(cmds, m.waitForMpvEventCmd())
	}

	return tea.Batch(cmds...)
}

// tickCmd fires TickMsg after one second; Update re-issues it on every tick.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// waitForMpvEventCmd blocks on the mpv events channel and returns the next
// event as an mpvEventMsg. A tea.Cmd fires only once, so Update must re-issue
// this after every mpvEventMsg or later end-file events are lost.
func (m Model) waitForMpvEventCmd() tea.Cmd {
	return func() tea.Msg {
		ev := <-m.mpv.Events()
		return mpvEventMsg{ev: ev}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		return m, tea.Batch(tickCmd(), m.pollPlaybackCmd())

	case PlaybackStateMsg:
		m.playing = msg.Playing
		m.position = msg.Position
		// Only accept a known duration — mpv reports null for a moment when
		// a track is loading, and volumeCmd sends a state with no duration.
		// Neither should blank a length we already have.
		if msg.Duration > 0 {
			m.duration = msg.Duration
			// Save the length on the queue item if it didn't have one
			// (local files). The ID check skips a poll that started before
			// the track changed.
			if m.hasTrack && msg.TrackID == m.currentTrack.ID && m.currentTrack.Duration == 0 {
				m.currentTrack.Duration = msg.Duration
				m.queue.SetDuration(m.currentTrack.ID, msg.Duration)
			}
		}
		// A poll that started before a +/- press would still carry the
		// old volume, so give the new value a moment to settle.
		if msg.Volume >= 0 && time.Since(m.volumeSetAt) > 2*time.Second {
			m.volume = msg.Volume
		}

	case TrackStartMsg:
		m.autoStarting = false
		m.hasTrack = true
		m.currentTrack = msg.Item
		// Start from whatever the item knows; the next poll fills in the
		// real length from the backend.
		m.duration = msg.Item.Duration
		m.statusIsErr = false
		m.statusMsg = fmt.Sprintf("playing: %s — %s [%s]",
			msg.Item.Artist, msg.Item.Title, msg.Item.Source)

	case QueueAdvanceMsg:
		return m, m.advanceQueueCmd(msg.Failed)

	case queueEndedMsg:
		m.hasTrack = false
		m.currentTrack = queue.Item{}
		m.duration = 0
		m.position = 0
		m.statusIsErr = msg.Reason != ""
		m.statusMsg = msg.Reason
		if m.statusMsg == "" {
			m.statusMsg = "end of queue — add more tracks from [2] Search"
		}

	case mpvEventMsg:
		// Keep listening; see waitForMpvEventCmd.
		var cmds []tea.Cmd
		cmds = append(cmds, m.waitForMpvEventCmd())

		switch msg.ev.Type {
		case "end-file":
			switch msg.ev.Reason {
			case "eof":
				m.failStreak = 0
				cmds = append(cmds, func() tea.Msg { return QueueAdvanceMsg{} })
			case "error":
				m.failStreak++
				// With repeat on, a queue where nothing plays would cycle
				// forever, so give up once every track has failed in a row.
				if m.failStreak >= m.queue.Len() {
					m.failStreak = 0
					cmds = append(cmds, func() tea.Msg {
						return queueEndedMsg{Reason: "mpv: every track failed to play, stopping"}
					})
					break
				}
				m.statusMsg = "mpv: error playing track, skipping"
				m.statusIsErr = true
				cmds = append(cmds, func() tea.Msg { return QueueAdvanceMsg{Failed: true} })
			}
			// "stop" and "quit" come from skipping or shutting down, not a
			// track finishing, so they must not advance the queue.

		case "start-file":
			m.statusIsErr = false
		}

		return m, tea.Batch(cmds...)

	case searchResultsMsg:
		if msg.seq != m.searchSeq {
			return m, nil
		}
		m.searching = false
		m.searchResults = msg.items
		m.searchCursor = 0
		m.searchErr = msg.err

	case ErrorMsg:
		m.autoStarting = false
		m.statusMsg = msg.Err.Error()
		m.statusIsErr = true

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode {
		return m.handleSearchKey(msg)
	}

	// On the Search tab s/y/l pick the search source, so they must be
	// checked before the global keys (l would otherwise seek).
	if m.activeTab == tabSearch {
		if src, ok := searchModeKeys[msg.String()]; ok {
			return m.setSearchSource(src)
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "1":
		m.activeTab = tabQueue
	case "2":
		m.activeTab = tabSearch
	case "3":
		m.activeTab = tabLyrics

	case " ":
		return m, m.togglePauseCmd()
	case "n":
		m.failStreak = 0
		return m, m.nextTrackCmd()
	case "p":
		m.failStreak = 0
		return m, m.prevTrackCmd()
	case "right", "l":
		return m, m.seekCmd(+10)
	case "left", "h":
		return m, m.seekCmd(-10)
	case "+", "=":
		return m.changeVolume(+5)
	case "-":
		return m.changeVolume(-5)

	case "z":
		m.statusIsErr = false
		if m.queue.ToggleShuffle() {
			m.statusMsg = "shuffle on"
		} else {
			m.statusMsg = "shuffle off"
		}
	case "r":
		m.statusIsErr = false
		m.statusMsg = "repeat: " + m.queue.CycleRepeat().String()

	case "/":
		if m.activeTab == tabSearch {
			m.searchMode = true
			m.searchQuery = ""
		}

	case "up", "k":
		if m.activeTab == tabSearch && m.searchCursor > 0 {
			m.searchCursor--
		}
	case "down", "j":
		if m.activeTab == tabSearch && m.searchCursor < len(m.searchResults)-1 {
			m.searchCursor++
		}

	case "enter":
		switch m.activeTab {
		case tabQueue:
			m.failStreak = 0
			return m, m.playCurrentQueueItemCmd()
		case tabSearch:
			if m.searchCursor < len(m.searchResults) {
				item := m.searchResults[m.searchCursor]
				// With shuffle on the item lands somewhere in the upcoming
				// part of the queue rather than at the end.
				idx := m.queue.Add(item)
				m.statusIsErr = false
				m.statusMsg = fmt.Sprintf("added to queue: %s — %s", item.Artist, item.Title)
				// Nothing playing (empty queue, or the queue ran out): start
				// the track just added instead of waiting for [n].
				if !m.hasTrack && !m.autoStarting {
					m.autoStarting = true
					m.failStreak = 0
					m.queue.JumpTo(idx)
					return m, m.playCurrentQueueItemCmd()
				}
			}
		}
	}

	return m, nil
}

func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		}
	case "tab":
		m.searchSource = cycleSource(m.searchSource, +1)
	case "shift+tab":
		m.searchSource = cycleSource(m.searchSource, -1)
	case "enter":
		m.searchMode = false
		query := strings.TrimSpace(m.searchQuery)
		if query == "" {
			return m, nil
		}
		return m.startSearch(query)
	default:
		if len(msg.String()) == 1 {
			m.searchQuery += msg.String()
		}
	}
	return m, nil
}

// searchSources is the order Tab cycles through.
var searchSources = []queue.SourceType{queue.SourceSpotify, queue.SourceYouTube, queue.SourceLocal}

var searchModeKeys = map[string]queue.SourceType{
	"s": queue.SourceSpotify,
	"y": queue.SourceYouTube,
	"l": queue.SourceLocal,
}

func cycleSource(cur queue.SourceType, step int) queue.SourceType {
	for i, s := range searchSources {
		if s == cur {
			n := len(searchSources)
			return searchSources[((i+step)%n+n)%n]
		}
	}
	return queue.SourceSpotify
}

// setSearchSource switches the search mode and re-runs the last query on
// the new source so the results match the label.
func (m Model) setSearchSource(src queue.SourceType) (tea.Model, tea.Cmd) {
	if src == m.searchSource {
		return m, nil
	}
	m.searchSource = src
	if m.lastQuery == "" {
		return m, nil
	}
	return m.startSearch(m.lastQuery)
}

// searchUnavailableHint explains how to enable a source that has no Searcher.
func searchUnavailableHint(src queue.SourceType) string {
	switch src {
	case queue.SourceSpotify:
		return "Spotify not connected — run `termix auth` to enable search"
	case queue.SourceYouTube:
		return "yt-dlp not found — install it or set TERMIX_YTDLP"
	}
	return src.String() + " search is not available"
}

func (m Model) startSearch(query string) (tea.Model, tea.Cmd) {
	m.lastQuery = query
	m.searchResults = nil
	m.searchCursor = 0
	m.searchErr = nil
	// Bump seq even when the source is unavailable so a search still in
	// flight for the previous source can't fill in results under this label.
	m.searchSeq++
	searcher, ok := m.searchers[m.searchSource]
	if !ok {
		m.searching = false
		m.statusMsg = searchUnavailableHint(m.searchSource)
		m.statusIsErr = true
		return m, nil
	}
	m.searching = true
	return m, m.searchCmd(m.searchSeq, searcher, query)
}

func (m Model) searchCmd(seq int, searcher Searcher, query string) tea.Cmd {
	return func() tea.Msg {
		// yt-dlp can take a while to answer, so allow more than a web API call.
		ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		defer cancel()
		items, err := searcher.Search(ctx, query)
		return searchResultsMsg{seq: seq, items: items, err: err}
	}
}

// pollPlaybackCmd asks the active player for its position and duration.
// It runs on every TickMsg.
func (m Model) pollPlaybackCmd() tea.Cmd {
	trackID := m.currentTrack.ID
	return func() tea.Msg {
		pos, playing, err := m.router.Position(m.ctx)
		if err != nil {
			return nil // transient poll errors are expected; don't spam the status bar
		}
		// Duration is polled separately so a failure here doesn't drop the
		// position update — we just report "unknown" (0) for this tick.
		dur, err := m.router.Duration(m.ctx)
		if err != nil {
			dur = 0
		}
		vol, err := m.router.Volume(m.ctx)
		if err != nil {
			vol = -1
		}
		return PlaybackStateMsg{
			Playing:  playing,
			Position: time.Duration(pos * float64(time.Second)),
			Duration: time.Duration(dur * float64(time.Second)),
			Volume:   vol,
			TrackID:  trackID,
		}
	}
}

func (m Model) togglePauseCmd() tea.Cmd {
	return func() tea.Msg {
		var err error
		if m.playing {
			err = m.router.Pause(m.ctx)
		} else {
			err = m.router.Resume(m.ctx)
		}
		if err != nil {
			return ErrorMsg{err}
		}
		return nil
	}
}

func (m Model) nextTrackCmd() tea.Cmd {
	return func() tea.Msg {
		item, ok := m.queue.Next()
		if !ok {
			return nil
		}
		if err := m.router.Play(m.ctx, item); err != nil {
			return ErrorMsg{err}
		}
		return TrackStartMsg{Item: item}
	}
}

func (m Model) prevTrackCmd() tea.Cmd {
	return func() tea.Msg {
		item, ok := m.queue.Prev()
		if !ok {
			return nil
		}
		if err := m.router.Play(m.ctx, item); err != nil {
			return ErrorMsg{err}
		}
		return TrackStartMsg{Item: item}
	}
}

func (m Model) seekCmd(delta float64) tea.Cmd {
	return func() tea.Msg {
		pos, _, _ := m.router.Position(m.ctx)
		newPos := pos + delta
		if newPos < 0 {
			newPos = 0
		}
		if err := m.router.Seek(m.ctx, newPos); err != nil {
			return ErrorMsg{err}
		}
		return nil
	}
}

// changeVolume shows the new volume right away and sends it to the player.
// Until the first poll the real volume is unknown, so there's nothing to
// step from yet.
func (m Model) changeVolume(delta int) (tea.Model, tea.Cmd) {
	if m.volume < 0 {
		return m, nil
	}
	newVol := m.volume + delta
	if newVol < 0 {
		newVol = 0
	}
	if newVol > 100 {
		newVol = 100
	}
	m.volume = newVol
	m.volumeSetAt = time.Now()
	return m, func() tea.Msg {
		if err := m.router.SetVolume(m.ctx, newVol); err != nil {
			return ErrorMsg{err}
		}
		return nil
	}
}

func (m Model) playCurrentQueueItemCmd() tea.Cmd {
	return func() tea.Msg {
		// Nothing has played yet: start from the top of the queue.
		if m.queue.CurrentIndex() < 0 {
			m.queue.JumpTo(0)
		}
		item, ok := m.queue.Current()
		if !ok {
			return nil
		}
		if err := m.router.Play(m.ctx, item); err != nil {
			return ErrorMsg{err}
		}
		return TrackStartMsg{Item: item}
	}
}

// advanceQueueCmd picks what plays after a track ends on its own. A track
// that finished normally honours repeat-one; one that failed always moves
// on so a bad URI can't loop.
func (m Model) advanceQueueCmd(failed bool) tea.Cmd {
	return func() tea.Msg {
		var item queue.Item
		var ok bool
		if failed {
			item, ok = m.queue.Next()
		} else {
			item, ok = m.queue.Advance()
		}
		if !ok {
			return queueEndedMsg{}
		}
		if err := m.router.Play(m.ctx, item); err != nil {
			return ErrorMsg{err}
		}
		return TrackStartMsg{Item: item}
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	header := m.renderHeader()
	playerBar := m.renderPlayerBar()
	statusBar := m.renderStatus()
	contentHeight := m.height -
		lipgloss.Height(header) -
		lipgloss.Height(playerBar) -
		lipgloss.Height(statusBar)
	content := m.renderContent(contentHeight)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		content,
		playerBar,
		statusBar,
	)
}
