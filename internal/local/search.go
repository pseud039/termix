package local

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pseud039/termix/internal/queue"
)

const maxResults = 100

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".ogg": true,
	".opus": true, ".wav": true, ".aac": true, ".wma": true,
}

// SearchProvider finds audio files under a music folder by name.
type SearchProvider struct {
	dir string
}

func NewSearchProvider(dir string) *SearchProvider {
	return &SearchProvider{dir: dir}
}

// Search walks the music folder and returns files whose path (relative to
// the folder) contains every word of q, ignoring case.
func (s *SearchProvider) Search(ctx context.Context, q string) ([]queue.Item, error) {
	if info, err := os.Stat(s.dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("music folder %q not found (set TERMIX_MUSIC_DIR)", s.dir)
	}
	words := strings.Fields(strings.ToLower(q))

	var items []queue.Item
	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !audioExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		rel, err := filepath.Rel(s.dir, path)
		if err != nil {
			rel = path
		}
		lower := strings.ToLower(rel)
		for _, w := range words {
			if !strings.Contains(lower, w) {
				return nil
			}
		}
		items = append(items, itemFor(path))
		if len(items) >= maxResults {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func itemFor(path string) queue.Item {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title, artist := name, filepath.Base(filepath.Dir(path))
	if a, t, ok := strings.Cut(name, " - "); ok {
		artist, title = strings.TrimSpace(a), strings.TrimSpace(t)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return queue.Item{
		ID:     "local-" + abs,
		Title:  title,
		Artist: artist,
		Source: queue.SourceLocal,
		URI:    abs,
	}
}
