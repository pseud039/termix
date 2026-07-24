package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
)

// ── Messages ──────────────────────────────────────────────────────────────────

// TickMsg fires every second from the tea.Tick loop.
type TickMsg time.Time

// PlaybackStateMsg carries a fresh position snapshot polled from the player.
type PlaybackStateMsg player.State

// ErrorMsg shows in the status bar for one cycle.
type ErrorMsg struct{ Err error }

// TrackStartMsg is dispatched whenever a new track begins (play cmd or auto-advance).
type TrackStartMsg struct{ Item queue.Item }

// QueueAdvanceMsg is dispatched by waitForMpvEventCmd when mpv fires "end-file"
// with reason "eof" — meaning the track finished naturally (not skipped/stopped).
type QueueAdvanceMsg struct{}

// mpvEventMsg is an internal message carrying the raw MpvEvent from the channel.
// It is handled in Update and never leaks outside this file.
type mpvEventMsg struct{ ev player.MpvEvent }

// ── Tabs ──────────────────────────────────────────────────────────────────────

type tab int

const (
	tabQueue tab = iota
	tabSearch
	tabLyrics
)

var tabNames = []string{"Queue", "Search", "Lyrics"}

// ── Model ─────────────────────────────────────────────────────────────────────

// Model is the single source of truth for the entire application.
// Bubbletea calls Init → Update → View in a tight loop on the main goroutine.
// Everything that touches shared state must go through this model.
type Model struct {
	// Injected subsystems
	queue  *queue.Queue
	router *player.Router
	mpv    *player.MpvPlayer
	ctx    context.Context

	// TUI layout
	activeTab   tab
	width       int
	height      int
	statusMsg   string
	statusIsErr bool

	// Playback state — updated every second by pollPlaybackCmd
	playing      bool
	position     time.Duration
	volume       int
	currentTrack queue.Item
	hasTrack     bool

	// Whether mpv started successfully — gates playback-related UI hints
	mpvReady bool

	// Search pane
	searchQuery string
	searchMode  bool
}

func New(ctx context.Context, q *queue.Queue, r *player.Router, mpv *player.MpvPlayer, mpvReady bool) Model {
	return Model{
		queue:    q,
		router:   r,
		mpv:      mpv,
		ctx:      ctx,
		volume:   80,
		mpvReady: mpvReady,
	}
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}

	// Only start listening for mpv events if mpv actually started.
	// If mpv isn't running, waitForMpvEventCmd would block forever on a
	// channel that never receives — so we gate it here.
	if m.mpvReady {
		cmds = append(cmds, m.waitForMpvEventCmd())
	}

	return tea.Batch(cmds...)
}

