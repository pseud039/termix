package app

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/pseud039/termix/internal/player"
	"github.com/pseud039/termix/internal/queue"
)

const testTrackLen = 200 * time.Second

// playingSpotifyModel is a model part-way through a Spotify track that
// started a minute ago, so the start grace period is over.
func playingSpotifyModel() Model {
	m := New(context.Background(), queue.New(), nil, nil, nil, nil, nil, nil, nil)
	m.hasTrack = true
	m.currentTrack = queue.Item{ID: "A", Title: "A", Source: queue.SourceSpotify, URI: "spotify:track:A", Duration: testTrackLen}
	m.duration = testTrackLen
	m.trackStartedAt = time.Now().Add(-time.Minute)
	return m
}

// poll is a PlaybackStateMsg for the app's track "A" as Spotify reports it.
func poll(itemID string, pos time.Duration, playing bool) PlaybackStateMsg {
	return PlaybackStateMsg(player.State{
		Playing:  playing,
		Position: pos,
		Duration: testTrackLen,
		Volume:   50,
		TrackID:  "A",
		ItemID:   itemID,
	})
}

func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func TestSpotifyAdvancesNearEndOnce(t *testing.T) {
	m := playingSpotifyModel()

	m, cmd := send(t, m, poll("A", 100*time.Second, true))
	if cmd != nil {
		t.Fatal("advanced mid-track")
	}
	m, cmd = send(t, m, poll("A", testTrackLen-5*time.Second, true))
	if cmd != nil {
		t.Fatal("advanced with 5s left")
	}
	m, cmd = send(t, m, poll("A", testTrackLen-time.Second, true))
	if cmd == nil {
		t.Fatal("did not advance in the final seconds")
	}
	if m.spotifyAdvancedFrom != "A" {
		t.Fatalf("spotifyAdvancedFrom = %q, want A", m.spotifyAdvancedFrom)
	}
	// Polls keep arriving while the next track loads: no second advance.
	for i := 0; i < 3; i++ {
		if _, cmd = send(t, m, poll("A", testTrackLen-time.Second, true)); cmd != nil {
			t.Fatal("advanced twice for the same track")
		}
	}
}

func TestSpotifyStalePollAfterStartDoesNotSkipNewTrack(t *testing.T) {
	m := playingSpotifyModel()
	m, _ = send(t, m, poll("A", testTrackLen-time.Second, true)) // advance fires

	// The next track starts. Spotify still reports the old one at its end.
	m, _ = send(t, m, TrackStartMsg{Item: queue.Item{ID: "B", Title: "B", Source: queue.SourceSpotify, Duration: testTrackLen}})
	stale := poll("A", testTrackLen-time.Second, true)
	stale.TrackID = "B" // the app already thinks B is current
	m, cmd := send(t, m, stale)
	if cmd != nil {
		t.Fatal("stale state for the previous track skipped the new one")
	}
	if m.duration != testTrackLen {
		t.Fatalf("duration overwritten by stale state: %v", m.duration)
	}
}

func TestSpotifyRepeatOneStalePollIgnoredDuringGrace(t *testing.T) {
	m := playingSpotifyModel()
	// Repeat-one restarts the same ID; stale state looks identical to a
	// genuine near-the-end poll, so only the grace period can tell.
	m, _ = send(t, m, TrackStartMsg{Item: m.currentTrack})
	if _, cmd := send(t, m, poll("A", testTrackLen-time.Second, true)); cmd != nil {
		t.Fatal("advanced inside the start grace period")
	}
}

func TestSpotifyTrackVanishingBetweenPollsStillAdvances(t *testing.T) {
	// Seen near the end, then gone: no item, or a different one.
	for name, gone := range map[string]PlaybackStateMsg{
		"no item":        poll("", 0, false),
		"different item": poll("Z", 3*time.Second, true),
		"stopped at end": poll("A", testTrackLen, false),
		"reset to start": poll("A", 0, false),
	} {
		t.Run(name, func(t *testing.T) {
			m := playingSpotifyModel()
			m, cmd := send(t, m, poll("A", testTrackLen-5*time.Second, true))
			if cmd != nil || !m.spotifyNearEnd {
				t.Fatalf("expected to be armed without advancing (cmd=%v armed=%v)", cmd != nil, m.spotifyNearEnd)
			}
			if _, cmd = send(t, m, gone); cmd == nil {
				t.Fatal("track ended between polls but queue did not advance")
			}
		})
	}
}

func TestSpotifyPauseNearEndDoesNotAdvance(t *testing.T) {
	m := playingSpotifyModel()
	m, _ = send(t, m, poll("A", testTrackLen-5*time.Second, true))
	for i := 0; i < 3; i++ {
		if _, cmd := send(t, m, poll("A", testTrackLen-5*time.Second, false)); cmd != nil {
			t.Fatal("advanced while paused")
		}
	}
}

func TestSpotifySeekingBackDisarms(t *testing.T) {
	m := playingSpotifyModel()
	m, _ = send(t, m, poll("A", testTrackLen-5*time.Second, true))
	m, _ = send(t, m, poll("A", 30*time.Second, true))
	if m.spotifyNearEnd {
		t.Fatal("still armed after seeking away from the end")
	}
	if _, cmd := send(t, m, poll("", 0, false)); cmd != nil {
		t.Fatal("advanced after the user seeked away from the end")
	}
}

func TestNonSpotifyTrackNeverAutoAdvancesFromPolls(t *testing.T) {
	m := playingSpotifyModel()
	m.currentTrack.Source = queue.SourceYouTube // mpv reports its own end-of-file
	if _, cmd := send(t, m, poll("", testTrackLen-time.Second, true)); cmd != nil {
		t.Fatal("poll-based advance fired for a non-Spotify track")
	}
}

func TestPollForOtherTrackIDIsIgnored(t *testing.T) {
	m := playingSpotifyModel()
	late := poll("A", testTrackLen-time.Second, true)
	late.TrackID = "previous" // poll began before the track changed
	if _, cmd := send(t, m, late); cmd != nil {
		t.Fatal("advanced on a poll that began before the track changed")
	}
}
