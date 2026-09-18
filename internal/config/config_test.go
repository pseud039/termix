package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// clearEnv makes sure the developer's real environment can't leak into a
// test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{EnvPath, "SPOTIFY_ID", "SPOTIFY_SECRET", "TERMIX_SPOTIFY_DEVICE", "LASTFM_API_KEY", "TERMIX_MUSIC_DIR", "TERMIX_YTDLP"} {
		t.Setenv(k, "")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFromFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, `
[spotify]
client_id = "id"
client_secret = "secret"
device = "spotifyd@box"

[lastfm]
api_key = "lfm"

[local]
music_dir = '`+dir+`'

[youtube]
ytdlp = "/opt/yt-dlp"
`)

	cfg, notes, err := loadFrom([]string{path}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("unexpected notes: %v", notes)
	}
	if cfg.Path != path {
		t.Errorf("Path = %q, want %q", cfg.Path, path)
	}
	if cfg.Spotify.ClientID != "id" || cfg.Spotify.ClientSecret != "secret" || cfg.Spotify.Device != "spotifyd@box" {
		t.Errorf("spotify = %+v", cfg.Spotify)
	}
	if cfg.LastFM.APIKey != "lfm" {
		t.Errorf("lastfm = %+v", cfg.LastFM)
	}
	if cfg.Local.MusicDir != dir {
		t.Errorf("music_dir = %q, want %q", cfg.Local.MusicDir, dir)
	}
	if cfg.YouTube.YTDLP != "/opt/yt-dlp" {
		t.Errorf("ytdlp = %q", cfg.YouTube.YTDLP)
	}
}

func TestLoadOrder(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first.toml")
	second := filepath.Join(dir, "second.toml")
	writeFile(t, first, "[lastfm]\napi_key = \"first\"\n")
	writeFile(t, second, "[lastfm]\napi_key = \"second\"\n")

	cfg, _, err := loadFrom([]string{filepath.Join(dir, "missing.toml"), first, second}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != first || cfg.LastFM.APIKey != "first" {
		t.Fatalf("got %q from %q, want first", cfg.LastFM.APIKey, cfg.Path)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, "[spotify]\nclient_id = \"file\"\nclient_secret = \"filesecret\"\n")

	t.Setenv("SPOTIFY_ID", " env ")
	cfg, _, err := loadFrom([]string{path}, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Spotify.ClientID != "env" {
		t.Errorf("client_id = %q, want env override", cfg.Spotify.ClientID)
	}
	// An exported-but-empty variable must not blank the file value.
	if cfg.Spotify.ClientSecret != "filesecret" {
		t.Errorf("client_secret = %q, want file value", cfg.Spotify.ClientSecret)
	}
}

func TestTemplateWrittenWhenMissing(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	tmpl := filepath.Join(dir, "sub", "config.toml")

	cfg, notes, err := loadFrom([]string{filepath.Join(dir, "nope.toml")}, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != "" {
		t.Errorf("Path = %q, want empty", cfg.Path)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], tmpl) {
		t.Fatalf("notes = %v, want one mentioning %s", notes, tmpl)
	}
	data, err := os.ReadFile(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != Template {
		t.Error("template file content differs from Template")
	}

	// Second load finds the template and does not rewrite it.
	cfg, notes, err = loadFrom([]string{tmpl}, tmpl)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != tmpl || len(notes) != 0 {
		t.Errorf("second load: Path = %q, notes = %v", cfg.Path, notes)
	}
}

func TestTemplateParses(t *testing.T) {
	var cfg Config
	meta, err := toml.Decode(Template, &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if u := meta.Undecoded(); len(u) != 0 {
		t.Errorf("template has keys the Config struct doesn't: %v", u)
	}
}

func TestUnknownKeyNoted(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "[spotify]\nclientid = \"x\"\n")

	_, notes, err := loadFrom([]string{path}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "spotify.clientid") {
		t.Errorf("notes = %v, want unknown-key note", notes)
	}
}

func TestBadTomlIsError(t *testing.T) {
	clearEnv(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	writeFile(t, path, "[spotify\n")

	if _, _, err := loadFrom([]string{path}, ""); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMusicDirDefault(t *testing.T) {
	clearEnv(t)
	cfg, _, err := loadFrom(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cfg.Local.MusicDir) != "Music" {
		t.Errorf("music_dir = %q, want a Music folder", cfg.Local.MusicDir)
	}
}
