package player

import (
	"context"
	"fmt"
	"strings"

	zspotify "github.com/zmb3/spotify/v2"

	"github.com/pseud039/termix/internal/queue"
)

// SpotifyPlayer never touches audio directly. spotifyd owns the entire
// decode/output path as a Spotify Connect device; SpotifyPlayer's only
// job is to tell Spotify's servers what that device should do, via the
// Web API's /me/player/* endpoints.
type SpotifyPlayer struct {
	client     *zspotify.Client
	deviceName string // exact Connect device name from config; "" auto-detects
	deviceID   zspotify.ID
	hasDevice  bool
}

// NewSpotifyPlayer controls the Connect device called deviceName
// (spotify.device in config.toml), or auto-detects spotifyd when it is empty.
func NewSpotifyPlayer(client *zspotify.Client, deviceName string) *SpotifyPlayer {
	return &SpotifyPlayer{client: client, deviceName: deviceName}
}

// ensureDevice finds spotifyd's Connect device and caches its ID.
func (s *SpotifyPlayer) ensureDevice(ctx context.Context) error {
	if s.hasDevice {
		return nil
	}
	devices, err := s.client.PlayerDevices(ctx)
	if err != nil {
		return fmt.Errorf("listing spotify devices: %w", err)
	}
	dev, err := pickDevice(devices, s.deviceName)
	if err != nil {
		return err
	}
	s.deviceID = dev.ID
	s.hasDevice = true
	return nil
}

func pickDevice(devices []zspotify.PlayerDevice, want string) (zspotify.PlayerDevice, error) {
	if len(devices) == 0 {
		return zspotify.PlayerDevice{}, fmt.Errorf("no Spotify Connect devices found — is spotifyd running?")
	}

	if want != "" {
		for _, d := range devices {
			if strings.EqualFold(d.Name, want) {
				return d, nil
			}
		}
		return zspotify.PlayerDevice{}, fmt.Errorf("no Spotify device named %q found (saw %s)", want, describeDevices(devices))
	}

	var usable []zspotify.PlayerDevice
	for _, d := range devices {
		if !d.Restricted {
			usable = append(usable, d)
		}
	}
	if len(usable) == 0 {
		return zspotify.PlayerDevice{}, fmt.Errorf("all Spotify devices are restricted (saw %s)", describeDevices(devices))
	}

	if d, ok := single(usable, func(d zspotify.PlayerDevice) bool {
		return strings.Contains(strings.ToLower(d.Name), "spotifyd")
	}); ok {
		return d, nil
	}
	if d, ok := single(usable, func(d zspotify.PlayerDevice) bool {
		return strings.EqualFold(d.Type, "Speaker")
	}); ok {
		return d, nil
	}
	if len(usable) == 1 {
		return usable[0], nil
	}

	return zspotify.PlayerDevice{}, fmt.Errorf("can't tell which Spotify device is spotifyd (saw %s) — set spotify.device in config.toml to its name", describeDevices(usable))
}

func single(devices []zspotify.PlayerDevice, keep func(zspotify.PlayerDevice) bool) (zspotify.PlayerDevice, bool) {
	var found zspotify.PlayerDevice
	n := 0
	for _, d := range devices {
		if keep(d) {
			found = d
			n++
		}
	}
	return found, n == 1
}

func describeDevices(devices []zspotify.PlayerDevice) string {
	parts := make([]string, len(devices))
	for i, d := range devices {
		parts[i] = fmt.Sprintf("%q (%s)", d.Name, d.Type)
	}
	return strings.Join(parts, ", ")
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

func (s *SpotifyPlayer) Volume(ctx context.Context) (int, error) {
	state, err := s.client.PlayerState(ctx)
	if err != nil {
		return 0, err
	}
	if state == nil {
		return 0, fmt.Errorf("no active Spotify device")
	}
	return int(state.Device.Volume), nil
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
