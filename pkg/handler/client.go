package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/gavinmcnair/tvproxy/pkg/models"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

var validMatchTypes = map[string]bool{"exists": true, "contains": true, "equals": true, "prefix": true}

type ClientHandler struct {
	clientService *service.ClientService
}

func NewClientHandler(clientService *service.ClientService) *ClientHandler {
	return &ClientHandler{clientService: clientService}
}

func (h *ClientHandler) List(w http.ResponseWriter, r *http.Request) {
	clients, err := h.clientService.ListClients(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list clients")
		return
	}
	respondJSON(w, http.StatusOK, clients)
}

type clientCreateRequest struct {
	Name       string                   `json:"name"`
	Priority   int                      `json:"priority"`
	ListenPort int                      `json:"listen_port"`
	IsEnabled  bool                     `json:"is_enabled"`
	Rules      []clientMatchRuleRequest `json:"match_rules"`
}

type clientMatchRuleRequest struct {
	HeaderName string `json:"header_name"`
	MatchType  string `json:"match_type"`
	MatchValue string `json:"match_value"`
}

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
	if len(req.Rules) == 0 && req.ListenPort == 0 {
		respondError(w, http.StatusBadRequest, "at least one match rule or a listen port is required")
		return
	}
	if err := validateRules(req.Rules); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	client := &models.Client{
		Name:       req.Name,
		Priority:   req.Priority,
		ListenPort: req.ListenPort,
		IsEnabled:  req.IsEnabled,
	}
	rules := toMatchRules("", req.Rules)

	if err := h.clientService.CreateClient(r.Context(), client, rules); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to create client")
		return
	}

	client, err := h.clientService.GetClient(r.Context(), client.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reload client")
		return
	}
	respondJSON(w, http.StatusCreated, client)
}

func (h *ClientHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	client, err := h.clientService.GetClient(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "client not found")
		return
	}
	respondJSON(w, http.StatusOK, client)
}

func (h *ClientHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	client, err := h.clientService.GetClient(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "client not found")
		return
	}

	var req struct {
		Name            string                   `json:"name"`
		Priority        *int                     `json:"priority"`
		ListenPort      *int                     `json:"listen_port"`
		StreamProfileID *string                  `json:"stream_profile_id"`
		IsEnabled       *bool                    `json:"is_enabled"`
		Rules           []clientMatchRuleRequest `json:"match_rules"`
	}
	if err := decodeJSON(r, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Rules != nil {
		if len(req.Rules) == 0 && client.ListenPort == 0 {
			respondError(w, http.StatusBadRequest, "at least one match rule or a listen port is required")
			return
		}
		if err := validateRules(req.Rules); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if req.Name != "" {
		client.Name = req.Name
	}
	if req.Priority != nil {
		client.Priority = *req.Priority
	}
	if req.ListenPort != nil {
		client.ListenPort = *req.ListenPort
	}
	if req.StreamProfileID != nil {
		client.StreamProfileID = *req.StreamProfileID
	}
	if req.IsEnabled != nil {
		client.IsEnabled = *req.IsEnabled
	}

	var rules []models.ClientMatchRule
	if req.Rules != nil {
		rules = toMatchRules(client.ID, req.Rules)
	}

	if err := h.clientService.UpdateClient(r.Context(), client, rules); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update client")
		return
	}

	client, err = h.clientService.GetClient(r.Context(), client.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to reload client")
		return
	}
	respondJSON(w, http.StatusOK, client)
}

func (h *ClientHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.clientService.DeleteClient(r.Context(), id); err != nil {
		if strings.Contains(err.Error(), "getting client") {
			respondError(w, http.StatusNotFound, "client not found")
		} else {
			respondError(w, http.StatusInternalServerError, "failed to delete client")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateRules(rules []clientMatchRuleRequest) error {
	for _, rule := range rules {
		if rule.HeaderName == "" {
			return errors.New("header_name is required on each rule")
		}
		if !validMatchTypes[rule.MatchType] {
			return errors.New("match_type must be one of: exists, contains, equals, prefix")
		}
		if rule.MatchType != "exists" && rule.MatchValue == "" {
			return errors.New("match_value is required unless match_type is exists")
		}
	}
	return nil
}

func toMatchRules(clientID string, reqs []clientMatchRuleRequest) []models.ClientMatchRule {
	rules := make([]models.ClientMatchRule, len(reqs))
	for i, rr := range reqs {
		rules[i] = models.ClientMatchRule{
			ClientID:   clientID,
			HeaderName: rr.HeaderName,
			MatchType:  rr.MatchType,
			MatchValue: rr.MatchValue,
		}
	}
	return rules
}
