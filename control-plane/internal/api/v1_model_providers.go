package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelprovider"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func (s *Server) modelProvidersAvailable(w http.ResponseWriter) bool {
	if s.ModelProviders == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "model provider connections are unavailable")
		return false
	}
	return true
}

func (s *Server) v1ListModelProviders(w http.ResponseWriter, r *http.Request) {
	if !s.modelProvidersAvailable(w) {
		return
	}
	list, err := s.ModelProviders.List(r.Context())
	if err != nil {
		writeV1Err(w, 500, "internal", "could not list model provider connections")
		return
	}
	writeJSON(w, 200, map[string]any{"providers": list})
}

func (s *Server) v1SaveModelProvider(w http.ResponseWriter, r *http.Request) {
	if !s.modelProvidersAvailable(w) {
		return
	}
	var input modelprovider.Input
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeV1Err(w, 400, "invalid_request", "invalid provider configuration JSON")
		return
	}
	c, err := s.ModelProviders.Save(r.Context(), r.PathValue("id"), input)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, 404, "not_found", "no such model provider connection")
		return
	}
	if err != nil {
		writeV1Err(w, 400, "invalid_request", err.Error())
		return
	}
	s.auditAction(r, audit.Entry{Action: "model_provider.save", Target: c.ID})
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	writeJSON(w, status, c)
}

func (s *Server) v1DeleteModelProvider(w http.ResponseWriter, r *http.Request) {
	if !s.modelProvidersAvailable(w) {
		return
	}
	err := s.ModelProviders.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, 404, "not_found", "no such model provider connection")
		return
	}
	if err != nil {
		writeV1Err(w, 500, "internal", "could not delete model provider connection")
		return
	}
	s.auditAction(r, audit.Entry{Action: "model_provider.delete", Target: r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) v1TestModelProvider(w http.ResponseWriter, r *http.Request) {
	if !s.modelProvidersAvailable(w) {
		return
	}
	var input struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeV1Err(w, 400, "invalid_request", "expected a JSON object with an optional model")
		return
	}
	err := s.ModelProviders.Probe(r.Context(), r.PathValue("id"), input.Model)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, 404, "not_found", "no such model provider connection")
		return
	}
	if err != nil {
		writeV1Err(w, 502, "provider_test_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
