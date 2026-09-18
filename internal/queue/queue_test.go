package queue

import (
	"fmt"
	"math/rand"
	"testing"
)

// newTestQueue returns a queue with n items ("t0".."t{n-1}") and a fixed
// random seed so shuffle results are reproducible.
func newTestQueue(n int) *Queue {
	q := New()
	q.rng = rand.New(rand.NewSource(1))
	for i := 0; i < n; i++ {
		q.Add(Item{ID: fmt.Sprintf("t%d", i)})
	}
	return q
}

func ids(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[string]int{}
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
	}
	for _, c := range count {
		if c != 0 {
			return false
		}
	}
	return true
}

func TestShuffleKeepsPlayedPrefixAndCurrent(t *testing.T) {
	q := newTestQueue(10)
	q.JumpTo(3)
	before := ids(q.Items())

	q.SetShuffle(true)
	after := ids(q.Items())

	if !equal(before[:4], after[:4]) {
		t.Fatalf("played prefix and current changed: before %v after %v", before[:4], after[:4])
	}
	if cur, _ := q.Current(); cur.ID != "t3" {
		t.Fatalf("current changed to %s", cur.ID)
	}
	if !sameSet(before[4:], after[4:]) {
		t.Fatalf("upcoming region lost items: before %v after %v", before[4:], after[4:])
	}
	if equal(before[4:], after[4:]) {
		t.Fatalf("upcoming region was not permuted: %v", after[4:])
	}
}

func TestShuffleOffRestoresInsertionOrder(t *testing.T) {
	q := newTestQueue(10)
	original := ids(q.Items())
	q.JumpTo(2)
	q.SetShuffle(true)
	q.Next() // move onto a shuffled item
	cur, _ := q.Current()

	q.SetShuffle(false)

	if got := ids(q.Items()); !equal(got, original) {
		t.Fatalf("order not restored: got %v want %v", got, original)
	}
	if now, _ := q.Current(); now.ID != cur.ID {
		t.Fatalf("current changed on unshuffle: was %s now %s", cur.ID, now.ID)
	}
	if idx := q.CurrentIndex(); q.Items()[idx].ID != cur.ID {
		t.Fatalf("CurrentIndex %d does not point at %s", idx, cur.ID)
	}
}

func TestAddUnderShuffleLandsUpcoming(t *testing.T) {
	q := newTestQueue(8)
	q.JumpTo(4)
	q.SetShuffle(true)

	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("new%d", i)
		idx := q.Add(Item{ID: id})
		if idx <= q.CurrentIndex() || idx >= q.Len() {
			t.Fatalf("add %s landed at %d, current %d len %d", id, idx, q.CurrentIndex(), q.Len())
		}
		if got := q.Items()[idx].ID; got != id {
			t.Fatalf("Items()[%d] = %s, want %s", idx, got, id)
		}
	}
	if cur, _ := q.Current(); cur.ID != "t4" {
		t.Fatalf("current moved to %s", cur.ID)
	}
}

func TestAddUnderShuffleWithNothingPlaying(t *testing.T) {
	q := New()
	q.rng = rand.New(rand.NewSource(1))
	q.SetShuffle(true)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("t%d", i)
		idx := q.Add(Item{ID: id})
		if idx < 0 || idx >= q.Len() || q.Items()[idx].ID != id {
			t.Fatalf("add %s returned bad index %d (len %d)", id, idx, q.Len())
		}
	}
}

func TestAddWithoutShuffleAppends(t *testing.T) {
	q := newTestQueue(3)
	if idx := q.Add(Item{ID: "x"}); idx != 3 {
		t.Fatalf("got index %d, want 3", idx)
	}
}

func TestNextAtEndRespectsRepeat(t *testing.T) {
	q := newTestQueue(3)
	q.JumpTo(2)
	if _, ok := q.Next(); ok {
		t.Fatal("repeat off: Next at the end should fail")
	}

	q.SetRepeat(RepeatAll)
	item, ok := q.Next()
	if !ok || item.ID != "t0" || q.CurrentIndex() != 0 {
		t.Fatalf("repeat all: expected wrap to t0, got %v ok=%v idx=%d", item.ID, ok, q.CurrentIndex())
	}
}

func TestRepeatAllWrapReshuffles(t *testing.T) {
	q := newTestQueue(10)
	q.SetShuffle(true)
	q.SetRepeat(RepeatAll)
	q.JumpTo(9)
	before := ids(q.Items())
	q.Next()
	after := ids(q.Items())
	if q.CurrentIndex() != 0 {
		t.Fatalf("expected cursor at 0, got %d", q.CurrentIndex())
	}
	if !sameSet(before, after) || equal(before, after) {
		t.Fatalf("expected a reshuffle on wrap: before %v after %v", before, after)
	}
}

