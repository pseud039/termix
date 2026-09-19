# Termix

> A terminal music player that unifies **Spotify**, **YouTube**, and **local audio** into a single persistent queue.

<img src="https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white" /> <img src="https://img.shields.io/badge/Linux-FCC624?style=for-the-badge&logo=linux&logoColor=black" /> <img src="https://img.shields.io/badge/WSL2-4D4D4D?style=for-the-badge&logo=windows-terminal&logoColor=white" /> <img src="https://img.shields.io/badge/Spotify-1DB954?style=for-the-badge&logo=spotify&logoColor=white" /> <img src="https://img.shields.io/badge/YouTube-FF0000?style=for-the-badge&logo=youtube&logoColor=white" />
<img src="https://img.shields.io/badge/Status-Stable-green?style=for-the-badge" />

Termix is a keyboard-first TUI music player built with Go and Bubble Tea. Instead of switching between Spotify, YouTube, and local files, Termix lets you queue them all together and control everything from one interface.

---

## Features

- Unified queue across Spotify, YouTube, and local music
- Spotify playback via **spotifyd** + Web API
- YouTube playback through **mpv** + **yt-dlp**
- Search across Spotify, YouTube and your local music folder
- Shuffle, repeat, and smart shuffle that pulls similar tracks from Last.fm
- Synced lyrics from lrclib.net
- Terminal UI built with Bubble Tea, fully keyboard-driven
- OAuth authentication with automatic token refresh
- Automatic backend switching between music sources
- Single `config.toml` for credentials and settings

---

## Preview

<img width="1516" height="1038" alt="image" src="https://github.com/user-attachments/assets/403d0a7c-cdd0-433f-a255-9f3b788f0044" />


---

## Tech Stack

- Go
- Bubble Tea
- Lip Gloss
- Spotify Web API
- mpv
- yt-dlp
- spotifyd

---

## Requirements

Termix itself is a single Go binary. Playback is done by external programs, so which ones you need depends on which sources you want:

