package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pseud039/termix/internal/lyrics"
	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
	"github.com/pseud039/termix/internal/recommend"
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

// recommendationsMsg carries smart-shuffle picks for the queue. seq works
// like searchResultsMsg.seq: a fetch started before smart shuffle was
// switched off (or before a newer fetch) is ignored.
type recommendationsMsg struct {
	seq   int
	items []queue.Item
	err   error
}

// recommendationTimeout bounds one smart-shuffle fetch: a Last.fm call
// plus a search per pick, and yt-dlp searches take several seconds each.
const recommendationTimeout = 90 * time.Second

// lyricsMsg carries the outcome of a lyrics fetch for the track that
// started at seq; results for an older track are ignored.
type lyricsMsg struct {
	seq    int
	lyrics *lyrics.Lyrics
	err    error
}

// lyricsTickMsg redraws the Lyrics tab between the one-second playback
// polls so the highlighted line keeps up with the audio.
type lyricsTickMsg time.Time

// lyricsRefresh is how often the Lyrics tab redraws while it is open.
const lyricsRefresh = 250 * time.Millisecond

// lyricsOffsetStep is how far [ and ] nudge the lyric timing.
const lyricsOffsetStep = 500 * time.Millisecond

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

	// Smart shuffle. recommender is nil without a Last.fm key, which makes
	// the smart mode unavailable.
	recommender *recommend.Recommender
	recFetching bool // a recommendation fetch is in flight
	recSeq      int  // identifies the fetch whose answer we still want

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

	// Lyrics pane
	lyricsClient  *lyrics.Client // nil = lyrics off (tests)
	lyricsSeq     int
	lyricsLoading bool
	lyrics        *lyrics.Lyrics // nil when the current track has none
	lyricsErr     error
	lyricsCursor  int           // -1 = follow playback; else the line the user moved to
	lyricsOffset  time.Duration // user nudge; positive shows lyrics later
	lastPollAt    time.Time     // when position was last read, for interpolation
	lyricsTicking bool          // a lyricsTickMsg chain is running
}