func TestRepeatOneOnlyAffectsAdvance(t *testing.T) {
	q := newTestQueue(3)
	q.JumpTo(1)
	q.SetRepeat(RepeatOne)

	item, ok := q.Advance()
	if !ok || item.ID != "t1" || q.CurrentIndex() != 1 {
		t.Fatalf("Advance should replay t1, got %s idx=%d", item.ID, q.CurrentIndex())
	}
	item, ok = q.Next()
	if !ok || item.ID != "t2" {
		t.Fatalf("Next should skip ahead to t2, got %s ok=%v", item.ID, ok)
	}
	// Repeat one at the very end still loops, it never falls off.
	item, ok = q.Advance()
	if !ok || item.ID != "t2" {
		t.Fatalf("Advance at end with repeat one should replay t2, got %s ok=%v", item.ID, ok)
	}
}

func TestAdvanceWithRepeatOffEnds(t *testing.T) {
	q := newTestQueue(2)
	q.JumpTo(1)
	if _, ok := q.Advance(); ok {
		t.Fatal("Advance at the end with repeat off should fail")
	}
}

func TestCycleRepeat(t *testing.T) {
	q := New()
	want := []RepeatMode{RepeatAll, RepeatOne, RepeatOff}
	for _, w := range want {
		if got := q.CycleRepeat(); got != w {
			t.Fatalf("got %s want %s", got, w)
		}
	}
}

func TestRemoveKeepsCurrentItem(t *testing.T) {
	q := newTestQueue(5)
	q.JumpTo(3)
	q.Remove(1) // earlier item: t3 shifts to index 2
	if cur, _ := q.Current(); cur.ID != "t3" {
		t.Fatalf("current is %s after removing an earlier item", cur.ID)
	}
	q.Remove(3) // later item (t4)
	if cur, _ := q.Current(); cur.ID != "t3" {
		t.Fatalf("current is %s after removing a later item", cur.ID)
	}
}

func TestToggleShuffle(t *testing.T) {
	q := newTestQueue(2)
	if !q.ToggleShuffle() || !q.Shuffle() {
		t.Fatal("first toggle should turn shuffle on")
	}
	if q.ToggleShuffle() || q.Shuffle() {
		t.Fatal("second toggle should turn shuffle off")
	}
}

// upcomingInAddOrder reports whether the items after the cursor are in the
// order they were added (IDs "t0", "t1", … sort that way for n <= 10).
func upcomingInAddOrder(q *Queue) bool {
	up := ids(q.Items())[q.CurrentIndex()+1:]
	for i := 1; i < len(up); i++ {
		if up[i] < up[i-1] {
			return false
		}
	}
	return true
}

// Shuffle turned on after adding: the first track is playing, and even a
// two-track upcoming list must come out reordered.
func TestShuffleOnAlwaysReordersUpcoming(t *testing.T) {
	for seed := int64(0); seed < 500; seed++ {
		for n := 3; n <= 6; n++ {
			q := newTestQueue(n)
			q.rng = rand.New(rand.NewSource(seed))
			q.JumpTo(0)
			q.SetShuffle(true)
			if upcomingInAddOrder(q) {
				t.Fatalf("seed %d n %d: upcoming still in add order: %v", seed, n, ids(q.Items()))
			}
		}
	}
}

// Shuffle turned on before adding: the first track auto-plays, and once two
// or more tracks are waiting they must never be in add order.
func TestAddUnderShuffleNeverKeepsAddOrder(t *testing.T) {
	for seed := int64(0); seed < 500; seed++ {
		q := New()
		q.rng = rand.New(rand.NewSource(seed))
		q.SetShuffle(true)
		q.JumpTo(q.Add(Item{ID: "t0"})) // auto-play of the first add
		for i := 1; i < 8; i++ {
			q.Add(Item{ID: fmt.Sprintf("t%d", i)})
			if i >= 2 && upcomingInAddOrder(q) {
				t.Fatalf("seed %d after adding t%d: upcoming in add order: %v", seed, i, ids(q.Items()))
			}
		}
		if cur, _ := q.Current(); cur.ID != "t0" || q.CurrentIndex() != 0 {
			t.Fatalf("seed %d: playing track moved: %s at %d", seed, cur.ID, q.CurrentIndex())
		}
	}
}

// Wrapping with repeat all reshuffles the whole list, never back into add
// order.
func TestRepeatAllWrapNeverInAddOrder(t *testing.T) {
	for seed := int64(0); seed < 500; seed++ {
		q := newTestQueue(3)
		q.rng = rand.New(rand.NewSource(seed))
		q.SetShuffle(true)
		q.SetRepeat(RepeatAll)
		q.JumpTo(2)
		q.Next()
		if got := ids(q.Items()); equal(got, []string{"t0", "t1", "t2"}) {
			t.Fatalf("seed %d: wrap left the queue in add order", seed)
		}
	}
}

