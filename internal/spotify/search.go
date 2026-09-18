package spotify

import (
	"context"

	zspotify "github.com/zmb3/spotify/v2"

	"github.com/pseud039/termix/internal/queue"
)

// SearchProvider wraps the Spotify Web API's track search and returns
// results as queue.Items.
type SearchProvider struct {
	client *zspotify.Client
}

func NewSearchProvider(client *zspotify.Client) *SearchProvider {
	return &SearchProvider{client: client}
}

// Search queries Spotify for tracks matching q and returns up to 10
// results as queue.Items with Source == queue.SourceSpotify. Callers add
// the one the user picks straight into the queue with q.Add(item).
// 10 is the most Spotify allows for Development Mode apps (since Feb 2026);
// anything higher fails with "Invalid limit".
func (s *SearchProvider) Search(ctx context.Context, q string) ([]queue.Item, error) {
	result, err := s.client.Search(ctx, q, zspotify.SearchTypeTrack, zspotify.Limit(10))
	if err != nil {
		return nil, err
	}
	if result.Tracks == nil {
		return nil, nil
	}

	items := make([]queue.Item, 0, len(result.Tracks.Tracks))
	for i := range result.Tracks.Tracks {
		items = append(items, itemFromTrack(&result.Tracks.Tracks[i]))
	}
	return items, nil
}

// itemFromTrack boxes a Spotify track into the queue's common Item type.
// Search results, Liked Songs and playlist entries all go through here.
func itemFromTrack(t *zspotify.FullTrack) queue.Item {
	artist := ""
	if len(t.Artists) > 0 {
		artist = t.Artists[0].Name
	}
	cover := ""
	if len(t.Album.Images) > 0 {
		cover = t.Album.Images[0].URL
	}
	return queue.Item{
		ID:       string(t.ID),
		Title:    t.Name,
		Artist:   artist,
		Album:    t.Album.Name,
		Duration: t.TimeDuration(),
		Source:   queue.SourceSpotify,
		URI:      string(t.URI),
		CoverURL: cover,
	}
}
