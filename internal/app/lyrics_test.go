package app

import (
	"testing"
	"time"
)

func TestLyricsWindow(t *testing.T) {
	cases := []struct {
		n, height, center int
		start, end        int
	}{
		{5, 10, 3, 0, 5},    // everything fits
		{20, 5, 0, 0, 5},    // clamped at the top
		{20, 5, 10, 8, 13},  // centered
		{20, 5, 19, 15, 20}, // clamped at the bottom
	}
	for _, c := range cases {
		s, e := lyricsWindow(c.n, c.height, c.center)
		if s != c.start || e != c.end {
			t.Errorf("lyricsWindow(%d,%d,%d) = %d,%d want %d,%d", c.n, c.height, c.center, s, e, c.start, c.end)
		}
	}
}

func TestFormatOffset(t *testing.T) {
	cases := map[time.Duration]string{
		0:                        "0",
		500 * time.Millisecond:   "+0.5s",
		-1500 * time.Millisecond: "-1.5s",
	}
	for d, want := range cases {
		if got := formatOffset(d); got != want {
			t.Errorf("formatOffset(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestPlaybackPosInterpolates(t *testing.T) {
	m := Model{position: 10 * time.Second, playing: true, lastPollAt: time.Now().Add(-2 * time.Second), duration: 11 * time.Second}
	if got := m.playbackPos(); got != 11*time.Second {
		t.Errorf("expected clamp to duration, got %s", got)
	}
	m.duration = 0
	if got := m.playbackPos(); got < 12*time.Second || got > 13*time.Second {
		t.Errorf("expected ~12s, got %s", got)
	}
	m.playing = false
	if got := m.playbackPos(); got != 10*time.Second {
		t.Errorf("paused position should not advance, got %s", got)
	}
}
