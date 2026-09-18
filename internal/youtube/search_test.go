package youtube

import (
	"strings"
	"testing"
	"time"

	"github.com/pseud039/termix/internal/queue"
)

func TestParseResults(t *testing.T) {
	out := `{"id": "dQw4w9WgXcQ", "title": "Never Gonna Give You Up", "channel": "Rick Astley", "duration": 212.0}

{"id": "abc", "title": "Some upload", "uploader": "someone", "duration": null}
{"title": "no id, skipped"}
`
	items, err := parseResults(strings.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	first := items[0]
	if first.URI != "ytdl://dQw4w9WgXcQ" || first.Artist != "Rick Astley" ||
		first.Duration != 212*time.Second || first.Source != queue.SourceYouTube {
		t.Errorf("unexpected first item %+v", first)
	}
	if items[1].Artist != "someone" || items[1].Duration != 0 {
		t.Errorf("unexpected second item %+v", items[1])
	}
}

func TestParseResultsBadJSON(t *testing.T) {
	if _, err := parseResults(strings.NewReader("not json\n")); err == nil {
		t.Fatal("expected error")
	}
}
