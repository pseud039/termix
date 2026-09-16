package queue

import (
	"sync"
	"time"
)

// SourceType tells the player which backend to use for a given item.
type SourceType int

const (
	SourceSpotify SourceType = iota
	SourceYouTube
	SourceLocal
)

func (s SourceType) String() string {
	switch s {
	case SourceSpotify:
		return "spotify"
	case SourceYouTube:
		return "youtube"
	case SourceLocal:
		return "local"
	default:
		return "spotify" // default to Spotify for unknown values
	}
}

// Item is the single unit the entire app reasons about.
// Whether it came from Spotify, YouTube, or a local file,
// everything gets boxed into this struct before touching the queue.
type Item struct {
	ID       string
	Title    string
	Artist   string
	Album    string
	Duration time.Duration
	Source   SourceType

	// URI is what the player uses:
	//   Spotify → "spotify:track:4uLU6hMCjMI75M1A2tKUQC"
	//   YouTube → "ytdl://dQw4w9WgXcQ"  (mpv understands ytdl:// natively)
	//   Local   → "/home/user/music/track.flac"
	URI string

	// CoverURL is a remote image URL we can fetch for album art.
	// Empty string = no art available.
	CoverURL string
}

// Queue is a thread-safe, ordered list of Items with a cursor
// pointing at the currently active item.
type Queue struct {
	mu      sync.Mutex
	items   []Item
	current int // index of the playing item; -1 = nothing playing
}

func New() *Queue {
	return &Queue{current: -1}
}

func (q *Queue) Add(item Item) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, item)
}

func (q *Queue) AddNext(item Item) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current < 0 || q.current >= len(q.items)-1 {
		q.items = append(q.items, item)
		return
	}
	// Insert right after current.
	insertAt := q.current + 1
	q.items = append(q.items[:insertAt], append([]Item{item}, q.items[insertAt:]...)...)
}

// Current returns the item being played and whether one exists.
func (q *Queue) Current() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current < 0 || q.current >= len(q.items) {
		return Item{}, false
	}
	return q.items[q.current], true
}

// Next advances the cursor and returns the new current item.
// Returns false if we were already at the end.
func (q *Queue) Next() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current+1 >= len(q.items) {
		return Item{}, false
	}
	q.current++
	return q.items[q.current], true
}

// Prev moves the cursor back.
func (q *Queue) Prev() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current <= 0 {
		return Item{}, false
	}
	q.current--
	return q.items[q.current], true
}

// JumpTo sets the cursor to an absolute index.
func (q *Queue) JumpTo(index int) (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if index < 0 || index >= len(q.items) {
		return Item{}, false
	}
	q.current = index
	return q.items[q.current], true
}

func (q *Queue) Remove(index int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if index < 0 || index >= len(q.items) {
		return
	}
	q.items = append(q.items[:index], q.items[index+1:]...)
	if q.current >= index && q.current > 0 {
		q.current--
	}
}

// Items returns a snapshot copy so callers can read without holding the lock.
func (q *Queue) Items() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Item, len(q.items))
	copy(out, q.items)
	return out
}

func (q *Queue) CurrentIndex() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.current
}

func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Clear empties the queue and resets the cursor.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = nil
	q.current = -1
}
