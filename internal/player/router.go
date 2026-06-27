package player

import (
	"context"
	"fmt"

	"github.com/pseud039/termix/internal/queue"
)

// Router wraps the two concrete backends and forwards calls to the
// one that matches the current item's Source field.
//
// For now: Spotify items go to the (stubbed) SpotifyPlayer.
// YouTube and Local items go to MpvPlayer.
// When switching sources, Router calls Stop() on the outgoing backend.
type Router struct {
	mpv     *MpvPlayer
	spotify Player // nil until Spotify is wired up in a later milestone

	active Player // whichever backend is currently playing
}

func NewRouter(mpv *MpvPlayer) *Router {
	return &Router{mpv: mpv}
}

// SetSpotifyPlayer is called once auth is complete and the Spotify
// backend is ready to use.
func (r *Router) SetSpotifyPlayer(p Player) {
	r.spotify = p
}

func (r *Router) backendFor(source queue.SourceType) (Player, error) {
	switch source {
	case queue.SourceYouTube, queue.SourceLocal:
		return r.mpv, nil
	case queue.SourceSpotify:
		if r.spotify == nil {
			return nil, fmt.Errorf("spotify not configured — run `termix auth` first")
		}
		return r.spotify, nil
	default:
		return nil, fmt.Errorf("unknown source type: %v", source)
	}
}

// — Player interface —

func (r *Router) Play(ctx context.Context, item queue.Item) error {
	backend, err := r.backendFor(item.Source)
	if err != nil {
		return err
	}
	// Stop the previously active backend only if we're switching.
	if r.active != nil && r.active != backend {
		_ = r.active.Stop(ctx) // best-effort
	}
	r.active = backend
	return backend.Play(ctx, item)
}

func (r *Router) Pause(ctx context.Context) error {
	if r.active == nil {
		return nil
	}
	return r.active.Pause(ctx)
}

func (r *Router) Resume(ctx context.Context) error {
	if r.active == nil {
		return nil
	}
	return r.active.Resume(ctx)
}

func (r *Router) Seek(ctx context.Context, seconds float64) error {
	if r.active == nil {
		return nil
	}
	return r.active.Seek(ctx, seconds)
}

func (r *Router) SetVolume(ctx context.Context, pct int) error {
	if r.active == nil {
		return nil
	}
	return r.active.SetVolume(ctx, pct)
}

func (r *Router) Position(ctx context.Context) (float64, bool, error) {
	if r.active == nil {
		return 0, false, nil
	}
	return r.active.Position(ctx)
}

func (r *Router) Stop(ctx context.Context) error {
	if r.active == nil {
		return nil
	}
	err := r.active.Stop(ctx)
	r.active = nil
	return err
}
