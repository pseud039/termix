package queue

import (
	"math/rand"
	"sort"
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

// RepeatMode says what happens when a track finishes.
type RepeatMode int

const (
	RepeatOff RepeatMode = iota // stop at the end of the queue
	RepeatAll                   // wrap around to the first item
	RepeatOne                   // play the current item again
)

func (r RepeatMode) String() string {
	switch r {
	case RepeatAll:
		return "all"
	case RepeatOne:
		return "one"
	default:
		return "off"
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

	// Recommended marks an item smart shuffle injected rather than one the
	// user queued. Unplayed recommendations are dropped when smart shuffle
	// is turned off.
	Recommended bool
}

// ShuffleMode says how the upcoming items are ordered.
type ShuffleMode int

const (
	ShuffleOff   ShuffleMode = iota // play in insertion order
	ShuffleOn                       // upcoming items randomised
	ShuffleSmart                    // randomised plus similar tracks injected
)

func (s ShuffleMode) String() string {
	switch s {
	case ShuffleOn:
		return "on"
	case ShuffleSmart:
		return "smart"
	default:
		return "off"
	}
}

// entry is an Item plus the order it was added in, so shuffle can be undone
// by sorting on seq.
type entry struct {
	item Item
	seq  uint64
}

// Queue is a thread-safe list of Items kept in play order, with a cursor
// pointing at the currently active item. With shuffle off, play order is
// insertion order; with shuffle on, the items after the cursor are
// randomised and turning shuffle off restores insertion order.
//
// Every index taken or returned by Queue methods is a play-order position.
type Queue struct {
	mu      sync.Mutex
	entries []entry
	current int // index of the playing entry; -1 = nothing playing
	nextSeq uint64
	shuffle ShuffleMode
	repeat  RepeatMode
	rng     *rand.Rand
}

func New() *Queue {
	return &Queue{
		current: -1,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (q *Queue) stamp(item Item) entry {
	e := entry{item: item, seq: q.nextSeq}
	q.nextSeq++
	return e
}

func (q *Queue) insertAt(i int, e entry) {
	q.entries = append(q.entries, entry{})
	copy(q.entries[i+1:], q.entries[i:])
	q.entries[i] = e
}

// Add appends the item to the play order and returns the index it landed
// at. With shuffle on it is placed at a random slot among the items that
// haven't played yet, so it doesn't always come last.
func (q *Queue) Add(item Item) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.stamp(item)
	if q.shuffle == ShuffleOff {
		q.entries = append(q.entries, e)
		return len(q.entries) - 1
	}
	// Upcoming region is [current+1, len]; when nothing is playing that
	// is the whole list.
	lo := q.current + 1
	n := len(q.entries) - lo
	i := lo + q.rng.Intn(n+1)
	// The new item was added last, so putting it last behind upcoming
	// items that are still in add order would leave the whole upcoming
	// list in add order, which looks like shuffle is off. Any other slot
	// puts it ahead of an older item.
	if n > 0 && i == len(q.entries) && inAddOrder(q.entries[lo:]) {
		i = lo + q.rng.Intn(n)
	}
	q.insertAt(i, e)
	return i
}

// AddNext places the item right after the current one.
func (q *Queue) AddNext(item Item) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.stamp(item)
	if q.current < 0 || q.current >= len(q.entries)-1 {
		q.entries = append(q.entries, e)
		return
	}
	q.insertAt(q.current+1, e)
}

// Current returns the item being played and whether one exists.
func (q *Queue) Current() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current < 0 || q.current >= len(q.entries) {
		return Item{}, false
	}
	return q.entries[q.current].item, true
}

// Next advances the cursor and returns the new current item. At the end
// of the queue it wraps around when repeat is "all" (reshuffling first if
// shuffle is on) and otherwise returns false. Repeat "one" is ignored
// here: a deliberate skip always moves on. See Advance.
func (q *Queue) Next() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.next()
}

func (q *Queue) next() (Item, bool) {
	if q.current+1 < len(q.entries) {
		q.current++
		return q.entries[q.current].item, true
	}
	if q.repeat != RepeatAll || len(q.entries) == 0 {
		return Item{}, false
	}
	if q.shuffle != ShuffleOff {
		q.shuffleFrom(0)
	}
	q.current = 0
	return q.entries[0].item, true
}

// Advance is Next for a track that finished on its own: with repeat "one"
// it returns the current item again without moving the cursor.
func (q *Queue) Advance() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.repeat == RepeatOne && q.current >= 0 && q.current < len(q.entries) {
		return q.entries[q.current].item, true
	}
	return q.next()
}

// Prev moves the cursor back.
func (q *Queue) Prev() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.current <= 0 {
		return Item{}, false
	}
	q.current--
	return q.entries[q.current].item, true
}

// JumpTo sets the cursor to an absolute index.
func (q *Queue) JumpTo(index int) (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if index < 0 || index >= len(q.entries) {
		return Item{}, false
	}
	q.current = index
	return q.entries[q.current].item, true
}

func (q *Queue) Remove(index int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if index < 0 || index >= len(q.entries) {
		return
	}
	q.entries = append(q.entries[:index], q.entries[index+1:]...)
	if q.current >= index && q.current > 0 {
		q.current--
	}
}

// SetDuration fills in the length of every item with the given ID whose
// length isn't known yet (local files only learn it once mpv plays them).
func (q *Queue) SetDuration(id string, d time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.entries {
		if q.entries[i].item.ID == id && q.entries[i].item.Duration == 0 {
			q.entries[i].item.Duration = d
		}
	}
}

