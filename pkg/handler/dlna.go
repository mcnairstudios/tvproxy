package handler

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gavinmcnair/tvproxy/pkg/config"
	"github.com/gavinmcnair/tvproxy/pkg/service"
)

type DLNAHandler struct {
	dlnaService *service.DLNAService
	config      *config.Config
}

func NewDLNAHandler(dlnaService *service.DLNAService, cfg *config.Config) *DLNAHandler {
	return &DLNAHandler{
		dlnaService: dlnaService,
		config:      cfg,
	}
}

func (h *DLNAHandler) DeviceDescription(w http.ResponseWriter, r *http.Request) {
	if !h.dlnaService.IsEnabled(r.Context()) {
		http.NotFound(w, r)
		return
	}
	baseURL := h.baseURL()
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Write([]byte(h.dlnaService.DeviceDescriptionXML(baseURL)))
}

func (h *DLNAHandler) ContentDirectorySCPD(w http.ResponseWriter, r *http.Request) {
	if !h.dlnaService.IsEnabled(r.Context()) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Write([]byte(h.dlnaService.ContentDirectorySCPD()))
}

func (h *DLNAHandler) ConnectionManagerSCPD(w http.ResponseWriter, r *http.Request) {
	if !h.dlnaService.IsEnabled(r.Context()) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Write([]byte(h.dlnaService.ConnectionManagerSCPD()))
}

func (h *DLNAHandler) ContentDirectoryControl(w http.ResponseWriter, r *http.Request) {
	if !h.dlnaService.IsEnabled(r.Context()) {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	baseURL := h.baseURL()
	soapAction := r.Header.Get("SOAPAction")
	result, err := h.dlnaService.HandleContentDirectoryAction(r.Context(), baseURL, soapAction, body)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Write([]byte(result))
}

func (h *DLNAHandler) ConnectionManagerControl(w http.ResponseWriter, r *http.Request) {
	if !h.dlnaService.IsEnabled(r.Context()) {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	soapAction := r.Header.Get("SOAPAction")
	result, err := h.dlnaService.HandleConnectionManagerAction(r.Context(), soapAction, body)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Write([]byte(result))
}

func (h *DLNAHandler) baseURL() string {
	return fmt.Sprintf("%s:%d", h.config.BaseURL, h.config.Port)
}
