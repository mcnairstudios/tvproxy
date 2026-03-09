package handler

import (
	"net/http"
	"strconv"

	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

// EPGDataHandler handles EPG data HTTP requests.
type EPGDataHandler struct {
	epgDataRepo     *repository.EPGDataRepository
	programDataRepo *repository.ProgramDataRepository
}

// NewEPGDataHandler creates a new EPGDataHandler.
func NewEPGDataHandler(epgDataRepo *repository.EPGDataRepository, programDataRepo *repository.ProgramDataRepository) *EPGDataHandler {
	return &EPGDataHandler{
		epgDataRepo:     epgDataRepo,
		programDataRepo: programDataRepo,
	}
}

// epgDataWithPrograms represents EPG data combined with its programs for the API response.
type epgDataWithPrograms struct {
	ID          int64       `json:"id"`
	EPGSourceID int64       `json:"epg_source_id"`
	ChannelID   string      `json:"channel_id"`
	Name        string      `json:"name"`
	Icon        string      `json:"icon,omitempty"`
	Programs    interface{} `json:"programs"`
}

// List returns all EPG data, optionally filtered by source_id query parameter.
func (h *EPGDataHandler) List(w http.ResponseWriter, r *http.Request) {
	sourceIDStr := r.URL.Query().Get("source_id")

	if sourceIDStr != "" {
		sourceID, err := strconv.ParseInt(sourceIDStr, 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "invalid source_id")
			return
		}

		data, err := h.epgDataRepo.ListBySourceID(r.Context(), sourceID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to list epg data")
			return
		}

		results := make([]epgDataWithPrograms, 0, len(data))
		for _, d := range data {
			programs, err := h.programDataRepo.ListByEPGDataID(r.Context(), d.ID)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "failed to list program data")
				return
			}
			results = append(results, epgDataWithPrograms{
				ID:          d.ID,
				EPGSourceID: d.EPGSourceID,
				ChannelID:   d.ChannelID,
				Name:        d.Name,
				Icon:        d.Icon,
				Programs:    programs,
			})
		}

		respondJSON(w, http.StatusOK, results)
		return
	}

	data, err := h.epgDataRepo.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list epg data")
		return
	}

	results := make([]epgDataWithPrograms, 0, len(data))
	for _, d := range data {
		programs, err := h.programDataRepo.ListByEPGDataID(r.Context(), d.ID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to list program data")
			return
		}
		results = append(results, epgDataWithPrograms{
			ID:          d.ID,
			EPGSourceID: d.EPGSourceID,
			ChannelID:   d.ChannelID,
			Name:        d.Name,
			Icon:        d.Icon,
			Programs:    programs,
		})
	}

	respondJSON(w, http.StatusOK, results)
}