// Items returns a snapshot of the play order so callers can read without
// holding the lock.
func (q *Queue) Items() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Item, len(q.entries))
	for i, e := range q.entries {
		out[i] = e.item
	}
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
	return len(q.entries)
}

// Clear empties the queue and resets the cursor. Shuffle and repeat
// settings are kept.
func (q *Queue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.entries = nil
	q.current = -1
}

// Shuffle reports whether shuffle is on (plain or smart).
func (q *Queue) Shuffle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.shuffle != ShuffleOff
}

// ShuffleMode returns the current shuffle mode.
func (q *Queue) ShuffleMode() ShuffleMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.shuffle
}

// ToggleShuffle flips between off and plain shuffle and reports whether
// shuffle is now on. Smart shuffle counts as on and toggles to off.
func (q *Queue) ToggleShuffle() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.shuffle == ShuffleOff {
		q.setShuffleMode(ShuffleOn)
	} else {
		q.setShuffleMode(ShuffleOff)
	}
	return q.shuffle != ShuffleOff
}

// SetShuffle turns plain shuffle on or off. See SetShuffleMode.
func (q *Queue) SetShuffle(on bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if on {
		q.setShuffleMode(ShuffleOn)
	} else {
		q.setShuffleMode(ShuffleOff)
	}
}

// SetShuffleMode switches shuffle mode. Going from off to on or smart
// randomises the items after the cursor (the current item and everything
// already played stay put). Switching between on and smart keeps the
// order. Leaving smart drops the recommended items that haven't played.
// Going to off restores insertion order with the cursor on the same item.
func (q *Queue) SetShuffleMode(mode ShuffleMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.setShuffleMode(mode)
}

func (q *Queue) setShuffleMode(mode ShuffleMode) {
	if mode == q.shuffle {
		return
	}
	was := q.shuffle
	q.shuffle = mode
	if was == ShuffleSmart {
		q.removeRecommendedUpcoming()
	}
	if mode != ShuffleOff {
		if was == ShuffleOff {
			q.shuffleFrom(q.current + 1)
		}
		return
	}
	var cur uint64
	hasCur := q.current >= 0 && q.current < len(q.entries)
	if hasCur {
		cur = q.entries[q.current].seq
	}
	sort.SliceStable(q.entries, func(i, j int) bool {
		return q.entries[i].seq < q.entries[j].seq
	})
	if hasCur {
		for i, e := range q.entries {
			if e.seq == cur {
				q.current = i
				break
			}
		}
	}
}

// shuffleFrom randomises entries[from:] in place (Fisher-Yates). With two
// or more items it shuffles again until they are not in add order: a plain
// shuffle of a short list often leaves it unchanged (half the time for two
// items), which looks like shuffle did nothing.
func (q *Queue) shuffleFrom(from int) {
	if from < 0 {
		from = 0
	}
	if from >= len(q.entries) {
		return
	}
	rest := q.entries[from:]
	for {
		q.rng.Shuffle(len(rest), func(i, j int) {
			rest[i], rest[j] = rest[j], rest[i]
		})
		if len(rest) < 2 || !inAddOrder(rest) {
			return
		}
	}
}

// InsertRecommended spreads smart-shuffle recommendations evenly through
// the items that haven't played yet, so they don't clump together or all
// land at the end. It returns the play-order index of the first one, so a
// caller with nothing playing can start from it.
func (q *Queue) InsertRecommended(items []Item) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	lo := q.current + 1
	if lo > len(q.entries) {
		lo = len(q.entries)
	}
	first := -1
	n := len(items)
	for k, item := range items {
		item.Recommended = true
		// Each insert shifts what follows, so recompute against the
		// current length: the k-th of n goes about (k+1)/(n+1) of the way in.
		span := len(q.entries) - lo
		i := lo + (k+1)*span/(n+1)
		if i > len(q.entries) {
			i = len(q.entries)
		}
		q.insertAt(i, q.stamp(item))
		if first < 0 || i < first {
			first = i
		}
	}
	return first
}

// removeRecommendedUpcoming drops recommended items after the cursor.
// Played ones stay as history and the cursor never moves.
func (q *Queue) removeRecommendedUpcoming() {
	lo := q.current + 1
	if lo >= len(q.entries) {
		return
	}
	kept := q.entries[:lo]
	for _, e := range q.entries[lo:] {
		if !e.item.Recommended {
			kept = append(kept, e)
		}
	}
	q.entries = kept
}

// UpcomingCounts reports how many items after the cursor the user queued
// themselves and how many are smart-shuffle recommendations.
func (q *Queue) UpcomingCounts() (own, recommended int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := q.current + 1; i < len(q.entries); i++ {
		if q.entries[i].item.Recommended {
			recommended++
		} else {
			own++
		}
	}
	return own, recommended
}

// inAddOrder reports whether entries are in the order they were added.
func inAddOrder(entries []entry) bool {
	for i := 1; i < len(entries); i++ {
		if entries[i].seq < entries[i-1].seq {
			return false
		}
	}
	return true
}

// Repeat returns the current repeat mode.
func (q *Queue) Repeat() RepeatMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.repeat
}

// SetRepeat sets the repeat mode.
func (q *Queue) SetRepeat(mode RepeatMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.repeat = mode
}

// CycleRepeat steps off → all → one → off and returns the new mode.
func (q *Queue) CycleRepeat() RepeatMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	switch q.repeat {
	case RepeatOff:
		q.repeat = RepeatAll
	case RepeatAll:
		q.repeat = RepeatOne
	default:
		q.repeat = RepeatOff
	}
	return q.repeat
}
