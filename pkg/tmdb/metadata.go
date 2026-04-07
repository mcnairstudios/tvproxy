package tmdb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type MovieMeta struct {
	TMDBID      int      `json:"tmdb_id"`
	PosterPath  string   `json:"poster_path,omitempty"`
	Overview    string   `json:"overview,omitempty"`
	Year        string   `json:"year,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Certification string `json:"certification,omitempty"`
}

type SeriesMeta struct {
	TMDBID      int                    `json:"tmdb_id"`
	PosterPath  string                 `json:"poster_path,omitempty"`
	Overview    string                 `json:"overview,omitempty"`
	Year        string                 `json:"year,omitempty"`
	Rating      float64                `json:"rating,omitempty"`
	Genres      []string               `json:"genres,omitempty"`
	Certification string               `json:"certification,omitempty"`
	Seasons     map[int]*SeasonMeta    `json:"seasons,omitempty"`
}

type SeasonMeta struct {
	Episodes map[int]*EpisodeMeta `json:"episodes,omitempty"`
}

type EpisodeMeta struct {
	Name      string `json:"name,omitempty"`
	Overview  string `json:"overview,omitempty"`
	StillPath string `json:"still_path,omitempty"`
	AirDate   string `json:"air_date,omitempty"`
}

type CollectionMeta struct {
	TMDBID       int    `json:"tmdb_id"`
	PosterPath   string `json:"poster_path,omitempty"`
	BackdropPath string `json:"backdrop_path,omitempty"`
}

type MetadataStore struct {
	Movies      map[string]*MovieMeta      `json:"movies"`
	Series      map[string]*SeriesMeta     `json:"series"`
	Collections map[string]*CollectionMeta `json:"collections,omitempty"`
	mu     sync.RWMutex
	path   string
}

func NewMetadataStore(baseDir string) *MetadataStore {
	ms := &MetadataStore{
		Movies: make(map[string]*MovieMeta),
		Series: make(map[string]*SeriesMeta),
		path:   filepath.Join(baseDir, "metadata.json"),
	}
	ms.load()
	return ms
}

func (ms *MetadataStore) GetMovie(name string) *MovieMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.Movies[name]
}

func (ms *MetadataStore) SetMovie(name string, m *MovieMeta) {
	ms.mu.Lock()
	ms.Movies[name] = m
	ms.mu.Unlock()
	ms.save()
}

func (ms *MetadataStore) GetSeries(name string) *SeriesMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.Series[name]
}

func (ms *MetadataStore) SetSeries(name string, s *SeriesMeta) {
	ms.mu.Lock()
	ms.Series[name] = s
	ms.mu.Unlock()
	ms.save()
}

func (ms *MetadataStore) GetEpisode(seriesName string, season, episode int) *EpisodeMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	s := ms.Series[seriesName]
	if s == nil || s.Seasons == nil {
		return nil
	}
	sm := s.Seasons[season]
	if sm == nil || sm.Episodes == nil {
		return nil
	}
	return sm.Episodes[episode]
}

func (ms *MetadataStore) SetSeasonEpisodes(seriesName string, seasonNum int, episodes map[int]*EpisodeMeta) {
	ms.mu.Lock()
	s := ms.Series[seriesName]
	if s == nil {
		ms.mu.Unlock()
		return
	}
	if s.Seasons == nil {
		s.Seasons = make(map[int]*SeasonMeta)
	}
	s.Seasons[seasonNum] = &SeasonMeta{Episodes: episodes}
	ms.mu.Unlock()
	ms.save()
}

func (ms *MetadataStore) GetCollection(name string) *CollectionMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if ms.Collections == nil {
		return nil
	}
	return ms.Collections[name]
}

func (ms *MetadataStore) SetCollection(name string, c *CollectionMeta) {
	ms.mu.Lock()
	if ms.Collections == nil {
		ms.Collections = make(map[string]*CollectionMeta)
	}
	ms.Collections[name] = c
	ms.mu.Unlock()
	ms.save()
}

func (ms *MetadataStore) load() {
	data, err := os.ReadFile(ms.path)
	if err != nil {
		return
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	json.Unmarshal(data, ms)
	if ms.Movies == nil {
		ms.Movies = make(map[string]*MovieMeta)
	}
	if ms.Series == nil {
		ms.Series = make(map[string]*SeriesMeta)
	}
	if ms.Collections == nil {
		ms.Collections = make(map[string]*CollectionMeta)
	}
}

func (ms *MetadataStore) save() {
	ms.mu.RLock()
	data, err := json.MarshalIndent(ms, "", "  ")
	ms.mu.RUnlock()
	if err != nil {
		return
	}
	tmp := ms.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, ms.path)
}
