// Package config loads Termix settings from config.toml. It is the only
// package that reads settings from the environment: every other package
// receives the values it needs as plain arguments.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// FileName is the config file's base name wherever it lives.
const FileName = "config.toml"

// EnvPath names an environment variable that points at a config file and
// takes precedence over every other location.
const EnvPath = "TERMIX_CONFIG"

// Config holds every user setting. All fields are optional: an empty
// Config runs Termix with local playback only.
type Config struct {
	Spotify Spotify `toml:"spotify"`
	LastFM  LastFM  `toml:"lastfm"`
	Local   Local   `toml:"local"`
	YouTube YouTube `toml:"youtube"`

	// Path is the file the values came from. It is empty when no file
	// existed (a template was written, or could not be) and only the
	// environment and defaults applied.
	Path string `toml:"-"`
}

type Spotify struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	// Device is the exact Spotify Connect device name; empty auto-detects
	// spotifyd.
	Device string `toml:"device"`
}

type LastFM struct {
	APIKey string `toml:"api_key"`
}

type Local struct {
	// MusicDir is the folder Local search scans; empty means ~/Music.
	MusicDir string `toml:"music_dir"`
}

type YouTube struct {
	// YTDLP is the yt-dlp executable; empty means look on PATH.
	YTDLP string `toml:"ytdlp"`
}

// Template is written to the user config dir on first run and is also the
// content of config.example.toml in the repo.
const Template = `# Termix configuration.
#
# Termix looks for this file at, in order:
#   1. the path in the TERMIX_CONFIG environment variable
#   2. config.toml next to the termix executable
#   3. <user config dir>/termix/config.toml
#      (~/.config/termix on Linux, %AppData%\termix on Windows,
#       ~/Library/Application Support/termix on macOS)
#
# The environment variable named next to each key overrides its value.
# On Windows write paths in single quotes, e.g. 'C:\Users\me\Music', so the
# backslashes are kept as-is.

[spotify]
# Client ID and secret from https://developer.spotify.com/dashboard.
# Needed for Spotify search and playback. After filling them in, run
# ` + "`termix auth`" + ` once to log in.
# Env override: SPOTIFY_ID, SPOTIFY_SECRET
client_id = ""
client_secret = ""
# Optional. Termix auto-detects the spotifyd Connect device; set this to an
# exact device name only if it picks the wrong one (e.g. your phone).
# Env override: TERMIX_SPOTIFY_DEVICE
device = ""

[lastfm]
# Optional. Last.fm API key for smart shuffle (similar tracks are pulled from
# Last.fm and looked up on your sources). Free: https://www.last.fm/api/account/create
# Env override: LASTFM_API_KEY
api_key = ""

[local]
# Optional. Folder that Local search scans (default: your home Music folder).
# Env override: TERMIX_MUSIC_DIR
music_dir = ""

[youtube]
# Optional. Path to yt-dlp if it isn't on PATH (needed for YouTube search).
# Env override: TERMIX_YTDLP
ytdlp = ""
`

// DefaultPath is where Termix writes the template and looks last:
// <user config dir>/termix/config.toml, next to spotify_token.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "termix", FileName), nil
}

// candidates lists the places a config file may live, most specific first.
func candidates() []string {
	var paths []string
	if p := strings.TrimSpace(os.Getenv(EnvPath)); p != "" {
		paths = append(paths, p)
	}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), FileName))
	}
	if p, err := DefaultPath(); err == nil {
		paths = append(paths, p)
	}
	return paths
}

// Load finds the config file, writes a template when there is none, and
// applies environment overrides and defaults. The returned notes are
// human-readable messages for the caller to print (template written,
// unknown keys). The error is non-nil only when an existing file cannot be
// read or parsed; a missing file is never an error.
func Load() (*Config, []string, error) {
	templatePath, err := DefaultPath()
	if err != nil {
		templatePath = ""
	}
	return loadFrom(candidates(), templatePath)
}

func loadFrom(paths []string, templatePath string) (*Config, []string, error) {
	cfg := &Config{}
	var notes []string

	found := ""
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			found = p
			break
		}
	}

	switch {
	case found != "":
		meta, err := toml.DecodeFile(found, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", found, err)
		}
		cfg.Path = found
		for _, key := range meta.Undecoded() {
			notes = append(notes, fmt.Sprintf("%s: unknown key %s (ignored)", found, key))
		}
	case templatePath != "":
		if err := writeTemplate(templatePath); err != nil {
			notes = append(notes, fmt.Sprintf("could not write config template to %s: %v", templatePath, err))
		} else {
			notes = append(notes, fmt.Sprintf("wrote config template to %s — put your Spotify credentials there", templatePath))
		}
	}

	cfg.applyEnv()
	cfg.applyDefaults()
	return cfg, notes, nil
}

// writeTemplate creates path with the template content. It never
// overwrites an existing file.
func writeTemplate(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	if _, err := f.WriteString(Template); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// applyEnv lets environment variables override file values. Empty
// variables are ignored so an exported-but-blank name cannot erase a
// setting.
func (c *Config) applyEnv() {
	overrides := []struct {
		env string
		dst *string
	}{
		{"SPOTIFY_ID", &c.Spotify.ClientID},
		{"SPOTIFY_SECRET", &c.Spotify.ClientSecret},
		{"TERMIX_SPOTIFY_DEVICE", &c.Spotify.Device},
		{"LASTFM_API_KEY", &c.LastFM.APIKey},
		{"TERMIX_MUSIC_DIR", &c.Local.MusicDir},
		{"TERMIX_YTDLP", &c.YouTube.YTDLP},
	}
	for _, o := range overrides {
		if v := strings.TrimSpace(os.Getenv(o.env)); v != "" {
			*o.dst = v
		}
	}
}

// applyDefaults fills in values that have a sensible default.
func (c *Config) applyDefaults() {
	c.Spotify.ClientID = strings.TrimSpace(c.Spotify.ClientID)
	c.Spotify.ClientSecret = strings.TrimSpace(c.Spotify.ClientSecret)
	c.Spotify.Device = strings.TrimSpace(c.Spotify.Device)
	c.LastFM.APIKey = strings.TrimSpace(c.LastFM.APIKey)
	c.Local.MusicDir = strings.TrimSpace(c.Local.MusicDir)
	c.YouTube.YTDLP = strings.TrimSpace(c.YouTube.YTDLP)

	if c.Local.MusicDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			c.Local.MusicDir = "Music"
		} else {
			c.Local.MusicDir = filepath.Join(home, "Music")
		}
	}
}
