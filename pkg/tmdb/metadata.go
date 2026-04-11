package tmdb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type MovieMeta struct {
	TMDBID        int      `json:"tmdb_id"`
	PosterPath    string   `json:"poster_path,omitempty"`
	BackdropPath  string   `json:"backdrop_path,omitempty"`
	Overview      string   `json:"overview,omitempty"`
	Year          string   `json:"year,omitempty"`
	Rating        float64  `json:"rating,omitempty"`
	Genres        []string `json:"genres,omitempty"`
	Certification string   `json:"certification,omitempty"`
	CollectionID  int      `json:"collection_id,omitempty"`
}

type SeriesMeta struct {
	TMDBID        int                    `json:"tmdb_id"`
	PosterPath    string                 `json:"poster_path,omitempty"`
	BackdropPath  string                 `json:"backdrop_path,omitempty"`
	Overview      string                 `json:"overview,omitempty"`
	Year          string                 `json:"year,omitempty"`
	Rating        float64                `json:"rating,omitempty"`
	Genres        []string               `json:"genres,omitempty"`
	Certification string                 `json:"certification,omitempty"`
	Seasons       map[int]*SeasonMeta    `json:"seasons,omitempty"`
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
	Movies      map[int]*MovieMeta      `json:"movies"`
	Series      map[int]*SeriesMeta     `json:"series"`
	Collections map[int]*CollectionMeta `json:"collections,omitempty"`
	mu          sync.RWMutex
	path        string
	dirty       bool
}

func NewMetadataStore(baseDir string) *MetadataStore {
	ms := &MetadataStore{
		Movies:      make(map[int]*MovieMeta),
		Series:      make(map[int]*SeriesMeta),
		Collections: make(map[int]*CollectionMeta),
		path:        filepath.Join(baseDir, "metadata.json"),
	}
	ms.load()
	return ms
}

func (ms *MetadataStore) GetMovie(tmdbID int) *MovieMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.Movies[tmdbID]
}

func (ms *MetadataStore) SetMovie(tmdbID int, m *MovieMeta) {
	if tmdbID == 0 {
		return
	}
	ms.mu.Lock()
	ms.Movies[tmdbID] = m
	ms.dirty = true
	ms.mu.Unlock()
}

func (ms *MetadataStore) GetSeries(tmdbID int) *SeriesMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.Series[tmdbID]
}

func (ms *MetadataStore) SetSeries(tmdbID int, s *SeriesMeta) {
	if tmdbID == 0 {
		return
	}
	ms.mu.Lock()
	ms.Series[tmdbID] = s
	ms.dirty = true
	ms.mu.Unlock()
}

func (ms *MetadataStore) GetEpisode(tmdbID int, season, episode int) *EpisodeMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	s := ms.Series[tmdbID]
	if s == nil || s.Seasons == nil {
		return nil
	}
	sm := s.Seasons[season]
	if sm == nil || sm.Episodes == nil {
		return nil
	}
	return sm.Episodes[episode]
}

func (ms *MetadataStore) SetSeasonEpisodes(tmdbID int, seasonNum int, episodes map[int]*EpisodeMeta) {
	if tmdbID == 0 {
		return
	}
	ms.mu.Lock()
	s := ms.Series[tmdbID]
	if s == nil {
		ms.mu.Unlock()
		return
	}
	if s.Seasons == nil {
		s.Seasons = make(map[int]*SeasonMeta)
	}
	s.Seasons[seasonNum] = &SeasonMeta{Episodes: episodes}
	ms.dirty = true
	ms.mu.Unlock()
}

func (ms *MetadataStore) SeriesNeedingEpisodes() []int {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	var ids []int
	for id, s := range ms.Series {
		if s.TMDBID > 0 && len(s.Seasons) == 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func (ms *MetadataStore) GetCollection(tmdbID int) *CollectionMeta {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.Collections[tmdbID]
}

func (ms *MetadataStore) SetCollection(tmdbID int, c *CollectionMeta) {
	if tmdbID == 0 {
		return
	}
	ms.mu.Lock()
	ms.Collections[tmdbID] = c
	ms.dirty = true
	ms.mu.Unlock()
}

type legacyMetadata struct {
	Movies      map[string]*MovieMeta      `json:"movies"`
	Series      map[string]*SeriesMeta     `json:"series"`
	Collections map[string]*CollectionMeta `json:"collections,omitempty"`
}

func (ms *MetadataStore) load() {
	data, err := os.ReadFile(ms.path)
	if err != nil {
		return
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if err := json.Unmarshal(data, ms); err != nil || len(ms.Movies) == 0 {
		ms.Movies = make(map[int]*MovieMeta)
		ms.Series = make(map[int]*SeriesMeta)
		ms.Collections = make(map[int]*CollectionMeta)

		var legacy legacyMetadata
		if err := json.Unmarshal(data, &legacy); err == nil {
			ms.migrateLegacy(&legacy)
		}
	}

	if ms.Movies == nil {
		ms.Movies = make(map[int]*MovieMeta)
	}
	if ms.Series == nil {
		ms.Series = make(map[int]*SeriesMeta)
	}
	if ms.Collections == nil {
		ms.Collections = make(map[int]*CollectionMeta)
	}
}

func (ms *MetadataStore) migrateLegacy(legacy *legacyMetadata) {
	for _, m := range legacy.Movies {
		if m.TMDBID > 0 {
			ms.Movies[m.TMDBID] = m
		}
	}
	for _, s := range legacy.Series {
		if s.TMDBID > 0 {
			ms.Series[s.TMDBID] = s
		}
	}
	for _, c := range legacy.Collections {
		if c.TMDBID > 0 {
			ms.Collections[c.TMDBID] = c
		}
	}
}

func (ms *MetadataStore) Save() {
	ms.mu.Lock()
	if !ms.dirty {
		ms.mu.Unlock()
		return
	}
	ms.dirty = false
	data, err := json.MarshalIndent(ms, "", "  ")
	ms.mu.Unlock()
	if err != nil {
		return
	}
	tmp := ms.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, ms.path)
}
