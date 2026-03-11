package handler

import (
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/ffmpeg"
	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/repository"
)

var validMatchTypes = map[string]bool{"exists": true, "contains": true, "equals": true, "prefix": true}

// ClientHandler handles client detection HTTP requests.
type ClientHandler struct {
	clientRepo        *repository.ClientRepository
	streamProfileRepo *repository.StreamProfileRepository
}

// NewClientHandler creates a new ClientHandler.
func NewClientHandler(clientRepo *repository.ClientRepository, streamProfileRepo *repository.StreamProfileRepository) *ClientHandler {
	return &ClientHandler{
		clientRepo:        clientRepo,
		streamProfileRepo: streamProfileRepo,
	}
}

// List returns all clients with their match rules.
func (h *ClientHandler) List(w http.ResponseWriter, r *http.Request) {
	clients, err := h.clientRepo.List(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list clients")
		return
	}

	respondJSON(w, http.StatusOK, clients)
}

type clientCreateRequest struct {
	Name      string                   `json:"name"`
	Priority  int                      `json:"priority"`
	IsEnabled bool                     `json:"is_enabled"`
	Rules     []clientMatchRuleRequest `json:"match_rules"`
}

type clientMatchRuleRequest struct {
	HeaderName string `json:"header_name"`
	MatchType  string `json:"match_type"`
	MatchValue string `json:"match_value"`
}

// Create creates a new client with auto-created stream profile.
func (h *ClientHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req clientCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Rules) == 0 {
		respondError(w, http.StatusBadRequest, "at least one match rule is required")
		return
	}
	if err := validateRules(req.Rules); err != "" {
		respondError(w, http.StatusBadRequest, err)
		return
	}

	// Auto-create a stream profile for this client
	args := ffmpeg.ComposeStreamProfileArgs("m3u", "none", "copy", "mpegts")
	profile := &models.StreamProfile{
		Name:       req.Name,
		StreamMode: "ffmpeg",
		SourceType: "m3u",
		HWAccel:    "none",
		VideoCodec: "copy",
		Container:  "mpegts",
		Command:    "ffmpeg",
		Args:       args,
	}
	if err := h.streamProfileRepo.Create(r.Context(), profile); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create stream profile")
		return
	}

	client := &models.Client{
		Name:            req.Name,
		Priority:        req.Priority,
		StreamProfileID: profile.ID,
		IsEnabled:       req.IsEnabled,
	}

	if err := h.clientRepo.Create(r.Context(), client); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create client")
		return
	}

	// Set match rules
	rules := make([]models.ClientMatchRule, len(req.Rules))
	for i, rr := range req.Rules {
		rules[i] = models.ClientMatchRule{
			ClientID:   client.ID,
			HeaderName: rr.HeaderName,
			MatchType:  rr.MatchType,
			MatchValue: rr.MatchValue,
		}
	}
	if err := h.clientRepo.SetMatchRules(r.Context(), client.ID, rules); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to set match rules")
		return
	}

	// Reload to get hydrated rules
	client, err := h.clientRepo.GetByID(r.Context(), client.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reload client")
		return
	}

	respondJSON(w, http.StatusCreated, client)
}

// Get returns a client by ID.
func (h *ClientHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid client id")
		return
	}

	client, err := h.clientRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "client not found")
		return
	}

	respondJSON(w, http.StatusOK, client)
}

// Update updates a client by ID.
func (h *ClientHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid client id")
		return
	}

	client, err := h.clientRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "client not found")
		return
	}

	var req struct {
		Name            string                   `json:"name"`
		Priority        *int                     `json:"priority"`
		StreamProfileID *int64                   `json:"stream_profile_id"`
		IsEnabled       *bool                    `json:"is_enabled"`
		Rules           []clientMatchRuleRequest `json:"match_rules"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != "" {
		client.Name = req.Name
	}
	if req.Priority != nil {
		client.Priority = *req.Priority
	}
	if req.StreamProfileID != nil {
		client.StreamProfileID = *req.StreamProfileID
	}
	if req.IsEnabled != nil {
		client.IsEnabled = *req.IsEnabled
	}

	if err := h.clientRepo.Update(r.Context(), client); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update client")
		return
	}

	// Update rules if provided
	if req.Rules != nil {
		if len(req.Rules) == 0 {
			respondError(w, http.StatusBadRequest, "at least one match rule is required")
			return
		}
		if errMsg := validateRules(req.Rules); errMsg != "" {
			respondError(w, http.StatusBadRequest, errMsg)
			return
		}
		rules := make([]models.ClientMatchRule, len(req.Rules))
		for i, rr := range req.Rules {
			rules[i] = models.ClientMatchRule{
				ClientID:   client.ID,
				HeaderName: rr.HeaderName,
				MatchType:  rr.MatchType,
				MatchValue: rr.MatchValue,
			}
		}
		if err := h.clientRepo.SetMatchRules(r.Context(), client.ID, rules); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to update match rules")
			return
		}
	}

	// Reload
	client, err = h.clientRepo.GetByID(r.Context(), client.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reload client")
		return
	}

	respondJSON(w, http.StatusOK, client)
}

// Delete deletes a client by ID. Also cleans up the linked stream profile if orphaned.
func (h *ClientHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid client id")
		return
	}

	client, err := h.clientRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "client not found")
		return
	}

	profileID := client.StreamProfileID

	if err := h.clientRepo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to delete client")
		return
	}

	// Clean up orphaned stream profile (only if not referenced by other clients and not a system profile)
	profile, err := h.streamProfileRepo.GetByID(r.Context(), profileID)
	if err == nil && !profile.IsSystem {
		referenced, err := h.clientRepo.IsStreamProfileReferenced(r.Context(), profileID)
		if err == nil && !referenced {
			_ = h.streamProfileRepo.Delete(r.Context(), profileID)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func validateRules(rules []clientMatchRuleRequest) string {
	for _, rule := range rules {
		if rule.HeaderName == "" {
			return "header_name is required on each rule"
		}
		if !validMatchTypes[rule.MatchType] {
			return "match_type must be one of: exists, contains, equals, prefix"
		}
		if rule.MatchType != "exists" && rule.MatchValue == "" {
			return "match_value is required unless match_type is exists"
		}
	}
	return ""
}
