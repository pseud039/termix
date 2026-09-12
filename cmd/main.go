package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pseud039/termix/internal/app"
	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
	spotifyclient "github.com/pseud039/termix/internal/spotify"
)

func main() {
	ctx := context.Background()
	if err := loadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not load .env: %v\n", err)
	}

	// `termix auth` runs the one-time browser OAuth flow and exits —
	// it does not launch the TUI. Run this once before anything else.
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		if err := spotifyclient.Login(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "termix auth: %v\n", err)
			os.Exit(1)
		}
		return
	}

	q := queue.New()
	mpv := player.NewMpvPlayer()
	router := player.NewRouter(mpv)

	// ── Spotify (M3) ──────────────────────────────────────────────────────────
	// If a cached login exists (from `termix auth`), wire up the Spotify
	// backend. If not, Spotify items just show router's "run `termix auth`
	// first" error when played — everything else still works.
	var spotifySearch *spotifyclient.SearchProvider
	if spClient, err := spotifyclient.NewClient(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "note: Spotify not connected (%v)\n", err)
	} else {
		router.SetSpotifyPlayer(player.NewSpotifyPlayer(spClient))
		spotifySearch = spotifyclient.NewSearchProvider(spClient)
	}
	_ = spotifySearch // wired into the Search pane once that lands

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

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		if len(value) >= 2 {
			if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) || (strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
				value = value[1 : len(value)-1]
			}
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}

	return nil
}

// loadDemoQueue populates the queue with placeholder tracks for display testing.
// Every item here has a fake URI — they won't play until you replace them.
// Keep SourceLocal and SourceYouTube first so mpvReady auto-play can fire.
func loadDemoQueue(q *queue.Queue) {
	q.Add(queue.Item{
		ID:     "local-1",
		Title:  "Replace me with a real path",
		Artist: "Local File",
		Album:  "Your Music",
		Source: queue.SourceLocal,
		URI:    "/tmp/replace-me.mp3", // ← put an actual file path here
	})
	q.Add(queue.Item{
		ID:     "yt-1",
		Title:  "Never Gonna Give You Up",
		Artist: "Rick Astley",
		Album:  "Whenever You Need Somebody",
		Source: queue.SourceYouTube,
		URI:    "ytdl://dQw4w9WgXcQ", // needs yt-dlp in PATH
	})
	q.Add(queue.Item{
		ID:     "spotify-1",
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		Source: queue.SourceSpotify,
		URI:    "spotify:track:0VjIjW4GlUZAMYd2vXMi3b", // needs auth (M3)
	})
}
