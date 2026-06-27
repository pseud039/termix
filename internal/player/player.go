package player

import (
	"context"
	"time"

	"github.com/pseud039/termix/internal/queue"
)

// Player is the interface every playback backend must satisfy.
// SpotifyPlayer and MpvPlayer both implement this.
type Player interface {
	// Play starts playing the given item from the beginning.
	Play(ctx context.Context, item queue.Item) error

	// Pause pauses playback without losing position.
	Pause(ctx context.Context) error

	// Resume continues from wherever we paused.
	Resume(ctx context.Context) error

	// Seek jumps to an absolute position in seconds.
	Seek(ctx context.Context, seconds float64) error

	// SetVolume takes 0–100.
	SetVolume(ctx context.Context, pct int) error

	// Position returns elapsed seconds. The second return is whether
	// playback is active at all (false when stopped/no track).
	Position(ctx context.Context) (float64, bool, error)

	// Stop halts playback entirely and releases the track.
	Stop(ctx context.Context) error
}

// State is a snapshot of what the active player is doing, polled
// by the app ticker and forwarded into the Bubbletea update loop.
type State struct {
	Playing  bool
	Position time.Duration
	Volume   int
	TrackID  string // so the app can detect track changes
}
