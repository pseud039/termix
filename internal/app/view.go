package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/pseud039/termix/internal/queue"
)

// ── Palette ──────────────────────────────────────────────────────────────────
// Going for a dark terminal aesthetic: dim charcoal bg, green accent,
// muted grey secondary. Inspired by rmpc's dense info layout.

var (
	colBg       = lipgloss.Color("#1a1a1a")
	colSurface  = lipgloss.Color("#242424")
	colBorder   = lipgloss.Color("#3a3a3a")
	colMuted    = lipgloss.Color("#666666")
	colText     = lipgloss.Color("#d4d4d4")
	colAccent   = lipgloss.Color("#98c379") // green
	colAccent2  = lipgloss.Color("#61afef") // blue for secondary info
	colWarning  = lipgloss.Color("#e5c07b")
	colDanger   = lipgloss.Color("#e06c75")
	colTabActive   = lipgloss.Color("#98c379")
	colTabInactive = lipgloss.Color("#555555")
)

// ── Base styles ───────────────────────────────────────────────────────────────

var (
	styleBase = lipgloss.NewStyle().
		Background(colBg).
		Foreground(colText)

	styleBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colBorder)

	styleAccent = lipgloss.NewStyle().
		Foreground(colAccent).
		Bold(true)

	styleMuted = lipgloss.NewStyle().
		Foreground(colMuted)

	styleText = lipgloss.NewStyle().
		Foreground(colText)
)

// ── Header (tab bar) ──────────────────────────────────────────────────────────

func (m Model) renderHeader() string {
	tabs := make([]string, len(tabNames))
	for i, name := range tabNames {
		if tab(i) == m.activeTab {
			tabs[i] = lipgloss.NewStyle().
				Foreground(colTabActive).
				Bold(true).
				Padding(0, 2).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(colTabActive).
				Render(name)
		} else {
			tabs[i] = lipgloss.NewStyle().
				Foreground(colTabInactive).
				Padding(0, 2).
				Render(name)
		}
	}

	tabRow := lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)

	title := lipgloss.NewStyle().
		Foreground(colAccent).
		Bold(true).
		PaddingRight(4).
		Render("▶ termix")

	right := lipgloss.NewStyle().
		Foreground(colMuted).
		Render("[1-3] tabs  [space] play/pause  [n/p] next/prev  [q] quit")

	gap := m.width - lipgloss.Width(title) - lipgloss.Width(tabRow) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}

	row := lipgloss.JoinHorizontal(lipgloss.Bottom,
		title,
		tabRow,
		strings.Repeat(" ", gap),
		right,
	)

	return lipgloss.NewStyle().
		Background(colSurface).
		Width(m.width).
		Render(row)
}

// ── Player bar ────────────────────────────────────────────────────────────────

func (m Model) renderPlayerBar() string {
	w := m.width

	var track, artist string
	if m.hasTrack {
		track = m.currentTrack.Title
		artist = m.currentTrack.Artist
	} else {
		track = "No track"
		artist = "—"
	}

	// Source badge
	var badge string
	if m.hasTrack {
		switch m.currentTrack.Source {
		case queue.SourceSpotify:
			badge = lipgloss.NewStyle().Foreground(colAccent).Render("[spotify]")
		case queue.SourceYouTube:
			badge = lipgloss.NewStyle().Foreground(colDanger).Render("[youtube]")
		case queue.SourceLocal:
			badge = lipgloss.NewStyle().Foreground(colAccent2).Render("[local]")
		}
	}

	// Play state indicator
	playIcon := "▶"
	if m.playing {
		playIcon = "⏸"
	}

	// Progress bar
	var progressBar string
	if m.hasTrack && m.currentTrack.Duration > 0 {
		pct := float64(m.position) / float64(m.currentTrack.Duration)
		progressBar = renderProgressBar(32, pct)
	} else {
		progressBar = renderProgressBar(32, 0)
	}

	// Time display
	timeStr := fmt.Sprintf("%s / %s",
		formatDuration(m.position),
		formatDuration(m.currentTrack.Duration),
	)

	// Volume
	volStr := fmt.Sprintf("vol %d%%", m.volume)

	// Left section: icon + track info + badge
	left := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Foreground(colAccent).Bold(true).PaddingRight(2).Render(playIcon),
		lipgloss.NewStyle().Foreground(colText).Bold(true).PaddingRight(1).Render(truncate(track, 28)),
		lipgloss.NewStyle().Foreground(colMuted).PaddingRight(1).Render("·"),
		lipgloss.NewStyle().Foreground(colMuted).PaddingRight(2).Render(truncate(artist, 22)),
		badge,
	)

	// Center: progress + time
	center := lipgloss.JoinHorizontal(lipgloss.Center,
		progressBar,
		lipgloss.NewStyle().Foreground(colMuted).PaddingLeft(2).Render(timeStr),
	)

	// Right: volume + hint
	right := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Foreground(colAccent2).Render(volStr),
		lipgloss.NewStyle().Foreground(colMuted).PaddingLeft(2).Render("[±] vol  [←→] seek"),
	)

	lw := lipgloss.Width(left)
	cw := lipgloss.Width(center)
	rw := lipgloss.Width(right)

	pad1 := (w/2 - lw - cw/2)
	if pad1 < 1 {
		pad1 = 1
	}
	pad2 := w - lw - pad1 - cw - rw
	if pad2 < 1 {
		pad2 = 1
	}

	row := lipgloss.JoinHorizontal(lipgloss.Center,
		left,
		strings.Repeat(" ", pad1),
		center,
		strings.Repeat(" ", pad2),
		right,
	)

	return lipgloss.NewStyle().
		Background(colSurface).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colBorder).
		Width(w).
		Padding(0, 1).
		Render(row)
}

