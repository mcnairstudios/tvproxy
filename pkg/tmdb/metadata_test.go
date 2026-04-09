package tmdb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataStore_MovieCRUD(t *testing.T) {
	dir := t.TempDir()
	ms := NewMetadataStore(dir)

	if m := ms.GetMovie(603); m != nil {
		t.Fatal("expected nil for unknown movie")
	}

	ms.SetMovie(603, &MovieMeta{TMDBID: 603, PosterPath: "/poster.jpg", Overview: "Neo"})
	ms.Save()

	m := ms.GetMovie(603)
	if m == nil {
		t.Fatal("expected movie")
	}
	if m.Overview != "Neo" {
		t.Errorf("got overview %q, want %q", m.Overview, "Neo")
	}
	if m.PosterPath != "/poster.jpg" {
		t.Errorf("got poster %q, want %q", m.PosterPath, "/poster.jpg")
	}

	if _, err := os.Stat(filepath.Join(dir, "metadata.json")); err != nil {
		t.Fatal("metadata.json not created")
	}
}

func TestMetadataStore_ZeroIDIgnored(t *testing.T) {
	dir := t.TempDir()
	ms := NewMetadataStore(dir)

	ms.SetMovie(0, &MovieMeta{Overview: "should not be stored"})
	if m := ms.GetMovie(0); m != nil {
		t.Fatal("zero ID should not be stored")
	}

	ms.SetSeries(0, &SeriesMeta{Overview: "nope"})
	if s := ms.GetSeries(0); s != nil {
		t.Fatal("zero ID should not be stored")
	}

	ms.SetCollection(0, &CollectionMeta{PosterPath: "/nope.jpg"})
	if c := ms.GetCollection(0); c != nil {
		t.Fatal("zero ID should not be stored")
	}
}

func TestMetadataStore_SeriesEpisodes(t *testing.T) {
	dir := t.TempDir()
	ms := NewMetadataStore(dir)

	ms.SetSeries(1396, &SeriesMeta{TMDBID: 1396, Overview: "Breaking Bad"})
	ms.SetSeasonEpisodes(1396, 1, map[int]*EpisodeMeta{
		1: {Name: "Pilot"},
		2: {Name: "Cat's in the Bag..."},
	})

	ep := ms.GetEpisode(1396, 1, 1)
	if ep == nil || ep.Name != "Pilot" {
		t.Fatalf("got episode %+v, want Pilot", ep)
	}

	ep2 := ms.GetEpisode(1396, 1, 2)
	if ep2 == nil || ep2.Name != "Cat's in the Bag..." {
		t.Fatalf("got episode %+v", ep2)
	}

	if ms.GetEpisode(1396, 1, 99) != nil {
		t.Fatal("expected nil for unknown episode")
	}
	if ms.GetEpisode(1396, 99, 1) != nil {
		t.Fatal("expected nil for unknown season")
	}
}

func TestMetadataStore_Collections(t *testing.T) {
	dir := t.TempDir()
	ms := NewMetadataStore(dir)

	ms.SetCollection(2344, &CollectionMeta{TMDBID: 2344, PosterPath: "/matrix.jpg"})

	c := ms.GetCollection(2344)
	if c == nil || c.PosterPath != "/matrix.jpg" {
		t.Fatalf("got collection %+v", c)
	}
}

func TestMetadataStore_PersistAndReload(t *testing.T) {
	dir := t.TempDir()
	ms := NewMetadataStore(dir)

	ms.SetMovie(603, &MovieMeta{TMDBID: 603, Overview: "Matrix"})
	ms.SetSeries(1396, &SeriesMeta{TMDBID: 1396, Overview: "Breaking Bad"})
	ms.SetCollection(2344, &CollectionMeta{TMDBID: 2344, PosterPath: "/poster.jpg"})
	ms.Save()

	ms2 := NewMetadataStore(dir)
	if m := ms2.GetMovie(603); m == nil || m.Overview != "Matrix" {
		t.Fatalf("movie not reloaded: %+v", m)
	}
	if s := ms2.GetSeries(1396); s == nil || s.Overview != "Breaking Bad" {
		t.Fatalf("series not reloaded: %+v", s)
	}
	if c := ms2.GetCollection(2344); c == nil || c.PosterPath != "/poster.jpg" {
		t.Fatalf("collection not reloaded: %+v", c)
	}
}

func TestMetadataStore_LegacyMigration(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
		"movies": {
			"The Matrix (1999)": {"tmdb_id": 603, "overview": "Neo", "poster_path": "/m.jpg"},
			"No ID Movie": {"tmdb_id": 0, "overview": "skip me"}
		},
		"series": {
			"Breaking Bad": {"tmdb_id": 1396, "overview": "Walter White"}
		},
		"collections": {
			"Matrix Collection": {"tmdb_id": 2344, "poster_path": "/col.jpg"}
		}
	}`
	os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(legacy), 0644)

	ms := NewMetadataStore(dir)

	if m := ms.GetMovie(603); m == nil || m.Overview != "Neo" {
		t.Fatalf("legacy movie not migrated: %+v", m)
	}
	if ms.GetMovie(0) != nil {
		t.Fatal("zero-ID movie should not be migrated")
	}
	if s := ms.GetSeries(1396); s == nil || s.Overview != "Walter White" {
		t.Fatalf("legacy series not migrated: %+v", s)
	}
	if c := ms.GetCollection(2344); c == nil || c.PosterPath != "/col.jpg" {
		t.Fatalf("legacy collection not migrated: %+v", c)
	}
}
