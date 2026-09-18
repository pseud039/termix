package player

import (
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/Microsoft/go-winio"
)

// On Windows, mpv's IPC server is a named pipe. The JSON protocol is identical
// to the Unix socket one, so only the transport differs.
const mpvIPCAddr = `\\.\pipe\termix-mpv`

// mpvBinName asks for mpv.exe explicitly. A bare "mpv" would match mpv.com
// first (PATHEXT order): a console wrapper that spawns mpv.exe as a child, so
// killing it on Shutdown would leave mpv.exe (and the pipe) running.
const mpvBinName = "mpv.exe"

const mpvInstallHint = "install with: scoop install mpv yt-dlp  (or: choco install mpvio yt-dlp)"

// dialIPC connects to mpv's named pipe. The standard library can't dial named
// pipes; winio.DialPipe returns a regular net.Conn so nothing else changes.
func dialIPC() (net.Conn, error) {
	timeout := 200 * time.Millisecond
	return winio.DialPipe(mpvIPCAddr, &timeout)
}

// cleanupIPC is a no-op: Windows removes the pipe when mpv exits.
func cleanupIPC() {}

// mpvCandidates lists places mpv.exe commonly lives when it isn't on PATH:
// next to termix.exe, then Scoop (user and global), then Chocolatey.
func mpvCandidates() []string {
	var candidates []string

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "mpv.exe"))
	}

	scoop := os.Getenv("SCOOP")
	if scoop == "" {
		if home, err := os.UserHomeDir(); err == nil {
			scoop = filepath.Join(home, "scoop")
		}
	}
	scoopGlobal := os.Getenv("SCOOP_GLOBAL")
	if scoopGlobal == "" {
		scoopGlobal = `C:\ProgramData\scoop`
	}
	for _, root := range []string{scoop, scoopGlobal} {
		if root == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, "shims", "mpv.exe"),
			filepath.Join(root, "apps", "mpv", "current", "mpv.exe"),
		)
	}

	choco := os.Getenv("ChocolateyInstall")
	if choco == "" {
		choco = `C:\ProgramData\chocolatey`
	}
	candidates = append(candidates, filepath.Join(choco, "bin", "mpv.exe"))

	return candidates
}
