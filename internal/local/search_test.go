package local

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestSearch(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"The Weeknd - Blinding Lights.mp3",
		"Bollywood/aae_ganpat_bjana.MP3",
		"Bollywood/notes.txt",
		"Rick Astley/Never Gonna Give You Up.flac",
	}
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewSearchProvider(dir)

	tests := []struct {
		query string
		want  []string
	}{
		{"blinding", []string{"Blinding Lights"}},
		{"WEEKND lights", []string{"Blinding Lights"}},
		{"weeknd gonna", nil},
		{"ganpat", []string{"aae_ganpat_bjana"}},
		{"bollywood", []string{"aae_ganpat_bjana"}},
		{"rick", []string{"Never Gonna Give You Up"}},
	}
	for _, tc := range tests {
		items, err := s.Search(context.Background(), tc.query)
		if err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		var got []string
		for _, it := range items {
			got = append(got, it.Title)
		}
		sort.Strings(got)
		if len(got) != len(tc.want) {
			t.Fatalf("%q: got %v, want %v", tc.query, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%q: got %v, want %v", tc.query, got, tc.want)
			}
		}
	}

	items, _ := s.Search(context.Background(), "blinding")
	if items[0].Artist != "The Weeknd" {
		t.Errorf("artist = %q, want The Weeknd", items[0].Artist)
	}
	items, _ = s.Search(context.Background(), "ganpat")
	if items[0].Artist != "Bollywood" {
		t.Errorf("artist = %q, want folder name Bollywood", items[0].Artist)
	}
}

func TestSearchMissingDir(t *testing.T) {
	s := NewSearchProvider(filepath.Join(t.TempDir(), "nope"))
	if _, err := s.Search(context.Background(), "x"); err == nil {
		t.Fatal("expected error for missing folder")
	}
}
