package lyrics

import (
	"testing"
	"time"
)

func TestDiskCacheRoundTrip(t *testing.T) {
	c := NewDiskCache(t.TempDir())
	if _, ok := c.Get("missing"); ok {
		t.Fatal("unexpected hit on empty cache")
	}
	in := &Lyrics{
		Plain:  "hello\nworld",
		Synced: []Line{{At: time.Second, Text: "hello"}, {At: 2 * time.Second, Text: "world"}},
	}
	if err := c.Put("k", in); err != nil {
		t.Fatal(err)
	}
	out, ok := c.Get("k")
	if !ok {
		t.Fatal("expected a hit")
	}
	if out.Plain != in.Plain || len(out.Synced) != 2 || out.Synced[1] != in.Synced[1] {
		t.Errorf("got %+v, want %+v", out, in)
	}
}

func TestNilCacheIsNoop(t *testing.T) {
	var c *DiskCache
	if err := c.Put("k", &Lyrics{Plain: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("nil cache should never hit")
	}
}