// recs returns n recommended items "r0".."r{n-1}".
func recs(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{ID: fmt.Sprintf("r%d", i)}
	}
	return out
}

func TestInsertRecommendedLandsUpcomingAndSpreads(t *testing.T) {
	q := newTestQueue(7) // t0..t6
	q.JumpTo(1)          // t0 played, t1 current, t2..t6 upcoming
	first := q.InsertRecommended(recs(2))

	items := q.Items()
	if items[0].ID != "t0" || items[1].ID != "t1" || q.CurrentIndex() != 1 {
		t.Fatalf("played/current disturbed: %v cur=%d", ids(items), q.CurrentIndex())
	}
	var recIdx []int
	for i, it := range items {
		if it.Recommended {
			recIdx = append(recIdx, i)
		}
	}
	if len(recIdx) != 2 || recIdx[0] != first {
		t.Fatalf("recommended at %v, first=%d", recIdx, first)
	}
	if recIdx[1]-recIdx[0] < 2 {
		t.Errorf("recommendations adjacent: %v in %v", recIdx, ids(items))
	}
	if recIdx[0] <= 1 {
		t.Errorf("recommendation landed at or before the cursor: %v", ids(items))
	}
	if own, rec := q.UpcomingCounts(); own != 5 || rec != 2 {
		t.Errorf("UpcomingCounts = %d, %d", own, rec)
	}
}

func TestInsertRecommendedIntoEmptyQueue(t *testing.T) {
	q := New()
	first := q.InsertRecommended(recs(3))
	if first != 0 || q.Len() != 3 {
		t.Fatalf("first=%d len=%d", first, q.Len())
	}
	if _, ok := q.JumpTo(first); !ok {
		t.Fatal("cannot jump to first recommendation")
	}
}

func TestSetShuffleModeOffFromSmartDropsUpcomingRecommended(t *testing.T) {
	q := newTestQueue(6)
	q.JumpTo(0)
	q.SetShuffleMode(ShuffleSmart)
	q.InsertRecommended(recs(2))
	if q.ShuffleMode() != ShuffleSmart || !q.Shuffle() {
		t.Fatal("mode not smart")
	}

	// Play through until a recommendation has been played.
	for {
		cur, _ := q.Current()
		if cur.Recommended {
			break
		}
		if _, ok := q.Next(); !ok {
			t.Fatal("never reached a recommendation")
		}
	}
	playedRec, _ := q.Current()

	q.SetShuffleMode(ShuffleOff)

	items := q.Items()
	cur, _ := q.Current()
	if cur.ID != playedRec.ID {
		t.Errorf("cursor moved: was %s now %s", playedRec.ID, cur.ID)
	}
	var recCount int
	var own []Item
	for _, it := range items {
		if it.Recommended {
			recCount++
		} else {
			own = append(own, it)
		}
	}
	if recCount != 1 {
		t.Errorf("want only the played recommendation kept, got %d in %v", recCount, ids(items))
	}
	if !equal(ids(own), []string{"t0", "t1", "t2", "t3", "t4", "t5"}) {
		t.Errorf("own items not in add order: %v", ids(own))
	}
}

func TestSmartToOnKeepsOrderDropsRecs(t *testing.T) {
	q := newTestQueue(6)
	q.JumpTo(0)
	q.SetShuffleMode(ShuffleOn)
	shuffled := ids(q.Items())
	q.SetShuffleMode(ShuffleSmart)
	if !equal(ids(q.Items()), shuffled) {
		t.Fatal("on -> smart changed the order")
	}
	q.InsertRecommended(recs(2))
	q.SetShuffleMode(ShuffleOn)
	if !equal(ids(q.Items()), shuffled) {
		t.Errorf("smart -> on: %v, want %v", ids(q.Items()), shuffled)
	}
	if own, rec := q.UpcomingCounts(); own != 5 || rec != 0 {
		t.Errorf("UpcomingCounts = %d, %d", own, rec)
	}
}

func TestToggleShuffleFromSmartGoesOff(t *testing.T) {
	q := newTestQueue(3)
	q.SetShuffleMode(ShuffleSmart)
	if on := q.ToggleShuffle(); on || q.ShuffleMode() != ShuffleOff {
		t.Errorf("toggle from smart: on=%v mode=%v", on, q.ShuffleMode())
	}
	if on := q.ToggleShuffle(); !on || q.ShuffleMode() != ShuffleOn {
		t.Errorf("toggle from off: on=%v mode=%v", on, q.ShuffleMode())
	}
}
