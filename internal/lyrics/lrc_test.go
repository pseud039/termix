package lyrics

import (
	"testing"
	"time"
)

func TestParseLRC(t *testing.T) {
	src := "[ar:Someone]\n[offset:0]\n[00:12.50]First line\r\n[00:01.125]Early\n[00:20.00]\n[01:02.00][02:03.10]Chorus\nno tag here\n"
	got := ParseLRC(src)

	want := []Line{
		{At: 1125 * time.Millisecond, Text: "Early"},
		{At: 12500 * time.Millisecond, Text: "First line"},
		{At: 20 * time.Second, Text: ""},
		{At: 62 * time.Second, Text: "Chorus"},
		{At: 123100 * time.Millisecond, Text: "Chorus"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseLRCEmpty(t *testing.T) {
	if got := ParseLRC(""); len(got) != 0 {
		t.Errorf("expected no lines, got %+v", got)
	}
}

func TestActiveLine(t *testing.T) {
	l := &Lyrics{Synced: []Line{
		{At: 5 * time.Second, Text: "a"},
		{At: 10 * time.Second, Text: "b"},
		{At: 15 * time.Second, Text: "c"},
	}}
	cases := []struct {
		pos  time.Duration
		want int
	}{
		{0, -1},
		{4999 * time.Millisecond, -1},
		{5 * time.Second, 0},
		{12 * time.Second, 1},
		{15 * time.Second, 2},
		{time.Hour, 2},
	}
	for _, c := range cases {
		if got := l.ActiveLine(c.pos); got != c.want {
			t.Errorf("ActiveLine(%s) = %d, want %d", c.pos, got, c.want)
		}
	}
	var nilLyrics *Lyrics
	if nilLyrics.ActiveLine(0) != -1 || nilLyrics.IsSynced() {
		t.Error("nil lyrics should have no active line")
	}
}
