package tmdb

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestResolveIDFromSearchCache(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(dir, func() string { return "" }, zerolog.Nop())

	c.cache.Set("search_The Matrix (1999)_movie", map[string]any{
		"results": []any{
			map[string]any{"id": float64(603), "title": "The Matrix", "poster_path": "/poster.jpg"},
		},
	})

	id := c.resolveID("The Matrix (1999)", "movie")
	if id != 603 {
		t.Errorf("resolveID got %d, want 603", id)
	}

	id2 := c.resolveID("Unknown Movie", "movie")
	if id2 != 0 {
		t.Errorf("resolveID for unknown got %d, want 0", id2)
	}
}

func TestLookupMovieByName(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(dir, func() string { return "" }, zerolog.Nop())

	c.cache.Set("search_The Matrix (1999)_movie", map[string]any{
		"results": []any{
			map[string]any{"id": float64(603), "title": "The Matrix"},
		},
	})
	c.meta.SetMovie(603, &MovieMeta{TMDBID: 603, Overview: "Neo", PosterPath: "/m.jpg"})

	m := c.LookupMovie("The Matrix (1999)")
	if m == nil {
		t.Fatal("expected movie")
	}
	if m.Overview != "Neo" {
		t.Errorf("overview got %q, want Neo", m.Overview)
	}

	m2 := c.LookupMovie("Unknown")
	if m2 != nil {
		t.Fatal("expected nil for unknown movie")
	}
}

func TestLookupSeriesByName(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(dir, func() string { return "" }, zerolog.Nop())

	c.cache.Set("search_Breaking Bad_tv", map[string]any{
		"results": []any{
			map[string]any{"id": float64(1396), "name": "Breaking Bad"},
		},
	})
	c.meta.SetSeries(1396, &SeriesMeta{
		TMDBID:        1396,
		Overview:      "Walter White",
		Certification: "TV-MA",
		Seasons:       map[int]*SeasonMeta{1: {Episodes: map[int]*EpisodeMeta{1: {Name: "Pilot"}}}},
	})

	s := c.LookupSeries("Breaking Bad")
	if s == nil || s.Overview != "Walter White" {
		t.Fatalf("unexpected series: %+v", s)
	}
	if s.Certification != "TV-MA" {
		t.Errorf("certification got %q, want TV-MA", s.Certification)
	}

	ep := c.LookupEpisode("Breaking Bad", 1, 1)
	if ep == nil || ep.Name != "Pilot" {
		t.Fatalf("unexpected episode: %+v", ep)
	}
}

func TestLookupPosterByName(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(dir, func() string { return "" }, zerolog.Nop())

	c.cache.Set("search_Alien (1979)_movie", map[string]any{
		"results": []any{
			map[string]any{"id": float64(348), "title": "Alien"},
		},
	})
	c.meta.SetMovie(348, &MovieMeta{TMDBID: 348, PosterPath: "/alien.jpg"})

	poster := c.LookupPoster("Alien (1979)", "movie")
	if poster == "" {
		t.Fatal("expected poster URL")
	}

	poster2 := c.LookupPoster("Unknown", "movie")
	if poster2 != "" {
		t.Errorf("expected empty poster for unknown, got %q", poster2)
	}
}
