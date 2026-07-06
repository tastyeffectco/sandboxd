// v1_app_config.go — app-scoped config and secrets, owned by the
// control plane (not Docker env, workspace files, or task logs).
// Sensitive values are AES-256-GCM-encrypted at rest and write-only over
// the API (GET returns metadata only). Scoped to the app, which is
// scoped to the API tenant. Plaintext is never logged or audited.
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/events"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

var validAccessPolicies = map[string]bool{
	"control_plane_only": true, // never leaves sandboxd (default)
	"agent_access":       true, // agent task may request via the broker
	"runtime_access":     true, // app runtime may request via the broker
	"both":               true,
}

type v1ConfigItem struct {
	Key          string  `json:"key"`
	Sensitive    bool    `json:"sensitive"`
	AccessPolicy string  `json:"access_policy"`
	ValueSet     bool    `json:"value_set"`
	Value        *string `json:"value,omitempty"` // non-sensitive only
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// redactConfig builds the API view. A sensitive value is never returned.
func redactConfig(c *store.AppConfig) v1ConfigItem {
	item := v1ConfigItem{
		Key:          c.Key,
		Sensitive:    c.Sensitive,
		AccessPolicy: c.AccessPolicy,
		CreatedAt:    c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    c.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if c.Sensitive {
		item.ValueSet = len(c.ValueCiphertext) > 0
	} else if c.ValuePlaintext.Valid {
		item.ValueSet = true
		v := c.ValuePlaintext.String
		item.Value = &v
	}
	return item
}

// appForConfig resolves the tenant-owned app or writes the error. A
// missing or cross-tenant app is a 404 (no existence leak).
func (s *Server) appForConfig(w http.ResponseWriter, r *http.Request) (*store.App, bool) {
	app, err := s.Store.GetAppForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return nil, false
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return nil, false
	}
	return app, true
}

func validConfigKey(k string) bool {
	if k == "" || len(k) > 256 {
		return false
	}
	return strings.IndexFunc(k, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

type v1CreateConfigReq struct {
	Key          string `json:"key"`
	Value        string `json:"value"`
	Sensitive    bool   `json:"sensitive"`
	AccessPolicy string `json:"access_policy"`
}

// v1CreateAppConfig — POST /v1/apps/{id}/config.
func (s *Server) v1CreateAppConfig(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appForConfig(w, r)
	if !ok {
		return
	}
	var req v1CreateConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	if !validConfigKey(req.Key) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "key is required (<=256 chars, no control characters)")
		return
	}
	if req.AccessPolicy == "" {
		req.AccessPolicy = "control_plane_only"
	}
	if !validAccessPolicies[req.AccessPolicy] {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid access_policy")
		return
	}

	c := &store.AppConfig{ID: newULID(), AppID: app.ID, Key: req.Key,
		Sensitive: req.Sensitive, AccessPolicy: req.AccessPolicy}
	if err := s.encodeConfigValue(c, req.Value); err != nil {
		writeV1Err(w, http.StatusServiceUnavailable, "internal", err.Error())
		return
	}
	if err := s.Store.CreateAppConfig(r.Context(), c); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeV1Err(w, http.StatusConflict, "conflict", "config key already exists; PATCH to update")
			return
		}
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.auditConfig(r, "app_config.create", app.ID, c.Key)
	s.recordEvent(r, events.Event{Type: events.ConfigCreated, Severity: events.SeverityInfo,
		Message: "Config key created: " + c.Key, AppID: app.ID,
		Payload: map[string]any{"key": c.Key, "sensitive": c.Sensitive}})
	writeJSON(w, http.StatusCreated, redactConfig(c))
}

