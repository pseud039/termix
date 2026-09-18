package player

import (
	"context"
	"fmt"
	"os"

	zspotify "github.com/zmb3/spotify/v2"

	"github.com/pseud039/termix/internal/queue"
)

// SpotifyPlayer never touches audio directly. spotifyd owns the entire
// decode/output path as a Spotify Connect device; SpotifyPlayer's only
// job is to tell Spotify's servers what that device should do, via the
// Web API's /me/player/* endpoints.
type SpotifyPlayer struct {
	client    *zspotify.Client
	deviceID  zspotify.ID
	hasDevice bool
}

func NewSpotifyPlayer(client *zspotify.Client) *SpotifyPlayer {
	return &SpotifyPlayer{client: client}
}

// ensureDevice finds spotifyd's Connect device and caches its ID.
// If you have more than one Spotify Connect device visible (phone,
// desktop app, another spotifyd instance), set TERMIX_SPOTIFY_DEVICE to
// the exact device name to disambiguate.
func (s *SpotifyPlayer) ensureDevice(ctx context.Context) error {
	if s.hasDevice {
		return nil
	}
	devices, err := s.client.PlayerDevices(ctx)
	if err != nil {
		return fmt.Errorf("listing spotify devices: %w", err)
	}
	if len(devices) == 0 {
		return fmt.Errorf("no Spotify Connect devices found — is spotifyd running?")
	}

	if want := os.Getenv("TERMIX_SPOTIFY_DEVICE"); want != "" {
		for _, d := range devices {
			if d.Name == want {
				s.deviceID = d.ID
				s.hasDevice = true
				return nil
			}
		}
		return fmt.Errorf("no Spotify device named %q found", want)
	}

	if len(devices) > 1 {
		names := make([]string, len(devices))
		for i, d := range devices {
			names[i] = d.Name
		}
		return fmt.Errorf("multiple Spotify devices found %v — set TERMIX_SPOTIFY_DEVICE to pick one", names)
	}

	s.deviceID = devices[0].ID
	s.hasDevice = true
	return nil
}

func (s *SpotifyPlayer) opts() *zspotify.PlayOptions {
	return &zspotify.PlayOptions{DeviceID: &s.deviceID}
}

func (s *SpotifyPlayer) Play(ctx context.Context, item queue.Item) error {
	if err := s.ensureDevice(ctx); err != nil {
		return err
	}
	return s.client.PlayOpt(ctx, &zspotify.PlayOptions{
		DeviceID: &s.deviceID,
		URIs:     []zspotify.URI{zspotify.URI(item.URI)},
	})
}

func (s *SpotifyPlayer) Pause(ctx context.Context) error {
	if err := s.ensureDevice(ctx); err != nil {
		return err
	}
	return s.client.PauseOpt(ctx, s.opts())
}

func (s *SpotifyPlayer) Resume(ctx context.Context) error {
	if err := s.ensureDevice(ctx); err != nil {
		return err
	}
	// PlayOpt with DeviceID but no URIs resumes wherever we left off.
	return s.client.PlayOpt(ctx, s.opts())
}

func (s *SpotifyPlayer) Seek(ctx context.Context, seconds float64) error {
	if err := s.ensureDevice(ctx); err != nil {
		return err
	}
	return s.client.SeekOpt(ctx, int(seconds*1000), s.opts())
}

func (s *SpotifyPlayer) SetVolume(ctx context.Context, pct int) error {
	if err := s.ensureDevice(ctx); err != nil {
		return err
	}
	return s.client.VolumeOpt(ctx, pct, s.opts())
}

func (s *SpotifyPlayer) Position(ctx context.Context) (float64, bool, error) {
	state, err := s.client.PlayerState(ctx)
	if err != nil {
		return 0, false, err
	}
	if state == nil || state.Item == nil {
		return 0, false, nil
	}
	return float64(state.Progress) / 1000.0, state.Playing, nil
}

func (s *SpotifyPlayer) Duration(ctx context.Context) (float64, error) {
	state, err := s.client.PlayerState(ctx)
	if err != nil {
		return 0, err
	}
	if state == nil || state.Item == nil {
		return 0, nil
	}
	// FullTrack.Duration is in milliseconds.
	return float64(state.Item.Duration) / 1000.0, nil
}

func (s *SpotifyPlayer) Stop(ctx context.Context) error {
	// The Web API has no hard "stop" — Pause is the closest equivalent.
	// Router.Stop() is only called when switching away from Spotify to
	// another source anyway, so leaving position intact is fine.
	if !s.hasDevice {
		return nil // never played anything, nothing to stop
	}
	return s.client.PauseOpt(ctx, s.opts())
}
