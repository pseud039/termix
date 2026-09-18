//go:build !windows

package player

import (
	"net"
	"os"
)

// On Unix, mpv's IPC server is a Unix domain socket file.
const mpvIPCAddr = "/tmp/termix-mpv.sock"

const mpvBinName = "mpv"

const mpvInstallHint = "install mpv and yt-dlp (apt install mpv yt-dlp / pacman -S mpv yt-dlp / brew install mpv yt-dlp)"

func dialIPC() (net.Conn, error) {
	return net.Dial("unix", mpvIPCAddr)
}

// cleanupIPC removes the socket file, which mpv leaves behind if it crashes.
func cleanupIPC() {
	os.Remove(mpvIPCAddr)
}

// mpvCandidates lists common install locations that package managers
// (brew, snap, nix) use and that might not be on the PATH we inherit.
func mpvCandidates() []string {
	candidates := []string{
		"/usr/bin/mpv",
		"/usr/local/bin/mpv",
		"/opt/homebrew/bin/mpv", // Apple Silicon brew
		"/snap/bin/mpv",
		"/nix/var/nix/profiles/default/bin/mpv",
	}
	if home := os.Getenv("HOME"); home != "" {
		candidates = append(candidates, home+"/.nix-profile/bin/mpv")
	}
	return candidates
}
