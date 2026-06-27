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

// — Messages the update loop understands —

// TickMsg fires every second to poll playback position.
type TickMsg time.Time

// PlaybackStateMsg carries a fresh snapshot from the active player.
type PlaybackStateMsg player.State

// ErrorMsg is displayed in the status bar.
type ErrorMsg struct{ Err error }

// TrackStartMsg is sent when a new track begins playing.
type TrackStartMsg struct{ Item queue.Item }

// QueueAdvanceMsg is sent when the current track ends and we should
// advance the queue to the next item.
type QueueAdvanceMsg struct{}

// — Tabs —

type tab int

const (
	tabQueue tab = iota
	tabSearch
	tabLyrics
)

var tabNames = []string{"Queue", "Search", "Lyrics"}

// — Model —

// Model is the single source of truth for the entire application.
// Bubbletea calls Init, Update, and View on this.
type Model struct {
	// Core subsystems (injected at construction)
	queue  *queue.Queue
	router *player.Router
	mpv    *player.MpvPlayer
	ctx    context.Context

	// TUI state
	activeTab   tab
	width       int
	height      int
	statusMsg   string
	statusIsErr bool

	// Playback state (refreshed every tick)
	playing      bool
	position     time.Duration
	volume       int // 0–100
	currentTrack queue.Item
	hasTrack     bool

	// Search pane state
	searchQuery  string
	searchMode   bool // true = user is typing
}

func New(ctx context.Context, q *queue.Queue, r *player.Router, mpv *player.MpvPlayer) Model {
	return Model{
		queue:  q,
		router: r,
		mpv:    mpv,
		ctx:    ctx,
		volume: 80,
	}
}

// — Init —

func (m Model) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// — Update —

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
		m.volume = msg.Volume

	case TrackStartMsg:
		m.hasTrack = true
		m.currentTrack = msg.Item
		m.statusMsg = fmt.Sprintf("Now playing: %s – %s", msg.Item.Artist, msg.Item.Title)

	case QueueAdvanceMsg:
		return m, m.advanceQueueCmd()

	case ErrorMsg:
		m.statusMsg = msg.Err.Error()
		m.statusIsErr = true

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If user is typing a search query, intercept most keys.
	if m.searchMode {
		return m.handleSearchKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	// Tab switching
	case "1":
		m.activeTab = tabQueue
	case "2":
		m.activeTab = tabSearch
	case "3":
		m.activeTab = tabLyrics

	// Playback controls
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

	// Search mode entry
	case "/":
		if m.activeTab == tabSearch {
			m.searchMode = true
			m.searchQuery = ""
		}

	// Queue: play selected item
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
		// TODO: trigger actual search in M3+ when source adapters are wired
	default:
		if len(msg.String()) == 1 {
			m.searchQuery += msg.String()
		}
	}
	return m, nil
}

// — Commands (side effects) —

func (m Model) pollPlaybackCmd() tea.Cmd {
	return func() tea.Msg {
		pos, playing, err := m.router.Position(m.ctx)
		if err != nil {
			return nil // silent — poll errors are transient
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
			return nil
		}
		if err := m.router.Play(m.ctx, item); err != nil {
			return ErrorMsg{err}
		}
		return TrackStartMsg{Item: item}
	}
}

// — View —

func (m Model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	header := m.renderHeader()
	playerBar := m.renderPlayerBar()
	statusBar := m.renderStatus()
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(playerBar) - lipgloss.Height(statusBar)
	content := m.renderContent(contentHeight)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		content,
		playerBar,
		statusBar,
	)
}