| Dependency | Needed for | Required? |
|------------|-----------|-----------|
| [mpv](https://mpv.io) | Local files and YouTube playback | Yes, unless you only use Spotify |
| [yt-dlp](https://github.com/yt-dlp/yt-dlp) | YouTube search and playback (mpv calls it) | For YouTube |
| [spotifyd](https://github.com/Spotifyd/spotifyd) (Windows: [Nemesis-AS fork](https://github.com/Nemesis-AS/spotifyd)) | Spotify playback (runs as a Spotify Connect device) | **Required** for Spotify |
| Spotify Premium account | Spotify playback and search | For Spotify |
| Spotify developer app | Client ID and secret for the Web API | For Spotify |
| [Last.fm API key](https://www.last.fm/api/account/create) | Smart shuffle (similar-track picks) | Optional |
| Go 1.22+ | Only to build from source | Build only |

Termix runs on Linux, WSL2, macOS and native Windows.

---

## Installation

### 1. Get Termix

With Go installed:

```bash
go install github.com/pseud039/termix/cmd@latest
```

That puts a binary called `cmd` in `$GOPATH/bin`. Rename it, or build from source with a proper name:

```bash
git clone https://github.com/pseud039/termix.git
cd termix
go build -o termix ./cmd        # termix.exe on Windows
```

Put the binary somewhere on your `PATH`, or run it from where it is.

### 2. Install mpv and yt-dlp

Ubuntu / Debian:

```bash
sudo apt install mpv yt-dlp
```

Arch:

```bash
sudo pacman -S mpv yt-dlp
```

macOS ([Homebrew](https://brew.sh)):

```bash
brew install mpv yt-dlp
```

Windows ([Scoop](https://scoop.sh) or [Chocolatey](https://chocolatey.org)):

```powershell
scoop install mpv yt-dlp
# or
choco install mpvio yt-dlp
```

Termix looks for `mpv` on your `PATH`, then next to its own binary, then in the default Scoop and Chocolatey folders (Windows) or `~/.nix-profile/bin` (Linux). `yt-dlp` must be on `PATH` or next to `mpv`; if it lives somewhere else, set `youtube.ytdlp` in `config.toml`. If mpv can't be found, Termix still starts and shows an install hint in the status line.

Distro packages of yt-dlp go stale and YouTube breaks often, so keep it updated:

```bash
yt-dlp -U
```

### 3. Install spotifyd (Spotify only)

Skip this step if you don't use Spotify.

> **Termix cannot play Spotify without spotifyd running.** Termix only sends commands through the Web API, and spotifyd does the actual playback.

**Linux / macOS:** install spotifyd from your package manager (`sudo pacman -S spotifyd`, `brew install spotifyd`) or from the [releases page](https://github.com/Spotifyd/spotifyd/releases), then log it in with your Spotify account following the [spotifyd docs](https://docs.spotifyd.rs) (recent versions use `spotifyd authenticate`; older ones take credentials in `spotifyd.conf`).

**Windows:** upstream spotifyd doesn't support Windows. Use the [Nemesis-AS fork](https://github.com/Nemesis-AS/spotifyd) instead. It has no prebuilt releases, so build it with [Rust](https://rustup.rs):

```powershell
git clone https://github.com/Nemesis-AS/spotifyd.git
cd spotifyd
cargo build --release
```

The binary ends up at `target\release\spotifyd.exe`. Put it on your `PATH`, then log in and configure it as described above.

On every platform, keep the default device name, or set one that contains `spotifyd`:

```toml
[global]
device_name = "spotifyd"
```

Start it before launching Termix (`spotifyd --no-daemon` in a second terminal, or as a user service). Termix picks the spotifyd Spotify Connect device automatically by its `spotifyd@...` name, or failing that by its "Speaker" type, so your phone or desktop app being online doesn't matter. If it still can't tell, set `spotify.device` in `config.toml` to the exact device name.

### 4. Create a Spotify app (Spotify only)

1. Go to the [Spotify developer dashboard](https://developer.spotify.com/dashboard) and create an app.
2. Under **Redirect URIs** add exactly `http://127.0.0.1:8080/callback`.
3. Copy the **Client ID** and **Client secret** for the next step.

### 5. Configure Termix

Run Termix once and it writes a commented `config.toml` to your user config directory and prints the path:

| OS | Path |
|----|------|
| Linux | `~/.config/termix/config.toml` |
| Windows | `%AppData%\termix\config.toml` |
| macOS | `~/Library/Application Support/termix/config.toml` |

Open it and fill in `client_id` and `client_secret` under `[spotify]`. The other keys are optional: a Last.fm key for smart shuffle, the spotifyd device name, your music folder (default: your home `Music` folder) and the yt-dlp path.

```toml
[spotify]
client_id = "..."
client_secret = "..."

[lastfm]
api_key = "..."          # optional, enables smart shuffle

[local]
music_dir = ""           # optional, default ~/Music
```

If you'd rather keep the config next to the binary, copy `config.example.toml` beside `termix` as `config.toml`; that file is used first. `TERMIX_CONFIG=/path/to/file` overrides both. The environment variables `SPOTIFY_ID`, `SPOTIFY_SECRET`, `LASTFM_API_KEY`, `TERMIX_SPOTIFY_DEVICE`, `TERMIX_MUSIC_DIR` and `TERMIX_YTDLP` still override individual values from the file.

### 6. Log in to Spotify (Spotify only)

```bash
termix auth
```

This prints a URL. Open it, approve the app, and the browser redirects back to Termix. The token is saved next to `config.toml` and refreshed automatically, so you only do this once.

The Library tab (`4`) lists your Liked Songs and playlists. It needs the library and playlist permissions, so if you logged in before that tab existed, run `termix auth` once more; the tab tells you when this is needed. Spotify-made playlists such as Discover Weekly cannot be read by third-party apps and show an error when opened.

### 7. Run

```bash
termix
```

Or, from a source checkout, `go run ./cmd`.

---

## Keybindings

| Key | Action |
|------|--------|
| `1-4` | Switch tabs (Queue, Search, Lyrics, Library) |
| `Space` | Play / Pause |
| `n` / `p` | Next / Previous |
| `←` `→` | Seek |
| `+` `-` | Volume |
| `z` | Shuffle off → on → smart (smart needs `lastfm.api_key` in `config.toml`) |
| `r` | Repeat off → all → one |
| `/` | Search |
| `Enter` | Play / Add |
| `Shift+Enter` | Insert next |
| `d` | Remove from queue |
| `j` / `k` | Move through search results, library lists or lyric lines |
| `Enter` (Library tab) | Open the selected playlist, or add the selected track to the queue |
| `a` (Library tab) | Add every track of the playlist to the queue |
| `Esc` (Library tab) | Back to the playlist list; `R` refetches from Spotify |
| `Enter` (Lyrics tab) | Seek to the selected lyric line; `Esc` follows playback again |
| `[` / `]` | Nudge lyric timing by 0.5s |
| `q` | Quit |

Lyrics come from [lrclib.net](https://lrclib.net) (no account needed) and are cached under your user cache directory (`termix/lyrics`).

---

## Project Structure

```text
termix/
├── cmd/                  entry point, `termix auth`
├── config.example.toml   copy to config.toml and fill in
└── internal/
    ├── app/              Bubble Tea UI
    ├── config/           config.toml loading
    ├── queue/            shared track queue
    ├── player/           mpv and spotifyd backends, router
    ├── spotify/          OAuth, search, Liked Songs and playlists
    ├── youtube/          yt-dlp search
    ├── local/            local file search
    ├── lastfm/           similar-track lookups
    ├── recommend/        smart shuffle picks
    └── lyrics/           lrclib.net lyrics
```

---

## In Progress

- Local library indexing
- Federated search across all sources
- Queue cursor (`Shift+Enter` insert-next and `d` remove are not wired up yet)
- Queue persistence
- Album artwork (Kitty graphics protocol)

---

## License

MIT
