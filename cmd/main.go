package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pseud039/termix/internal/app"
	"github.com/pseud039/termix/internal/config"
	"github.com/pseud039/termix/internal/lastfm"
	"github.com/pseud039/termix/internal/local"
	"github.com/pseud039/termix/internal/lyrics"
	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
	"github.com/pseud039/termix/internal/recommend"
	spotifyclient "github.com/pseud039/termix/internal/spotify"
	"github.com/pseud039/termix/internal/youtube"
)

func main() {
	ctx := context.Background()

	// Settings come from config.toml (TERMIX_CONFIG, next to the binary,
	// or the user config dir). A missing file gets a template written; a
	// broken one is fatal so a typo can't silently disable a source.
	cfg, notes, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "termix: %v\n", err)
		os.Exit(1)
	}
	for _, n := range notes {
		fmt.Fprintf(os.Stderr, "note: %s\n", n)
	}
	if cfg.Path != "" {
		fmt.Fprintf(os.Stderr, "config: %s\n", cfg.Path)
	}
	creds := spotifyclient.Credentials{
		ClientID:     cfg.Spotify.ClientID,
		ClientSecret: cfg.Spotify.ClientSecret,
	}

	// `termix auth` runs the one-time browser OAuth flow and exits without
	// launching the TUI.
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		if err := spotifyclient.Login(ctx, creds); err != nil {
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
	if spClient, err := spotifyclient.NewClient(ctx, creds); err != nil {
		fmt.Fprintf(os.Stderr, "note: Spotify not connected (%v)\n", err)
	} else {
		router.SetSpotifyPlayer(player.NewSpotifyPlayer(spClient, cfg.Spotify.Device))
		searchers[queue.SourceSpotify] = spotifyclient.NewSearchProvider(spClient)
	}
	if yt, err := youtube.NewSearchProvider(cfg.YouTube.YTDLP); err != nil {
		fmt.Fprintf(os.Stderr, "note: YouTube search off (%v)\n", err)
	} else {
		searchers[queue.SourceYouTube] = yt
	}
	searchers[queue.SourceLocal] = local.NewSearchProvider(cfg.Local.MusicDir)

	// Smart shuffle needs a Last.fm key. Without one the z key skips the
	// smart mode and the status line says how to enable it.
	var recommender *recommend.Recommender
	if lfm, err := lastfm.NewClient(cfg.LastFM.APIKey); err != nil {
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

	// Lyrics come from lrclib.net (no key needed) and are cached on disk;
	// without a cache dir they are simply refetched each time.
	var lyricsCache *lyrics.DiskCache
	if dir, err := os.UserCacheDir(); err == nil {
		lyricsCache = lyrics.NewDiskCache(filepath.Join(dir, "termix", "lyrics"))
	}
	lyr := lyrics.NewClient(lyricsCache)

	model := app.New(ctx, q, router, mpv, mpvErr, searchers, recommender, lyr)

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
