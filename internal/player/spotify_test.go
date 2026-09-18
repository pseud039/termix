package player

import (
	"strings"
	"testing"

	zspotify "github.com/zmb3/spotify/v2"
)

func dev(id, name, typ string) zspotify.PlayerDevice {
	return zspotify.PlayerDevice{ID: zspotify.ID(id), Name: name, Type: typ}
}

func TestPickDevice(t *testing.T) {
	spotifyd := dev("sd", "spotifyd@pseudo", "Speaker")
	phone := dev("ph", "realme 8i", "Smartphone")
	desktop := dev("pc", "DESKTOP-ABC", "Computer")

	tests := []struct {
		name    string
		devices []zspotify.PlayerDevice
		want    string
		wantID  string
		wantErr string
	}{
		{
			name:    "no devices",
			wantErr: "no Spotify Connect devices",
		},
		{
			name:    "spotifyd picked over phone by name",
			devices: []zspotify.PlayerDevice{phone, spotifyd},
			wantID:  "sd",
		},
		{
			name:    "spotifyd picked over phone and desktop",
			devices: []zspotify.PlayerDevice{phone, desktop, spotifyd},
			wantID:  "sd",
		},
		{
			name:    "renamed spotifyd found by Speaker type",
			devices: []zspotify.PlayerDevice{phone, dev("sp", "living room", "Speaker")},
			wantID:  "sp",
		},
		{
			name:    "single device used as-is",
			devices: []zspotify.PlayerDevice{phone},
			wantID:  "ph",
		},
		{
			name: "restricted device skipped",
			devices: []zspotify.PlayerDevice{
				{ID: "r", Name: "spotifyd@old", Type: "Speaker", Restricted: true},
				phone,
			},
			wantID: "ph",
		},
		{
			name:    "env override matches case-insensitively",
			devices: []zspotify.PlayerDevice{spotifyd, phone},
			want:    "REALME 8I",
			wantID:  "ph",
		},
		{
			name:    "env override not found lists devices",
			devices: []zspotify.PlayerDevice{spotifyd, phone},
			want:    "nope",
			wantErr: `"spotifyd@pseudo" (Speaker)`,
		},
		{
			name:    "two spotifyd instances is ambiguous",
			devices: []zspotify.PlayerDevice{spotifyd, dev("sd2", "spotifyd@other", "Speaker"), phone},
			wantErr: "TERMIX_SPOTIFY_DEVICE",
		},
		{
			name:    "phone and desktop only is ambiguous",
			devices: []zspotify.PlayerDevice{phone, desktop},
			wantErr: "TERMIX_SPOTIFY_DEVICE",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickDevice(tc.devices, tc.want)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got device %q", tc.wantErr, got.Name)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got.ID) != tc.wantID {
				t.Fatalf("picked %q (%s), want id %s", got.Name, got.ID, tc.wantID)
			}
		})
	}
}