// ── Status bar ────────────────────────────────────────────────────────────────

func (m Model) renderStatus() string {
	msg := m.statusMsg
	if msg == "" {
		msg = "ready"
	}
	style := styleMuted
	if m.statusIsErr {
		style = lipgloss.NewStyle().Foreground(colDanger)
	}
	return lipgloss.NewStyle().
		Background(colBg).
		Width(m.width).
		PaddingLeft(1).
		Render(style.Render(msg))
}

// ── Content panes ─────────────────────────────────────────────────────────────

func (m Model) renderContent(height int) string {
	switch m.activeTab {
	case tabQueue:
		return m.renderQueue(height)
	case tabSearch:
		return m.renderSearch(height)
	case tabLyrics:
		return m.renderLyrics(height)
	}
	return ""
}

func (m Model) renderQueue(height int) string {
	items := m.queue.Items()
	current := m.queue.CurrentIndex()

	var rows []string

	header := lipgloss.NewStyle().
		Foreground(colMuted).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colBorder).
		Width(m.width - 2).
		PaddingLeft(1).
		Render(fmt.Sprintf("%-4s  %-36s  %-24s  %-12s  %s",
			"#", "Title", "Artist", "Duration", "Source"))
	rows = append(rows, header)

	if len(items) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(colMuted).
			Width(m.width).
			Align(lipgloss.Center).
			PaddingTop(height / 3).
			Render("Queue is empty\n\nPress [2] to search and add tracks")
		return empty
	}

	for i, item := range items {
		isActive := i == current

		num := lipgloss.NewStyle().Foreground(colMuted).Render(fmt.Sprintf("%-4d", i+1))
		title := truncate(item.Title, 36)
		artist := truncate(item.Artist, 24)
		dur := formatDuration(item.Duration)

		var src string
		switch item.Source {
		case queue.SourceSpotify:
			src = lipgloss.NewStyle().Foreground(colAccent).Render("spotify")
		case queue.SourceYouTube:
			src = lipgloss.NewStyle().Foreground(colDanger).Render("youtube")
		case queue.SourceLocal:
			src = lipgloss.NewStyle().Foreground(colAccent2).Render("local")
		}

		row := fmt.Sprintf("%s  %-36s  %-24s  %-12s  %s",
			num, title, artist, dur, src)

		if isActive {
			row = lipgloss.NewStyle().
				Foreground(colAccent).
				Bold(true).
				Background(colSurface).
				Width(m.width - 2).
				PaddingLeft(1).
				Render("▶ " + row)
		} else {
			row = lipgloss.NewStyle().
				Foreground(colText).
				Width(m.width - 2).
				PaddingLeft(1).
				Render("  " + row)
		}

		rows = append(rows, row)

		// Don't render more rows than visible height.
		if len(rows) >= height-2 {
			break
		}
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Height(height).
		Render(strings.Join(rows, "\n"))
}

func (m Model) renderSearch(height int) string {
	var b strings.Builder

	promptStyle := lipgloss.NewStyle().
		Foreground(colAccent).
		Bold(true)

	inputStyle := lipgloss.NewStyle().
		Foreground(colText).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colAccent).
		Width(m.width - 4)

	hint := styleMuted.Render("Press [/] to search across Spotify, YouTube, and local files")

	b.WriteString("\n")
	b.WriteString(promptStyle.Render("  Search  "))
	b.WriteString("\n\n")

	query := m.searchQuery
	if m.searchMode {
		query += "█" // block cursor
	}
	b.WriteString("  ")
	b.WriteString(inputStyle.Render(query))
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(hint)
	b.WriteString("\n\n")

	if !m.searchMode {
		b.WriteString(styleMuted.Render("  Source adapters available after running `termix auth`.\n"))
		b.WriteString(styleMuted.Render("  Local files: set music_dir in ~/.config/termix/config.toml\n"))
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Height(height).
		Render(b.String())
}

func (m Model) renderLyrics(height int) string {
	body := lipgloss.NewStyle().
		Foreground(colMuted).
		Width(m.width).
		Align(lipgloss.Center).
		PaddingTop(height / 3).
		Render("Lyrics sync coming in M5\n\nPowered by lrclib.net")

	return lipgloss.NewStyle().
		Width(m.width).
		Height(height).
		Render(body)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func renderProgressBar(width int, pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(float64(width) * pct)
	bar := strings.Repeat("━", filled) + strings.Repeat("─", width-filled)
	return lipgloss.NewStyle().Foreground(colAccent).Render(bar)
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	d = d.Round(time.Second)
	m := d / time.Minute
	s := (d % time.Minute) / time.Second
	return fmt.Sprintf("%d:%02d", m, s)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}