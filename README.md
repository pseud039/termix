# Termix

> A terminal music player that unifies **Spotify**, **YouTube**, and **local audio** into a single persistent queue.

<img src="https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white" /> <img src="https://img.shields.io/badge/Linux-FCC624?style=for-the-badge&logo=linux&logoColor=black" /> <img src="https://img.shields.io/badge/WSL2-4D4D4D?style=for-the-badge&logo=windows-terminal&logoColor=white" /> <img src="https://img.shields.io/badge/Spotify-1DB954?style=for-the-badge&logo=spotify&logoColor=white" /> <img src="https://img.shields.io/badge/YouTube-FF0000?style=for-the-badge&logo=youtube&logoColor=white" />
<img src="https://img.shields.io/badge/Status-In_Development-orange?style=for-the-badge" />

Termix is a keyboard-first TUI music player built with Go and Bubble Tea. Instead of switching between Spotify, YouTube, and local files, Termix lets you queue them all together and control everything from one interface.

---

## Features

- Unified queue across Spotify, YouTube, and local music
- Spotify playback via **spotifyd** + Web API
- YouTube playback through **mpv** + **yt-dlp**
- Local library indexing and search
- Persistent terminal UI built with Bubble Tea
- Keyboard-driven workflow
- OAuth authentication with automatic token refresh
- Automatic backend switching between music sources

---

## Preview

```text
┌─────────────────────────────────────────────────────────────────────┐
│ ▶ termix   Queue   Search   Library                  [space] [q]    │
├─────────────────────────────────────────────────────────────────────┤
│ ▶ Bohemian Rhapsody         Queen          5:55    [spotify]         │
│   Never Gonna Give You Up   Rick Astley   3:33    [youtube]         │
│   Dreams                    Fleetwood...  4:18    [local]           │
├─────────────────────────────────────────────────────────────────────┤
│ ⏸ Queen · Bohemian Rhapsody      ━━━━━━━━━━━━━━━━────── 2:14 / 5:55 │
└─────────────────────────────────────────────────────────────────────┘
```

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

- Go 1.22+
- mpv
- yt-dlp
- spotifyd
- Spotify Premium (required for Spotify playback)

---

## Installation

```bash
git clone https://github.com/pseud039/termix.git
cd termix

go mod download
```

Ubuntu / Debian:

```bash
sudo apt install mpv yt-dlp
```

Arch: `sudo pacman -S mpv yt-dlp` · macOS: `brew install mpv yt-dlp`

Windows (native) — with [Scoop](https://scoop.sh) or [Chocolatey](https://chocolatey.org):

```powershell
scoop install mpv yt-dlp
# or
choco install mpvio yt-dlp
```

Termix looks for `mpv.exe` on your `PATH`, then next to `termix.exe`, then in the default Scoop and Chocolatey folders. YouTube playback also needs `yt-dlp` on your `PATH` (or next to `mpv.exe`). If mpv can't be found, Termix still starts and shows an install hint in the status line.

Install `spotifyd` separately. Termix picks the spotifyd Spotify Connect device automatically (by its `spotifyd@...` name, or failing that by its "Speaker" type), so your phone or desktop app being online doesn't matter. If it still can't tell, set `TERMIX_SPOTIFY_DEVICE` to the exact device name.

Set Spotify credentials:

```bash
export SPOTIFY_ID=your_client_id
export SPOTIFY_SECRET=your_client_secret
```

Authenticate once:

```bash
termix auth
```

Run:

```bash
go run ./cmd
```

---

## Keybindings

| Key | Action |
|------|--------|
| `1-3` | Switch tabs |
| `Space` | Play / Pause |
| `n` / `p` | Next / Previous |
| `←` `→` | Seek |
| `+` `-` | Volume |
| `/` | Search |
| `Enter` | Play / Add |
| `Shift+Enter` | Insert next |
| `d` | Remove from queue |
| `q` | Quit |

---

## Project Structure

```text
termix/
├── cmd/
├── config/
└── internal/
    ├── app/
    ├── auth/
    ├── lyrics/
    ├── player/
    ├── queue/
    └── source/
```

---

## In Progress

- Spotify search and library browsing
- YouTube search integration
- Local library indexing
- Federated search across all sources
- Synced lyrics
- Queue persistence
- Album artwork (Kitty graphics protocol)
- Smart Shuffle support

---

## License

MIT
