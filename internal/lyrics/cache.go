package lyrics

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// DiskCache keeps fetched lyrics as one JSON file per track so replays and
// restarts don't hit lrclib again. It is best effort: callers ignore errors.
type DiskCache struct {
	dir string
}

func NewDiskCache(dir string) *DiskCache {
	return &DiskCache{dir: dir}
}

func (c *DiskCache) path(key string) string {
	sum := sha1.Sum([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}

// Get returns the cached lyrics for key, if any.
func (c *DiskCache) Get(key string) (*Lyrics, bool) {
	if c == nil {
		return nil, false
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var l Lyrics
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, false
	}
	return &l, true
}

// Put stores lyrics for key, writing through a temp file so a crash can't
// leave a half-written entry behind.
func (c *DiskCache) Put(key string, l *Lyrics) error {
	if c == nil {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	dst := c.path(key)
	tmp, err := os.CreateTemp(c.dir, "lyrics-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
