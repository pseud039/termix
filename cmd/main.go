package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pseud039/termix/internal/app"
	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
)

func main() {
	ctx := context.Background()

	// ── Wire up subsystems ────────────────────────────────────────────────────

	q := queue.New()
	mpv := player.NewMpvPlayer()
	router := player.NewRouter(mpv)

	// Pre-load the queue with some demo local items so the UI looks alive
	// even without mpv installed. Remove these once you have real sources.
	q.Add(queue.Item{
		ID:       "demo-1",
		Title:    "Bohemian Rhapsody",
		Artist:   "Queen",
		Album:    "A Night at the Opera",
		Duration: 5*time.Minute + 54*time.Second,
		Source:   queue.SourceLocal,
		URI:      "/tmp/demo.mp3", // won't exist, just for display
	})
	q.Add(queue.Item{
		ID:       "demo-2",
		Title:    "Blinding Lights",
		Artist:   "The Weeknd",
		Album:    "After Hours",
		Duration: 3*time.Minute + 20*time.Second,
		Source:   queue.SourceSpotify,
		URI:      "spotify:track:0VjIjW4GlUZAMYd2vXMi3b",
	})
	q.Add(queue.Item{
		ID:       "demo-3",
		Title:    "Never Gonna Give You Up",
		Artist:   "Rick Astley",
		Album:    "Whenever You Need Somebody",
		Duration: 3*time.Minute + 32*time.Second,
		Source:   queue.SourceYouTube,
		URI:      "ytdl://dQw4w9WgXcQ",
	})
	q.Add(queue.Item{
		ID:       "demo-4",
		Title:    "Redbone",
		Artist:   "Childish Gambino",
		Album:    "Awaken, My Love!",
		Duration: 5*time.Minute + 27*time.Second,
		Source:   queue.SourceSpotify,
		URI:      "spotify:track:0wXuerDYiBnERgIpbb88ya",
	})
	// Set cursor to first item
	q.JumpTo(0)

	// ── Try to start mpv (optional — TUI works without it) ───────────────────

	if err := mpv.Start(ctx); err != nil {
		// mpv not installed or failed to start — that's okay for now.
		// The TUI still loads, just can't actually play anything.
		_ = err
	}

	// ── Build and run the Bubbletea program ───────────────────────────────────

	model := app.New(ctx, q, router, mpv)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // full-screen takeover
		tea.WithMouseCellMotion(), // for future click support
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "termix: %v\n", err)
		os.Exit(1)
	}

	// ── Cleanup ───────────────────────────────────────────────────────────────

	mpv.Shutdown()
}