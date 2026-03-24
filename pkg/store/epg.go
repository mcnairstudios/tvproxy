package store

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/models"
)

type epgSnapshot struct {
	EPGData  map[string]models.EPGData
	Programs map[string]models.ProgramData
}

type EPGStoreImpl struct {
	mu              sync.RWMutex
	epgData         map[string]models.EPGData
	programs        map[string]models.ProgramData
	bySourceID      map[string][]string
	programsByEPGID map[string][]string
	epgByChannelID  map[string]string

	path string
	log  zerolog.Logger
}

func NewEPGStore(path string, log zerolog.Logger) *EPGStoreImpl {
	return &EPGStoreImpl{
		epgData:         make(map[string]models.EPGData),
		programs:        make(map[string]models.ProgramData),
		bySourceID:      make(map[string][]string),
		programsByEPGID: make(map[string][]string),
		epgByChannelID:  make(map[string]string),
		path:            path,
		log:             log.With().Str("store", "epg").Logger(),
	}
}

func (s *EPGStoreImpl) ListEPGData(_ context.Context) ([]models.EPGData, error) {
	s.mu.RLock()
	items := make([]models.EPGData, 0, len(s.epgData))
	for _, v := range s.epgData {
		items = append(items, v)
	}
	s.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *EPGStoreImpl) ListBySourceID(_ context.Context, sourceID string) ([]models.EPGData, error) {
	s.mu.RLock()
	ids := s.bySourceID[sourceID]
	result := make([]models.EPGData, 0, len(ids))
	for _, id := range ids {
		if d, ok := s.epgData[id]; ok {
			result = append(result, d)
		}
	}
	s.mu.RUnlock()

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s *EPGStoreImpl) GetNowByChannelID(_ context.Context, channelID string, now time.Time) (*models.ProgramData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	epgDataID, ok := s.epgByChannelID[channelID]
	if !ok {
		return nil, fmt.Errorf("no epg data for channel: %s", channelID)
	}

	var best *models.ProgramData
	for _, pid := range s.programsByEPGID[epgDataID] {
		p, exists := s.programs[pid]
		if !exists {
			continue
		}
		if !p.Start.After(now) && p.Stop.After(now) {
			if best == nil || p.Start.After(best.Start) {
				cp := p
				best = &cp
			}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no current program for channel: %s", channelID)
	}
	return best, nil
}

func (s *EPGStoreImpl) ListNowPlaying(_ context.Context, now time.Time) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	for chID, epgID := range s.epgByChannelID {
		for _, pid := range s.programsByEPGID[epgID] {
			p, exists := s.programs[pid]
			if !exists {
				continue
			}
			if !p.Start.After(now) && p.Stop.After(now) {
				result[chID] = p.Title
				break
			}
		}
	}
	return result, nil
}

func (s *EPGStoreImpl) ListForGuide(_ context.Context, start, end time.Time) ([]models.GuideProgram, error) {
	s.mu.RLock()
	var result []models.GuideProgram
	for chID, epgID := range s.epgByChannelID {
		for _, pid := range s.programsByEPGID[epgID] {
			p, exists := s.programs[pid]
			if !exists {
				continue
			}
			if p.Start.Before(end) && p.Stop.After(start) {
				result = append(result, models.GuideProgram{
					ChannelID:   chID,
					Title:       p.Title,
					Description: p.Description,
					Start:       p.Start,
					Stop:        p.Stop,
					Category:    p.Category,
				})
			}
		}
	}
	s.mu.RUnlock()

	sort.Slice(result, func(i, j int) bool {
		if result[i].ChannelID != result[j].ChannelID {
			return result[i].ChannelID < result[j].ChannelID
		}
		return result[i].Start.Before(result[j].Start)
	})
	return result, nil
}

func (s *EPGStoreImpl) ListPrograms(_ context.Context, epgDataID string) ([]models.ProgramData, error) {
	s.mu.RLock()
	ids := s.programsByEPGID[epgDataID]
	result := make([]models.ProgramData, 0, len(ids))
	for _, id := range ids {
		if p, ok := s.programs[id]; ok {
			result = append(result, p)
		}
	}
	s.mu.RUnlock()

	sort.Slice(result, func(i, j int) bool {
		return result[i].Start.Before(result[j].Start)
	})
	return result, nil
}

func (s *EPGStoreImpl) ListProgramsByEPGDataIDs(_ context.Context, ids []string) (map[string][]models.ProgramData, error) {
	s.mu.RLock()
	result := make(map[string][]models.ProgramData, len(ids))
	for _, epgID := range ids {
		pids := s.programsByEPGID[epgID]
		progs := make([]models.ProgramData, 0, len(pids))
		for _, pid := range pids {
			if p, ok := s.programs[pid]; ok {
				progs = append(progs, p)
			}
		}
		sort.Slice(progs, func(i, j int) bool {
			return progs[i].Start.Before(progs[j].Start)
		})
		result[epgID] = progs
	}
	s.mu.RUnlock()
	return result, nil
}

