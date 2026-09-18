package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/pseud039/termix/internal/lyrics"
	"github.com/pseud039/termix/internal/queue"
)

var (
	colBg          = lipgloss.Color("#1a1a1a")
	colSurface     = lipgloss.Color("#242424")
	colBorder      = lipgloss.Color("#3a3a3a")
	colMuted       = lipgloss.Color("#666666")
	colText        = lipgloss.Color("#d4d4d4")
	colAccent      = lipgloss.Color("#98c379")
	colAccent2     = lipgloss.Color("#61afef")
	colWarning     = lipgloss.Color("#e5c07b")
	colDanger      = lipgloss.Color("#e06c75")
	colTabActive   = lipgloss.Color("#98c379")
	colTabInactive = lipgloss.Color("#555555")
)

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
		Render("[1-3] tabs  [space] play/pause  [n/p] next/prev  [z/r] shuffle/repeat  [q] quit")

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

func (m Model) renderPlayerBar() string {
	w := m.width

	var track, artist string
	if m.hasTrack {
		track = m.currentTrack.Title
		artist = m.currentTrack.Artist
	} else {
		track = "Nothing playing"
		artist = "[2] to search"
	}

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

	playIcon := "▶"
	if m.playing {
		playIcon = "⏸"
	}

	// Track length: prefer what the backend reported (polled every tick),
	// fall back to whatever the queue item carried.
	dur := m.duration
	if dur == 0 {
		dur = m.currentTrack.Duration
	}

	var progressBar string
	if m.hasTrack && dur > 0 {
		pct := float64(m.position) / float64(dur)
		progressBar = renderProgressBar(32, pct)
	} else {
		progressBar = renderProgressBar(32, 0)
	}

	timeStr := fmt.Sprintf("%s / %s",
		formatDuration(m.position),
		formatLength(dur),
	)

	volStr := "vol --"
	if m.volume >= 0 {
		volStr = fmt.Sprintf("vol %d%%", m.volume)
	}

	left := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Foreground(colAccent).Bold(true).PaddingRight(2).Render(playIcon),
		lipgloss.NewStyle().Foreground(colText).Bold(true).PaddingRight(1).Render(truncate(track, 28)),
		lipgloss.NewStyle().Foreground(colMuted).PaddingRight(1).Render("·"),
		lipgloss.NewStyle().Foreground(colMuted).PaddingRight(2).Render(truncate(artist, 22)),
		badge,
	)

	center := lipgloss.JoinHorizontal(lipgloss.Center,
		progressBar,
		lipgloss.NewStyle().Foreground(colMuted).PaddingLeft(2).Render(timeStr),
	)

	right := lipgloss.JoinHorizontal(lipgloss.Center,
		lipgloss.NewStyle().Foreground(colAccent2).Render(volStr),
		lipgloss.NewStyle().PaddingLeft(2).Render(m.renderModeBadges()),
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

// renderModeBadges shows the shuffle and repeat state: lit in the accent
// colour when on, dimmed when off.
func (m Model) renderModeBadges() string {
	on := lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	off := lipgloss.NewStyle().Foreground(colMuted)

	var shuffle string
	switch m.queue.ShuffleMode() {
	case queue.ShuffleOn:
		shuffle = on.Render("⇄ shuffle")
	case queue.ShuffleSmart:
		shuffle = on.Render("⇄ smart")
	default:
		shuffle = off.Render("⇄")
	}

	var repeat string
	switch m.queue.Repeat() {
	case queue.RepeatAll:
		repeat = on.Render("↻ all")
	case queue.RepeatOne:
		repeat = on.Render("↻ one")
	default:
		repeat = off.Render("↻")
	}

	return shuffle + "  " + repeat
}

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
		title := item.Title
		if item.Recommended {
			// Smart-shuffle pick, not something the user queued.
			title = "✦ " + title
		}
		title = truncate(title, 36)
		artist := truncate(item.Artist, 24)
		dur := formatLength(item.Duration)

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

	hint := styleMuted.Render("[/] type  [s/y/l] mode  [tab] mode while typing  [↑/↓] select  [enter] add")

	b.WriteString("\n")
	b.WriteString(promptStyle.Render("  Search  "))
	b.WriteString(m.renderSearchModes())
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

	switch {
	case m.searching:
		b.WriteString(styleMuted.Render("  Searching…"))
	case m.searchErr != nil:
		b.WriteString(lipgloss.NewStyle().Foreground(colDanger).Render("  Search failed: " + m.searchErr.Error()))
	case len(m.searchResults) > 0:
		b.WriteString(m.renderSearchResults(height - lipgloss.Height(b.String())))
	case m.searchSeq > 0 && !m.searchMode:
		b.WriteString(styleMuted.Render("  No results"))
	case m.searchers[m.searchSource] == nil && !m.searchMode:
		b.WriteString(styleMuted.Render("  " + searchUnavailableHint(m.searchSource)))
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Height(height).
		Render(b.String())
}

// renderSearchModes draws the source labels, highlighting the active one in
// the same colour as its queue badge.
func (m Model) renderSearchModes() string {
	labels := map[queue.SourceType]string{
		queue.SourceSpotify: "Spotify",
		queue.SourceYouTube: "YouTube",
		queue.SourceLocal:   "Local",
	}
	colors := map[queue.SourceType]lipgloss.Color{
		queue.SourceSpotify: colAccent,
		queue.SourceYouTube: colDanger,
		queue.SourceLocal:   colAccent2,
	}
	var parts []string
	for _, src := range searchSources {
		if src == m.searchSource {
			parts = append(parts, lipgloss.NewStyle().
				Foreground(colors[src]).
				Bold(true).
				Render("["+strings.ToUpper(labels[src])+"]"))
		} else {
			parts = append(parts, styleMuted.Render(labels[src]))
		}
	}
	return strings.Join(parts, "  ")
}

// renderSearchResults lists search results with the cursor row
// highlighted, scrolling so the cursor stays visible within height rows.
func (m Model) renderSearchResults(height int) string {
	if height < 1 {
		height = 1
	}
	start := 0
	if m.searchCursor >= height {
		start = m.searchCursor - height + 1
	}
	end := start + height
	if end > len(m.searchResults) {
		end = len(m.searchResults)
	}

	var rows []string
	for i := start; i < end; i++ {
		item := m.searchResults[i]
		row := fmt.Sprintf("%-36s  %-24s  %s",
			truncate(item.Title, 36), truncate(item.Artist, 24), formatLength(item.Duration))
		if i == m.searchCursor {
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
	}
	return strings.Join(rows, "\n")
}

func (m Model) renderLyrics(height int) string {
	frame := lipgloss.NewStyle().Width(m.width).Height(height)

	// centered draws a short message in the middle of the pane, the way
	// the empty queue does.
	centered := func(msg string, color lipgloss.Color) string {
		return frame.Render(lipgloss.NewStyle().
			Foreground(color).
			Width(m.width).
			Align(lipgloss.Center).
			PaddingTop(height / 3).
			Render(msg))
	}

	if !m.hasTrack {
		return centered("Nothing playing\n\nPress [2] to search and add tracks", colMuted)
	}

	var tag string
	switch {
	case m.lyricsLoading:
		tag = "fetching…"
	case m.lyrics.IsSynced():
		tag = "synced"
	case m.lyrics != nil && m.lyrics.Instrumental && m.lyrics.Plain == "":
		tag = "instrumental"
	case m.lyrics != nil:
		tag = "plain"
	}
	title := lipgloss.NewStyle().Foreground(colText).Bold(true).
		Render(truncate(m.currentTrack.Artist+" — "+m.currentTrack.Title, m.width-24))
	if tag != "" {
		title += styleMuted.Render("  [" + tag + "]")
	}
	if m.lyricsOffset != 0 {
		title += lipgloss.NewStyle().Foreground(colWarning).Render("  offset " + formatOffset(m.lyricsOffset))
	}
	hint := styleMuted.Render("[j/k] move  [enter] seek to line  [esc] follow  [ [ ] ] offset ±0.5s")

	head := "\n  " + title + "\n  " + hint + "\n\n"
	bodyHeight := height - lipgloss.Height(head)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	switch {
	case m.lyricsLoading:
		body = styleMuted.Render("  Fetching lyrics…")
	case errors.Is(m.lyricsErr, lyrics.ErrNotFound):
		body = styleMuted.Render(fmt.Sprintf("  No lyrics on lrclib.net for %s — %s", m.currentTrack.Artist, m.currentTrack.Title))
	case m.lyricsErr != nil:
		body = lipgloss.NewStyle().Foreground(colDanger).Render("  Lyrics failed: " + m.lyricsErr.Error())
	case m.lyrics == nil:
		body = "" // a track just started and no fetch was issued
	case m.lyrics.IsSynced():
		body = m.renderSyncedLyrics(bodyHeight)
	case m.lyrics.Plain != "":
		body = m.renderPlainLyrics(bodyHeight)
	default:
		body = lipgloss.NewStyle().Foreground(colMuted).Width(m.width).Align(lipgloss.Center).
			PaddingTop(bodyHeight / 3).Render("♪ instrumental")
	}

	return frame.Render(head + body)
}

// lyricsWindow returns the [start, end) range of n lines that keeps center
// in the middle of a view height rows tall, clamped to the ends.
func lyricsWindow(n, height, center int) (int, int) {
	if n <= height {
		return 0, n
	}
	start := center - height/2
	if start < 0 {
		start = 0
	}
	if start > n-height {
		start = n - height
	}
	return start, start + height
}

// renderSyncedLyrics shows the lines around the one being sung: past
// lines dimmed, the active one bold in the accent colour, upcoming lines
// in the normal text colour. A manual cursor (j/k) takes over centering
// and gets the same highlight the search results use.
func (m Model) renderSyncedLyrics(height int) string {
	lines := m.lyrics.Synced
	active := m.lyrics.ActiveLine(m.playbackPos() - m.lyricsOffset)
	center := active
	if m.lyricsCursor >= 0 {
		center = m.lyricsCursor
	}
	if center < 0 {
		center = 0
	}
	start, end := lyricsWindow(len(lines), height, center)

	past := styleMuted
	current := lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	upcoming := styleText
	cursor := lipgloss.NewStyle().Background(colSurface).Width(m.width - 2)

	var rows []string
	for i := start; i < end; i++ {
		text := lines[i].Text
		style := upcoming
		switch {
		case i == active:
			style = current
		case i < active:
			style = past
		}
		if text == "" {
			text = "♪"
			if i != active {
				style = past
			}
		}
		row := "  " + style.Render(truncate(text, m.width-4))
		if i == m.lyricsCursor {
			row = cursor.Render(row)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

// renderPlainLyrics scrolls untimed lyrics with the cursor as the
// position; nothing is highlighted since there is no timing to follow.
func (m Model) renderPlainLyrics(height int) string {
	lines := strings.Split(m.lyrics.Plain, "\n")
	center := m.lyricsCursor
	if center < 0 {
		center = 0
	}
	start, end := lyricsWindow(len(lines), height, center)

	cursor := lipgloss.NewStyle().Background(colSurface).Width(m.width - 2)
	var rows []string
	for i := start; i < end; i++ {
		row := "  " + styleText.Render(truncate(strings.TrimRight(lines[i], "\r"), m.width-4))
		if i == m.lyricsCursor {
			row = cursor.Render(row)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

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

// formatLength formats a track length, showing "--:--" when it isn't known.
func formatLength(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	return formatDuration(d)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
