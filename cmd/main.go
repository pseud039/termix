package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pseud039/termix/internal/app"
	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
)

func main() {
	ctx := context.Background()

	q := queue.New()
	mpv := player.NewMpvPlayer()
	router := player.NewRouter(mpv)

	// ── Load the queue ────────────────────────────────────────────────────────
	// Replace these items with your own local files for M2 testing.
	// The URI for a local file is just the absolute path to the file.
	// mpv plays MP3, FLAC, OGG, WAV, AAC — anything ffmpeg handles.
	//
	// To test auto-advance: add two short files and let the first one finish.
	// The "end-file" event will fire → QueueAdvanceMsg → second track starts.
	//
	// Example real entries (uncomment and set your own paths):
	//
	//   q.Add(queue.Item{
	//       ID:     "local-1",
	//       Title:  "Track One",
	//       Artist: "Artist",
	//       Source: queue.SourceLocal,
	//       URI:    "/home/you/music/track1.mp3",
	//   })
	//   q.Add(queue.Item{
	//       ID:     "yt-1",
	//       Title:  "Never Gonna Give You Up",
	//       Artist: "Rick Astley",
	//       Source: queue.SourceYouTube,
	//       URI:    "ytdl://dQw4w9WgXcQ",  // needs yt-dlp installed
	//   })
	//
	// For now, the demo items below are display-only placeholders.
	// Swap the URIs for real paths and they'll play immediately.
	loadDemoQueue(q)
	q.JumpTo(0)

	// ── Start mpv ─────────────────────────────────────────────────────────────
	// mpv.Start() checks PATH for the mpv binary and connects to its IPC socket.
	// If it fails we still launch the TUI — you can browse the queue, but
	// playback controls will return errors until mpv is available.
	mpvReady := true
	if err := mpv.Start(ctx); err != nil {
		// Print the error above the TUI so the user knows what happened.
		// We don't exit — the rest of the UI still works.
		fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		mpvReady = false
	}

	// ── If mpv is ready and queue has a local/YouTube item, start playing ─────
	// This gives you immediate audio feedback when you run the binary.
	// For Spotify items, playback requires auth (M3) so we skip those here.
	if mpvReady {
		if item, ok := q.Current(); ok {
			if item.Source == queue.SourceLocal || item.Source == queue.SourceYouTube {
				if err := router.Play(ctx, item); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not start initial playback: %v\n", err)
				}
			}
		}
	}

	// ── Build and run the Bubbletea program ───────────────────────────────────

	model := app.New(ctx, q, router, mpv, mpvReady)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // full-screen takeover, restores terminal on exit
		tea.WithMouseCellMotion(), // needed for future click-to-seek on the progress bar
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "termix: %v\n", err)
		os.Exit(1)
	}

	// ── Cleanup ───────────────────────────────────────────────────────────────
	// Kill the mpv subprocess and remove the IPC socket file.
	// This runs after the TUI exits — terminal is already restored by WithAltScreen.
	mpv.Shutdown()
}

// loadDemoQueue populates the queue with placeholder tracks for display testing.
// Every item here has a fake URI — they won't play until you replace them.
// Keep SourceLocal and SourceYouTube first so mpvReady auto-play can fire.
// func loadDemoQueue(q *queue.Queue) {
// 	q.Add(queue.Item{
// 		ID:     "local-1",
// 		Title:  "Replace me with a real path",
// 		Artist: "Local File",
// 		Album:  "Your Music",
// 		Source: queue.SourceLocal,
// 		URI:    "/tmp/replace-me.mp3", // ← put an actual file path here
// 	})
// 	q.Add(queue.Item{
// 		ID:     "yt-1",
// 		Title:  "Never Gonna Give You Up",
// 		Artist: "Rick Astley",
// 		Album:  "Whenever You Need Somebody",
// 		Source: queue.SourceYouTube,
// 		URI:    "ytdl://dQw4w9WgXcQ", // needs yt-dlp in PATH
// 	})
// 	q.Add(queue.Item{
// 		ID:     "spotify-1",
// 		Title:  "Blinding Lights",
// 		Artist: "The Weeknd",
// 		Album:  "After Hours",
// 		Source: queue.SourceSpotify,
// 		URI:    "spotify:track:0VjIjW4GlUZAMYd2vXMi3b", // needs auth (M3)
// 	})
// }

func loadDemoQueue(q *queue.Queue) {
	q.Add(queue.Item{
		ID:     "test-1",
		Title:  "Test Tone 440Hz",
		Artist: "ffmpeg",
		Source: queue.SourceLocal,
		URI:    "/tmp/t1.mp3",
	})
	q.Add(queue.Item{
		ID:     "test-2",
		Title:  "Test Tone 528Hz",
		Artist: "ffmpeg",
		Source: queue.SourceLocal,
		URI:    "/tmp/t2.mp3",
	})
}
