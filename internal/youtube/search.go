package youtube

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/pseud039/termix/internal/queue"
)

// SearchProvider searches YouTube by running yt-dlp and returns the
// results as queue.Items that mpv can play.
type SearchProvider struct {
	ytdlp string
}

// NewSearchProvider locates yt-dlp: ytdlpPath (youtube.ytdlp in
// config.toml) if set, otherwise PATH.
func NewSearchProvider(ytdlpPath string) (*SearchProvider, error) {
	if ytdlpPath != "" {
		if _, err := os.Stat(ytdlpPath); err != nil {
			return nil, fmt.Errorf("youtube.ytdlp: %w", err)
		}
		return &SearchProvider{ytdlp: ytdlpPath}, nil
	}
	p, err := exec.LookPath("yt-dlp")
	if err != nil {
		return nil, fmt.Errorf("yt-dlp not found on PATH (set youtube.ytdlp in config.toml)")
	}
	return &SearchProvider{ytdlp: p}, nil
}

// Search asks yt-dlp for the top 10 YouTube videos matching q.
func (s *SearchProvider) Search(ctx context.Context, q string) ([]queue.Item, error) {
	cmd := exec.CommandContext(ctx, s.ytdlp,
		"ytsearch10:"+q, "--flat-playlist", "--dump-json", "--no-warnings")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); msg != "" {
			return nil, fmt.Errorf("yt-dlp: %s", msg)
		}
		return nil, fmt.Errorf("yt-dlp: %w", err)
	}
	return parseResults(&stdout)
}

// parseResults reads yt-dlp's --dump-json output, one JSON object per line.
func parseResults(r io.Reader) ([]queue.Item, error) {
	var items []queue.Item
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var v struct {
			ID       string  `json:"id"`
			Title    string  `json:"title"`
			Channel  string  `json:"channel"`
			Uploader string  `json:"uploader"`
			Duration float64 `json:"duration"`
		}
		if err := json.Unmarshal(line, &v); err != nil {
			return nil, fmt.Errorf("parsing yt-dlp output: %w", err)
		}
		if v.ID == "" {
			continue
		}
		artist := v.Channel
		if artist == "" {
			artist = v.Uploader
		}
		items = append(items, queue.Item{
			ID:       "yt-" + v.ID,
			Title:    v.Title,
			Artist:   artist,
			Duration: time.Duration(v.Duration * float64(time.Second)),
			Source:   queue.SourceYouTube,
			URI:      "ytdl://" + v.ID,
		})
	}
	return items, sc.Err()
}
