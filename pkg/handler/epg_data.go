package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/store"
)

type EPGDataHandler struct {
	epgStore  store.EPGReader
	versioned store.Versioned
}

func NewEPGDataHandler(epgStore store.EPGReader, versioned store.Versioned) *EPGDataHandler {
	return &EPGDataHandler{epgStore: epgStore, versioned: versioned}
}

type epgDataWithPrograms struct {
	ID          string      `json:"id"`
	EPGSourceID string      `json:"epg_source_id"`
	ChannelID   string      `json:"channel_id"`
	Name        string      `json:"name"`
	Icon        string      `json:"icon,omitempty"`
	Programs    any `json:"programs"`
}

func (h *EPGDataHandler) List(w http.ResponseWriter, r *http.Request) {
	sourceIDStr := r.URL.Query().Get("source_id")
	includePrograms := r.URL.Query().Get("programs") == "true"

	etag := h.versioned.ETag()
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	var data []models.EPGData
	var err error

	if sourceIDStr != "" {
		data, err = h.epgStore.ListBySourceID(r.Context(), sourceIDStr)
	} else {
		data, err = h.epgStore.ListEPGData(r.Context())
	}

	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list epg data")
		return
	}

	if !includePrograms {
		respondCacheable(w, r, etag, http.StatusOK, data)
		return
	}

	ids := make([]string, len(data))
	for i, d := range data {
		ids[i] = d.ID
	}
	allPrograms, progErr := h.epgStore.ListProgramsByEPGDataIDs(r.Context(), ids)
	if progErr != nil {
		respondError(w, http.StatusInternalServerError, "failed to list program data")
		return
	}

	results := make([]epgDataWithPrograms, 0, len(data))
	for _, d := range data {
		progs := allPrograms[d.ID]
		if progs == nil {
			progs = []models.ProgramData{}
		}
		results = append(results, epgDataWithPrograms{
			ID:          d.ID,
			EPGSourceID: d.EPGSourceID,
			ChannelID:   d.ChannelID,
			Name:        d.Name,
			Icon:        d.Icon,
			Programs:    progs,
		})
	}
	respondCacheable(w, r, etag, http.StatusOK, results)
}

func (h *EPGDataHandler) NowPlaying(w http.ResponseWriter, r *http.Request) {
	channelID := r.URL.Query().Get("channel_id")

	if channelID == "" {
		nowMap, err := h.epgStore.ListNowPlaying(r.Context(), time.Now())
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to list now playing")
			return
		}
		respondJSON(w, http.StatusOK, nowMap)
		return
	}

	program, err := h.epgStore.GetNowByChannelID(r.Context(), channelID, time.Now())
	if err != nil {
		respondJSON(w, http.StatusOK, nil)
		return
	}
	respondJSON(w, http.StatusOK, program)
}

type guideResponse struct {
	Start    time.Time                          `json:"start"`
	Stop     time.Time                          `json:"stop"`
	Programs map[string][]models.GuideProgram `json:"programs"`
}

func (h *EPGDataHandler) Guide(w http.ResponseWriter, r *http.Request) {
	hours := 6
	if hs := r.URL.Query().Get("hours"); hs != "" {
		if parsed, err := strconv.Atoi(hs); err == nil && parsed > 0 && parsed <= 48 {
			hours = parsed
		}
	}

	var start time.Time
	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if parsed, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = parsed.Truncate(30 * time.Minute)
		}
	}
	if start.IsZero() {
		start = time.Now().Truncate(30 * time.Minute)
	}
	stop := start.Add(time.Duration(hours) * time.Hour)

	programs, err := h.epgStore.ListForGuide(r.Context(), start, stop)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list guide programs")
		return
	}

	grouped := make(map[string][]models.GuideProgram)
	for _, p := range programs {
		grouped[p.ChannelID] = append(grouped[p.ChannelID], p)
	}

	respondJSON(w, http.StatusOK, guideResponse{
		Start:    start,
		Stop:     stop,
		Programs: grouped,
	})
}
