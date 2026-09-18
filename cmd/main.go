package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

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

	// `termix auth` runs the one-time browser OAuth flow and exits without
	// launching the TUI.
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

	// Spotify is optional: without a saved login, Spotify items fail with a
	// "run `termix auth` first" error and everything else still works.
	var spotifySearch *spotifyclient.SearchProvider
	if spClient, err := spotifyclient.NewClient(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "note: Spotify not connected (%v)\n", err)
	} else {
		router.SetSpotifyPlayer(player.NewSpotifyPlayer(spClient))
		spotifySearch = spotifyclient.NewSearchProvider(spClient)
	}
	_ = spotifySearch // wired into the Search pane once that lands

	loadDemoQueue(q)
	q.JumpTo(0)

	// A failed mpv start is not fatal: the TUI still runs and shows the error
	// in the status line; only local/YouTube playback is unavailable.
	mpvErr := mpv.Start(ctx)
	mpvReady := mpvErr == nil
	if mpvErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", mpvErr)
	}

	// Auto-play only mpv-backed items; Spotify needs a device and a login.
	if mpvReady {
		if item, ok := q.Current(); ok {
			if item.Source == queue.SourceLocal || item.Source == queue.SourceYouTube {
				if err := router.Play(ctx, item); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not start initial playback: %v\n", err)
				}
			}
		}
	}

	model := app.New(ctx, q, router, mpv, mpvErr)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),       // full-screen takeover, restores terminal on exit
		tea.WithMouseCellMotion(), // needed for future click-to-seek on the progress bar
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "termix: %v\n", err)
		os.Exit(1)
	}

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

func loadDemoQueue(q *queue.Queue) {
	q.Add(queue.Item{
		ID:       "local-1",
		Title:    "Replace me with a real path",
		Artist:   "Local File",
		Album:    "Your Music",
		Source:   queue.SourceLocal,
		URI:      "C:\\Users\\gunsh\\Music\\aae_ganpat_bjana.mp3",
		Duration: 121 * time.Second,
	})
	q.Add(queue.Item{
		ID:       "yt-1",
		Title:    "Never Gonna Give You Up",
		Artist:   "Rick Astley",
		Album:    "Whenever You Need Somebody",
		Source:   queue.SourceYouTube,
		URI:      "ytdl://dQw4w9WgXcQ",
		Duration: 0, // unknown until mpv probes the file
	})
	q.Add(queue.Item{
		ID:     "spotify-1",
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		Source: queue.SourceSpotify,
		URI:    "spotify:track:0VjIjW4GlUZAMYd2vXMi3b",
	})
}