// New builds the Model. mpvErr is the result of mpv.Start; when non-nil it is
// shown in the status line so the user sees why local/YouTube playback is off.
// searchers holds one Searcher per available source; the Search tab explains
// how to enable any that are missing. recommender may be nil, in which case
// smart shuffle is unavailable and z only toggles plain shuffle. lyr fetches
// lyrics for each track that starts; nil turns the Lyrics tab off.
func New(ctx context.Context, q *queue.Queue, r *player.Router, mpv *player.MpvPlayer, mpvErr error, searchers map[queue.SourceType]Searcher, recommender *recommend.Recommender, lyr *lyrics.Client) Model {
	m := Model{
		queue:        q,
		router:       r,
		mpv:          mpv,
		searchers:    searchers,
		recommender:  recommender,
		searchSource: queue.SourceSpotify,
		ctx:          ctx,
		volume:       -1,
		mpvReady:     mpvErr == nil,
		lyricsClient: lyr,
		lyricsCursor: -1,
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
		m.lastPollAt = time.Now()
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
		// Fetch this track's lyrics; the seq bump in resetLyrics makes any
		// answer still coming for the previous track land unused.
		var cmds []tea.Cmd
		m.resetLyrics()
		if m.lyricsClient != nil {
			m.lyricsLoading = true
			cmds = append(cmds, m.fetchLyricsCmd(m.lyricsSeq, msg.Item))
		}
		// With smart shuffle on, keep the upcoming list topped up with
		// similar tracks, seeded on whatever just started.
		next, cmd := m.topUpRecommendations(msg.Item)
		cmds = append(cmds, cmd)
		return next, tea.Batch(cmds...)

	case lyricsMsg:
		if msg.seq != m.lyricsSeq {
			return m, nil
		}
		m.lyricsLoading = false
		m.lyrics = msg.lyrics
		m.lyricsErr = msg.err

	case lyricsTickMsg:
		// Only redraw; the view reads an interpolated position. The chain
		// stops once the tab is left and restarts on return (key 3).
		if m.activeTab != tabLyrics {
			m.lyricsTicking = false
			return m, nil
		}
		return m, lyricsTickCmd()

	case QueueAdvanceMsg:
		return m, m.advanceQueueCmd(msg.Failed)

	case queueEndedMsg:
		m.hasTrack = false
		m.currentTrack = queue.Item{}
		m.duration = 0
		m.position = 0
		m.resetLyrics()
		m.statusIsErr = msg.Reason != ""
		m.statusMsg = msg.Reason
		if m.statusMsg == "" {
			if m.queue.ShuffleMode() == queue.ShuffleSmart && m.recFetching {
				// The radio fetch is still running; it starts playback
				// itself when the picks arrive.
				m.statusMsg = "end of queue — finding similar tracks…"
			} else {
				m.statusMsg = "end of queue — add more tracks from [2] Search"
			}
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

	case recommendationsMsg:
		if msg.seq != m.recSeq {
			return m, nil
		}
		m.recFetching = false
		if m.queue.ShuffleMode() != queue.ShuffleSmart {
			return m, nil
		}
		if msg.err != nil {
			m.statusMsg = "smart shuffle: " + msg.err.Error()
			m.statusIsErr = true
			return m, nil
		}
		if len(msg.items) == 0 {
			m.statusMsg = "smart shuffle: no playable similar tracks found"
			m.statusIsErr = true
			return m, nil
		}
		idx := m.queue.InsertRecommended(msg.items)
		m.statusIsErr = false
		m.statusMsg = fmt.Sprintf("smart shuffle: added %d similar track%s", len(msg.items), plural(len(msg.items)))
		// The queue ran dry while we were fetching: start the first pick
		// rather than sit silent, the same way the Search tab does.
		if !m.hasTrack && !m.autoStarting {
			m.autoStarting = true
			m.failStreak = 0
			m.queue.JumpTo(idx)
			return m, m.playCurrentQueueItemCmd()
		}

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
		if !m.lyricsTicking {
			m.lyricsTicking = true
			return m, lyricsTickCmd()
		}

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
		return m.cycleShuffle()
	case "r":
		m.statusIsErr = false
		m.statusMsg = "repeat: " + m.queue.CycleRepeat().String()

	// Lyric timing nudges are global so you can fix a drifting track
	// from any tab.
	case "[":
		return m.nudgeLyrics(-lyricsOffsetStep)
	case "]":
		return m.nudgeLyrics(+lyricsOffsetStep)

	case "/":
		if m.activeTab == tabSearch {
			m.searchMode = true
			m.searchQuery = ""
		}

	case "esc":
		if m.activeTab == tabLyrics {
			m.lyricsCursor = -1
		}

	case "up", "k":
		switch m.activeTab {
		case tabSearch:
			if m.searchCursor > 0 {
				m.searchCursor--
			}
		case tabLyrics:
			m.moveLyricsCursor(-1)
		}
	case "down", "j":
		switch m.activeTab {
		case tabSearch:
			if m.searchCursor < len(m.searchResults)-1 {
				m.searchCursor++
			}
		case tabLyrics:
			m.moveLyricsCursor(+1)
		}

	case "enter":
		switch m.activeTab {
		case tabQueue:
			m.failStreak = 0
			return m, m.playCurrentQueueItemCmd()
		case tabLyrics:
			// Jump to the selected line, then follow playback again.
			if m.lyrics.IsSynced() && m.lyricsCursor >= 0 && m.lyricsCursor < len(m.lyrics.Synced) {
				at := m.lyrics.Synced[m.lyricsCursor].At + m.lyricsOffset
				m.lyricsCursor = -1
				return m, m.seekToCmd(at.Seconds())
			}
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
		return "yt-dlp not found — install it or set youtube.ytdlp in config.toml"
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

// cycleShuffle steps off → shuffle → smart → off on z. Smart shuffle is
// skipped when there is no recommender (no Last.fm key).
func (m Model) cycleShuffle() (tea.Model, tea.Cmd) {
	m.statusIsErr = false
	switch m.queue.ShuffleMode() {
	case queue.ShuffleOff:
		m.queue.SetShuffleMode(queue.ShuffleOn)
		m.statusMsg = "shuffle on"

	case queue.ShuffleOn:
		if m.recommender == nil {
			m.queue.SetShuffleMode(queue.ShuffleOff)
			m.statusMsg = "shuffle off — smart shuffle needs lastfm.api_key in config.toml (free key at last.fm/api)"
			m.statusIsErr = true
			return m, nil
		}
		m.queue.SetShuffleMode(queue.ShuffleSmart)
		m.statusMsg = "smart shuffle on — finding similar tracks…"
		seed, ok := m.currentTrack, m.hasTrack
		if !ok {
			// Nothing playing: seed on the last thing the user queued.
			if items := m.queue.Items(); len(items) > 0 {
				seed, ok = items[len(items)-1], true
			}
		}
		if !ok {
			m.statusMsg = "smart shuffle on — add a track to seed recommendations"
			return m, nil
		}
		return m.topUpRecommendations(seed)

	default: // smart
		m.queue.SetShuffleMode(queue.ShuffleOff)
		m.statusMsg = "shuffle off"
		// Drop the answer of any fetch still running.
		m.recSeq++
		m.recFetching = false
	}
	return m, nil
}

// topUpRecommendations fetches more smart-shuffle picks when the upcoming
// list is short on them: about one per three tracks the user queued, and a
// batch of three when the seed is the last track so playback keeps going.
func (m Model) topUpRecommendations(seed queue.Item) (tea.Model, tea.Cmd) {
	if m.recommender == nil || m.recFetching || m.queue.ShuffleMode() != queue.ShuffleSmart {
		return m, nil
	}
	own, recs := m.queue.UpcomingCounts()
	want := own / 3
	if want < 1 {
		want = 1
	}
	if own+recs == 0 {
		want = 3
	}
	if want -= recs; want <= 0 {
		return m, nil
	}
	m.recSeq++
	m.recFetching = true
	return m, m.fetchRecommendationsCmd(m.recSeq, seed, want)
}

// fetchRecommendationsCmd asks the recommender for n picks similar to seed,
// skipping anything already in the queue.
func (m Model) fetchRecommendationsCmd(seq int, seed queue.Item, n int) tea.Cmd {
	exclude := map[string]bool{}
	for _, it := range m.queue.Items() {
		exclude[recommend.Key(it.Artist, it.Title)] = true
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, recommendationTimeout)
		defer cancel()
		items, err := m.recommender.ForSeed(ctx, seed, exclude, n)
		return recommendationsMsg{seq: seq, items: items, err: err}
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
		return m.seekToCmd(pos + delta)()
	}
}

// seekToCmd jumps to an absolute position in seconds (clamped at 0).
func (m Model) seekToCmd(seconds float64) tea.Cmd {
	return func() tea.Msg {
		if seconds < 0 {
			seconds = 0
		}
		if err := m.router.Seek(m.ctx, seconds); err != nil {
			return ErrorMsg{err}
		}
		return nil
	}
}

// resetLyrics forgets the previous track's lyrics and invalidates any
// fetch still in flight for it.
func (m *Model) resetLyrics() {
	m.lyricsSeq++
	m.lyricsLoading = false
	m.lyrics = nil
	m.lyricsErr = nil
	m.lyricsCursor = -1
	m.lyricsOffset = 0
}

func (m Model) fetchLyricsCmd(seq int, item queue.Item) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
		defer cancel()
		l, err := m.lyricsClient.Fetch(ctx, item)
		return lyricsMsg{seq: seq, lyrics: l, err: err}
	}
}

// lyricsTickCmd fires lyricsTickMsg after lyricsRefresh; Update re-issues
// it while the Lyrics tab is open.
func lyricsTickCmd() tea.Cmd {
	return tea.Tick(lyricsRefresh, func(t time.Time) tea.Msg {
		return lyricsTickMsg(t)
	})
}

// playbackPos estimates the current position between polls: the last
// polled position plus the time since, while playing.
func (m Model) playbackPos() time.Duration {
	pos := m.position
	if m.playing && !m.lastPollAt.IsZero() {
		pos += time.Since(m.lastPollAt)
	}
	if m.duration > 0 && pos > m.duration {
		pos = m.duration
	}
	return pos
}

// lyricsLineCount is how many lines the Lyrics tab can move a cursor over.
func (m Model) lyricsLineCount() int {
	switch {
	case m.lyrics.IsSynced():
		return len(m.lyrics.Synced)
	case m.lyrics != nil && m.lyrics.Plain != "":
		return len(strings.Split(m.lyrics.Plain, "\n"))
	}
	return 0
}

// moveLyricsCursor steps the manual cursor. The first move starts from the
// line being sung (synced) or the top (plain).
func (m *Model) moveLyricsCursor(step int) {
	n := m.lyricsLineCount()
	if n == 0 {
		return
	}
	cur := m.lyricsCursor
	if cur < 0 {
		cur = 0
		if m.lyrics.IsSynced() {
			cur = m.lyrics.ActiveLine(m.playbackPos() - m.lyricsOffset)
			if cur < 0 {
				cur = 0
			}
		}
	}
	cur += step
	if cur < 0 {
		cur = 0
	}
	if cur > n-1 {
		cur = n - 1
	}
	m.lyricsCursor = cur
}

// nudgeLyrics shifts the lyric timing and reports the new offset.
func (m Model) nudgeLyrics(delta time.Duration) (tea.Model, tea.Cmd) {
	m.lyricsOffset += delta
	m.statusIsErr = false
	m.statusMsg = "lyrics offset: " + formatOffset(m.lyricsOffset)
	return m, nil
}

// formatOffset renders a timing nudge as "+0.5s", "-1.0s" or "0".
func formatOffset(d time.Duration) string {
	if d == 0 {
		return "0"
	}
	return fmt.Sprintf("%+.1fs", d.Seconds())
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