// tickCmd returns a Cmd that fires TickMsg after one second.
// We re-issue it on every TickMsg to keep the loop running.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// waitForMpvEventCmd returns a Cmd that blocks on the mpv events channel.
// When an event arrives it wraps it in mpvEventMsg and returns it to Update.
//
// Critical design point: tea.Cmd is one-shot. After it fires once, it's done.
// So every time we handle an mpvEventMsg in Update we MUST re-issue this cmd
// — otherwise we stop listening and future end-file events are silently lost.
func (m Model) waitForMpvEventCmd() tea.Cmd {
	return func() tea.Msg {
		// This blocks until mpv sends an event (end-file, start-file, etc.).
		// Bubbletea runs this in a goroutine so it doesn't freeze the UI.
		ev := <-m.mpv.Events()
		return mpvEventMsg{ev: ev}
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case TickMsg:
		// Every second: reschedule the tick AND poll mpv for current position.
		return m, tea.Batch(tickCmd(), m.pollPlaybackCmd())

	case PlaybackStateMsg:
		m.playing = msg.Playing
		m.position = msg.Position
		// Don't overwrite volume here — we manage it locally to avoid
		// the value jumping around during the poll round-trip.

	case TrackStartMsg:
		m.hasTrack = true
		m.currentTrack = msg.Item
		m.statusIsErr = false
		m.statusMsg = fmt.Sprintf("playing: %s — %s [%s]",
			msg.Item.Artist, msg.Item.Title, msg.Item.Source)

	case QueueAdvanceMsg:
		// A track ended naturally — advance the queue and play the next item.
		return m, m.advanceQueueCmd()

	case mpvEventMsg:
		// We received an event from mpv. Re-issue waitForMpvEventCmd immediately
		// so we keep listening — if we forget this, future events are lost.
		var cmds []tea.Cmd
		cmds = append(cmds, m.waitForMpvEventCmd())

		switch msg.ev.Type {
		case "end-file":
			// reason "eof"  → track played to completion → advance queue
			// reason "stop" → we called stop() ourselves → don't advance
			// reason "error"→ mpv couldn't play the file → show error, advance
			switch msg.ev.Reason {
			case "eof":
				cmds = append(cmds, func() tea.Msg { return QueueAdvanceMsg{} })
			case "error":
				m.statusMsg = "mpv: error playing track, skipping"
				m.statusIsErr = true
				cmds = append(cmds, func() tea.Msg { return QueueAdvanceMsg{} })
			}
			// "stop" and "quit" → do nothing, user triggered it

		case "start-file":
			// mpv started loading a new file — clear any stale error.
			m.statusIsErr = false
		}

		return m, tea.Batch(cmds...)

	case ErrorMsg:
		m.statusMsg = msg.Err.Error()
		m.statusIsErr = true

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// ── Key handling ──────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searchMode {
		return m.handleSearchKey(msg)
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
		return m, m.nextTrackCmd()
	case "p":
		return m, m.prevTrackCmd()
	case "right", "l":
		return m, m.seekCmd(+10)
	case "left", "h":
		return m, m.seekCmd(-10)
	case "+", "=":
		return m, m.volumeCmd(+5)
	case "-":
		return m, m.volumeCmd(-5)

	case "/":
		if m.activeTab == tabSearch {
			m.searchMode = true
			m.searchQuery = ""
		}

	case "enter":
		if m.activeTab == tabQueue {
			return m, m.playCurrentQueueItemCmd()
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
	case "enter":
		m.searchMode = false
		// TODO M3: trigger source search
	default:
		if len(msg.String()) == 1 {
			m.searchQuery += msg.String()
		}
	}
	return m, nil
}

// ── Commands ──────────────────────────────────────────────────────────────────

// pollPlaybackCmd asks the active player for its current position.
// Runs every second via TickMsg. Returns nil (no-op) when nothing is playing.
func (m Model) pollPlaybackCmd() tea.Cmd {
	return func() tea.Msg {
		pos, playing, err := m.router.Position(m.ctx)
		if err != nil {
			return nil // transient poll errors are expected; don't spam the status bar
		}
		return PlaybackStateMsg{
			Playing:  playing,
			Position: time.Duration(pos * float64(time.Second)),
			Volume:   m.volume,
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

func (m Model) volumeCmd(delta int) tea.Cmd {
	newVol := m.volume + delta
	if newVol < 0 {
		newVol = 0
	}
	if newVol > 100 {
		newVol = 100
	}
	return func() tea.Msg {
		if err := m.router.SetVolume(m.ctx, newVol); err != nil {
			return ErrorMsg{err}
		}
		// Update volume locally immediately — don't wait for the next poll tick.
		return PlaybackStateMsg{Playing: m.playing, Position: m.position, Volume: newVol}
	}
}

func (m Model) playCurrentQueueItemCmd() tea.Cmd {
	return func() tea.Msg {
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

func (m Model) advanceQueueCmd() tea.Cmd {
	return func() tea.Msg {
		item, ok := m.queue.Next()
		if !ok {
			// End of queue — update status bar but don't error.
			return TrackStartMsg{Item: queue.Item{
				Title:  "End of queue",
				Artist: "Add more tracks with [2] Search",
			}}
		}
		if err := m.router.Play(m.ctx, item); err != nil {
			return ErrorMsg{err}
		}
		return TrackStartMsg{Item: item}
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

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