func (s *EPGStoreImpl) BulkCreateEPGData(_ context.Context, data []models.EPGData) error {
	for i := range data {
		if data[i].ID == "" {
			data[i].ID = uuid.New().String()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range data {
		if oldID, exists := s.epgByChannelID[data[i].ChannelID]; exists {
			if old, ok := s.epgData[oldID]; ok {
				s.removeFromSourceIndex(old.EPGSourceID, oldID)
				for _, pid := range s.programsByEPGID[oldID] {
					delete(s.programs, pid)
				}
				delete(s.programsByEPGID, oldID)
			}
			delete(s.epgData, oldID)
		}

		s.epgData[data[i].ID] = data[i]
		s.bySourceID[data[i].EPGSourceID] = append(s.bySourceID[data[i].EPGSourceID], data[i].ID)
		s.epgByChannelID[data[i].ChannelID] = data[i].ID
	}
	return nil
}

func (s *EPGStoreImpl) removeFromSourceIndex(sourceID, epgID string) {
	ids := s.bySourceID[sourceID]
	for j, id := range ids {
		if id == epgID {
			s.bySourceID[sourceID] = append(ids[:j], ids[j+1:]...)
			return
		}
	}
}

func (s *EPGStoreImpl) BulkCreatePrograms(_ context.Context, programs []models.ProgramData) error {
	for i := range programs {
		if programs[i].ID == "" {
			programs[i].ID = uuid.New().String()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range programs {
		s.programs[programs[i].ID] = programs[i]
		s.programsByEPGID[programs[i].EPGDataID] = append(s.programsByEPGID[programs[i].EPGDataID], programs[i].ID)
	}
	return nil
}

func (s *EPGStoreImpl) DeleteBySourceID(_ context.Context, sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, epgID := range s.bySourceID[sourceID] {
		if d, ok := s.epgData[epgID]; ok {
			delete(s.epgByChannelID, d.ChannelID)
		}
		delete(s.epgData, epgID)

		for _, pid := range s.programsByEPGID[epgID] {
			delete(s.programs, pid)
		}
		delete(s.programsByEPGID, epgID)
	}
	delete(s.bySourceID, sourceID)
	return nil
}

func (s *EPGStoreImpl) DeleteProgramsByEPGDataID(_ context.Context, epgDataID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, pid := range s.programsByEPGID[epgDataID] {
		delete(s.programs, pid)
	}
	delete(s.programsByEPGID, epgDataID)
	return nil
}

func (s *EPGStoreImpl) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.epgData = make(map[string]models.EPGData)
	s.programs = make(map[string]models.ProgramData)
	s.bySourceID = make(map[string][]string)
	s.programsByEPGID = make(map[string][]string)
	s.epgByChannelID = make(map[string]string)
	return nil
}

func (s *EPGStoreImpl) Save() error {
	s.mu.RLock()
	snap := epgSnapshot{
		EPGData:  make(map[string]models.EPGData, len(s.epgData)),
		Programs: make(map[string]models.ProgramData, len(s.programs)),
	}
	for k, v := range s.epgData {
		snap.EPGData[k] = v
	}
	for k, v := range s.programs {
		snap.Programs[k] = v
	}
	s.mu.RUnlock()

	if err := saveGob(s.path, snap); err != nil {
		return fmt.Errorf("saving epg store: %w", err)
	}
	s.log.Info().
		Int("epg_channels", len(snap.EPGData)).
		Int("programs", len(snap.Programs)).
		Msg("saved epg store")
	return nil
}

func (s *EPGStoreImpl) Load() error {
	var snap epgSnapshot
	if err := loadGob(s.path, &snap); err != nil {
		if os.IsNotExist(err) {
			s.log.Info().Msg("no epg store file found, starting empty")
			return nil
		}
		return fmt.Errorf("loading epg store: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.epgData = snap.EPGData
	s.programs = snap.Programs

	s.bySourceID = make(map[string][]string)
	s.programsByEPGID = make(map[string][]string)
	s.epgByChannelID = make(map[string]string)

	for id, d := range snap.EPGData {
		s.bySourceID[d.EPGSourceID] = append(s.bySourceID[d.EPGSourceID], id)
		s.epgByChannelID[d.ChannelID] = id
	}
	for id, p := range snap.Programs {
		s.programsByEPGID[p.EPGDataID] = append(s.programsByEPGID[p.EPGDataID], id)
	}

	s.log.Info().
		Int("epg_channels", len(snap.EPGData)).
		Int("programs", len(snap.Programs)).
		Msg("loaded epg store")
	return nil
}