// v1ListAppConfig — GET /v1/apps/{id}/config (metadata; redacted).
func (s *Server) v1ListAppConfig(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appForConfig(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.ListAppConfig(r.Context(), app.ID)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]v1ConfigItem, 0, len(rows))
	for _, c := range rows {
		out = append(out, redactConfig(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": out})
}

type v1PatchConfigReq struct {
	Value        *string `json:"value"`
	Sensitive    *bool   `json:"sensitive"`
	AccessPolicy *string `json:"access_policy"`
}

// v1PatchAppConfig — PATCH /v1/apps/{id}/config/{key}.
func (s *Server) v1PatchAppConfig(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appForConfig(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	existing, err := s.Store.GetAppConfig(r.Context(), app.ID, key)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such config key")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var req v1PatchConfigReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}

	updated := *existing
	if req.AccessPolicy != nil {
		if !validAccessPolicies[*req.AccessPolicy] {
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid access_policy")
			return
		}
		updated.AccessPolicy = *req.AccessPolicy
	}
	if req.Sensitive != nil {
		updated.Sensitive = *req.Sensitive
	}
	// Re-encode the stored value only when the value or its sensitivity
	// changes. A policy-only change keeps the existing bytes untouched.
	if req.Value != nil || updated.Sensitive != existing.Sensitive {
		plaintext, err := s.effectiveConfigValue(existing, req.Value)
		if err != nil {
			writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		if err := s.encodeConfigValue(&updated, plaintext); err != nil {
			writeV1Err(w, http.StatusServiceUnavailable, "internal", err.Error())
			return
		}
	}
	if err := s.Store.UpdateAppConfig(r.Context(), app.ID, key, &updated); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	got, _ := s.Store.GetAppConfig(r.Context(), app.ID, key)
	s.auditConfig(r, "app_config.update", app.ID, key)
	s.recordEvent(r, events.Event{Type: events.ConfigUpdated, Severity: events.SeverityInfo,
		Message: "Config key updated: " + key, AppID: app.ID,
		Payload: map[string]any{"key": key}})
	writeJSON(w, http.StatusOK, redactConfig(got))
}

// v1DeleteAppConfig — DELETE /v1/apps/{id}/config/{key}.
func (s *Server) v1DeleteAppConfig(w http.ResponseWriter, r *http.Request) {
	app, ok := s.appForConfig(w, r)
	if !ok {
		return
	}
	key := r.PathValue("key")
	if err := s.Store.DeleteAppConfig(r.Context(), app.ID, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeV1Err(w, http.StatusNotFound, "not_found", "no such config key")
			return
		}
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.auditConfig(r, "app_config.delete", app.ID, key)
	s.recordEvent(r, events.Event{Type: events.ConfigDeleted, Severity: events.SeverityInfo,
		Message: "Config key deleted: " + key, AppID: app.ID,
		Payload: map[string]any{"key": key}})
	w.WriteHeader(http.StatusNoContent)
}

// encodeConfigValue stores plaintext into c per c.Sensitive: encrypted
// (ciphertext+nonce) when sensitive, plaintext column otherwise.
func (s *Server) encodeConfigValue(c *store.AppConfig, plaintext string) error {
	if c.Sensitive {
		if s.Secrets == nil {
			return errors.New("secrets encryption is not configured on this host")
		}
		ct, nonce, err := s.Secrets.Seal([]byte(plaintext))
		if err != nil {
			return err
		}
		c.ValueCiphertext, c.ValueNonce = ct, nonce
		c.ValuePlaintext = sql.NullString{}
		return nil
	}
	c.ValuePlaintext = sql.NullString{String: plaintext, Valid: true}
	c.ValueCiphertext, c.ValueNonce = nil, nil
	return nil
}

// effectiveConfigValue returns the new plaintext to store: the request's
// value if provided, otherwise the existing value (decrypted if it was
// sensitive). Used only internally for re-encoding; never returned via API.
func (s *Server) effectiveConfigValue(existing *store.AppConfig, reqValue *string) (string, error) {
	if reqValue != nil {
		return *reqValue, nil
	}
	if existing.Sensitive {
		if s.Secrets == nil {
			return "", errors.New("secrets encryption is not configured on this host")
		}
		pt, err := s.Secrets.Open(existing.ValueCiphertext, existing.ValueNonce)
		if err != nil {
			return "", err
		}
		return string(pt), nil
	}
	return existing.ValuePlaintext.String, nil
}

// auditConfig records a config mutation. The value is NEVER included.
func (s *Server) auditConfig(r *http.Request, action, appID, key string) {
	s.auditAction(r, audit.Entry{
		Action: action, Target: appID,
		Detail: map[string]any{"key": key},
	})
}
