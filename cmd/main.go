package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pseud039/termix/internal/app"
	"github.com/pseud039/termix/internal/lastfm"
	"github.com/pseud039/termix/internal/local"
	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
	"github.com/pseud039/termix/internal/recommend"
	spotifyclient "github.com/pseud039/termix/internal/spotify"
	"github.com/pseud039/termix/internal/youtube"
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
	// Only sources whose provider was created go in the map; the Search tab
	// shows how to enable the rest.
	searchers := map[queue.SourceType]app.Searcher{}
	if spClient, err := spotifyclient.NewClient(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "note: Spotify not connected (%v)\n", err)
	} else {
		router.SetSpotifyPlayer(player.NewSpotifyPlayer(spClient))
		searchers[queue.SourceSpotify] = spotifyclient.NewSearchProvider(spClient)
	}
	if yt, err := youtube.NewSearchProvider(); err != nil {
		fmt.Fprintf(os.Stderr, "note: YouTube search off (%v)\n", err)
	} else {
		searchers[queue.SourceYouTube] = yt
	}
	searchers[queue.SourceLocal] = local.NewSearchProvider(musicDir())

	// Smart shuffle needs a Last.fm key. Without one the z key skips the
	// smart mode and the status line says how to enable it.
	var recommender *recommend.Recommender
	if lfm, err := lastfm.NewClient(); err != nil {
		fmt.Fprintf(os.Stderr, "note: smart shuffle off (%v)\n", err)
	} else {
		resolvers := map[queue.SourceType]recommend.Searcher{}
		for src, s := range searchers {
			resolvers[src] = s
		}
		recommender = recommend.New(lfm, resolvers)
	}

	// A failed mpv start is not fatal: the TUI still runs and shows the error
	// in the status line; only local/YouTube playback is unavailable.
	mpvErr := mpv.Start(ctx)
	if mpvErr != nil {
		fmt.Fprintf(os.Stderr, "warning: %v\n", mpvErr)
	}

	model := app.New(ctx, q, router, mpv, mpvErr, searchers, recommender)

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

// musicDir is where local search looks: TERMIX_MUSIC_DIR, else ~/Music.
func musicDir() string {
	if d := os.Getenv("TERMIX_MUSIC_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "Music"
	}
	return filepath.Join(home, "Music")
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
