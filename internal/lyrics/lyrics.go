// Package lyrics fetches song lyrics from lrclib.net and, when the site has
// them, the LRC timings that let the UI highlight the line being sung.
// lrclib needs no API key, so lyrics work for every source.
package lyrics

import (
	"errors"
	"sort"
	"time"
)

// ErrNotFound means lrclib has no lyrics for the track. It is "no data",
// not "request failed", so the UI shows a hint rather than an error.
var ErrNotFound = errors.New("lyrics: not found")

// Line is one synced lyric line: what to show and when it starts.
type Line struct {
	At   time.Duration
	Text string
}

// Lyrics is what the UI renders. Synced is nil when lrclib only has plain
// text; Plain may be empty when the track is instrumental.
type Lyrics struct {
	Plain        string
	Synced       []Line // sorted by At
	Instrumental bool
}

// IsSynced reports whether there are timed lines to follow.
func (l *Lyrics) IsSynced() bool {
	return l != nil && len(l.Synced) > 0
}

// ActiveLine returns the index of the last synced line that has started
// by pos, or -1 before the first line.
func (l *Lyrics) ActiveLine(pos time.Duration) int {
	if l == nil {
		return -1
	}
	// First line that starts after pos; the active one is just before it.
	i := sort.Search(len(l.Synced), func(i int) bool {
		return l.Synced[i].At > pos
	})
	return i - 1
}
