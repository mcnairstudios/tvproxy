package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/gavinmcnair/tvproxy/pkg/middleware"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type SchedulerHandler struct {
	schedulerService *service.SchedulerService
	log              zerolog.Logger
}

func NewSchedulerHandler(schedulerService *service.SchedulerService, log zerolog.Logger) *SchedulerHandler {
	return &SchedulerHandler{
		schedulerService: schedulerService,
		log:              log.With().Str("handler", "scheduler").Logger(),
	}
}

type scheduleRequest struct {
	ChannelID    string `json:"channel_id"`
	ChannelName  string `json:"channel_name"`
	ProgramTitle string `json:"program_title"`
	StartAt      string `json:"start_at"`
	StopAt       string `json:"stop_at"`
}

func (h *SchedulerHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req scheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ChannelID == "" {
		respondError(w, http.StatusBadRequest, "channel_id is required")
		return
	}

	startAt, err := time.Parse(time.RFC3339, req.StartAt)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid start_at time format")
		return
	}
	stopAt, err := time.Parse(time.RFC3339, req.StopAt)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid stop_at time format")
		return
	}

	rec := &models.ScheduledRecording{
		UserID:       user.UserID,
		ChannelID:    req.ChannelID,
		ChannelName:  req.ChannelName,
		ProgramTitle: req.ProgramTitle,
		StartAt:      startAt,
		StopAt:       stopAt,
	}

	if err := h.schedulerService.Schedule(r.Context(), rec); err != nil {
		if errors.Is(err, service.ErrScheduleConflict) {
			respondError(w, http.StatusConflict, "overlapping recording already scheduled")
			return
		}
		h.log.Error().Err(err).Msg("schedule recording failed")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, rec)
}

func (h *SchedulerHandler) List(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	list, err := h.schedulerService.List(r.Context(), user.UserID, user.IsAdmin)
	if err != nil {
		h.log.Error().Err(err).Msg("list scheduled recordings failed")
		respondError(w, http.StatusInternalServerError, "failed to list scheduled recordings")
		return
	}
	if list == nil {
		list = []models.ScheduledRecording{}
	}
	respondJSON(w, http.StatusOK, list)
}

func (h *SchedulerHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id := chi.URLParam(r, "id")
	rec, err := h.schedulerService.Get(r.Context(), id, user.UserID, user.IsAdmin)
	if err != nil {
		if errors.Is(err, service.ErrRecordingNotFound) {
			respondError(w, http.StatusNotFound, "scheduled recording not found")
			return
		}
		if errors.Is(err, service.ErrNotAuthorized) {
			respondError(w, http.StatusForbidden, "not authorized")
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, rec)
}

func (h *SchedulerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	id := chi.URLParam(r, "id")
	if err := h.schedulerService.Delete(r.Context(), id, user.UserID, user.IsAdmin); err != nil {
		if errors.Is(err, service.ErrRecordingNotFound) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if errors.Is(err, service.ErrNotAuthorized) {
			respondError(w, http.StatusForbidden, "not authorized")
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
