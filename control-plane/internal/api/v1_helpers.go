package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/docker"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/helpers"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelprovider"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// Helpers are operator agents (e.g. Hermes) the owner adds. Each runs as its
// own hardened container from a pinned image, keeps its state in
// <data>/helpers/<id>, reaches its model and sandboxd's app tools only through
// the proxy with its helper secret, and is chatted with through sandboxd.

type v1Helper struct {
	ID           string `json:"id"`
	Package      string `json:"package"`
	Name         string `json:"name"`
	ConnectionID string `json:"connection_id"`
	Model        string `json:"model"`
	Status       string `json:"status"` // running | starting | stopped | missing
	Telegram     bool   `json:"telegram"`
	AppsToday    int    `json:"apps_today"`
	AppsPerDay   int    `json:"apps_per_day"`
	CreatedAt    string `json:"created_at"`
}

// helperAppsPerDay caps how many apps one helper may create per day.
const helperAppsPerDay = 10

func helperContainer(id string) string { return "h-" + id }

func (s *Server) helperView(ctx context.Context, h *store.Helper) v1Helper {
	status := "missing"
	if s.Docker != nil {
		if c, err := s.Docker.Inspect(ctx, helperContainer(h.ID)); err == nil {
			status = "stopped"
			if c.State.Running {
				status = "running"
			}
		}
	}
	today := 0
	if h.AppsDay == time.Now().UTC().Format("2006-01-02") {
		today = h.AppsToday
	}
	return v1Helper{ID: h.ID, Package: h.Package, Name: h.Name, ConnectionID: h.ConnectionID, Model: h.Model,
		Status: status, Telegram: len(h.TelegramCiphertext) > 0, AppsToday: today, AppsPerDay: helperAppsPerDay,
		CreatedAt: h.CreatedAt}
}

// GET /v1/helper-packages
func (s *Server) v1ListHelperPackages(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"packages": helpers.List()})
}

// GET /v1/helpers
func (s *Server) v1ListHelpers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListHelpersForOwner(r.Context(), tenantToken(r))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]v1Helper, 0, len(rows))
	for _, h := range rows {
		out = append(out, s.helperView(r.Context(), h))
	}
	writeJSON(w, http.StatusOK, map[string]any{"helpers": out})
}

type v1CreateHelperReq struct {
	Package              string `json:"package"`
	Name                 string `json:"name"`
	ConnectionID         string `json:"connection_id"`
	Model                string `json:"model"`
	TelegramBotToken     string `json:"telegram_bot_token"`
	TelegramAllowedUsers string `json:"telegram_allowed_users"`
}

// POST /v1/helpers — add a helper and start it.
func (s *Server) v1CreateHelper(w http.ResponseWriter, r *http.Request) {
	if s.Docker == nil || s.Secrets == nil || s.ModelProviders == nil || s.AgentProxyURL == "" || s.HelperRoot == "" {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "helpers are not available on this engine")
		return
	}
	var req v1CreateHelperReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	pkg, ok := helpers.Get(req.Package)
	if !ok {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "unknown helper")
		return
	}
	model, err := s.helperModel(r.Context(), req.ConnectionID, req.Model)
	if err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	users := strings.TrimSpace(req.TelegramAllowedUsers)
	token := strings.TrimSpace(req.TelegramBotToken)
	if token != "" && users == "" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request",
			"add your Telegram user ID so only you can talk to the helper")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = pkg.Label
	}
	h := &store.Helper{ID: strings.ToLower(newULID()), OwnerToken: tenantToken(r), Package: pkg.ID, Name: name,
		ConnectionID: req.ConnectionID, Model: model, TelegramAllowedUsers: users,
		CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	apiKey, err := randomSecret()
	if err == nil {
		h.APIKeyCiphertext, h.APIKeyNonce, err = s.Secrets.Seal([]byte(apiKey))
	}
	if err == nil && token != "" {
		h.TelegramCiphertext, h.TelegramNonce, err = s.Secrets.Seal([]byte(token))
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not protect the helper's keys")
		return
	}
	h.SecretHash = hashSecret("pending-" + h.ID) // replaced when the container is created
	if err := s.Store.CreateHelper(r.Context(), h); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.provisionHelper(r.Context(), h, pkg); err != nil {
		s.Log.Warn("helper start failed", "helper", h.ID, "err", err.Error())
		writeJSON(w, http.StatusAccepted, map[string]any{"helper": s.helperView(r.Context(), h),
			"warning": "The helper was added but could not start: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"helper": s.helperView(r.Context(), h)})
}

// helperModel checks the connection speaks the OpenAI chat protocol (what the
// helpers use) and resolves its model.
func (s *Server) helperModel(ctx context.Context, connectionID, model string) (string, error) {
	conns, err := s.ModelProviders.List(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range conns {
		if c.ID != connectionID {
			continue
		}
		if c.Protocol != modelprovider.OpenAI {
			return "", errors.New("choose an OpenAI-compatible model connection for this helper")
		}
		if len(c.Models) == 0 {
			return "", errors.New("this model connection has no models")
		}
		if model == "" {
			return c.Models[0].ID, nil
		}
		for _, m := range c.Models {
			if m.ID == model {
				return model, nil
			}
		}
		return "", errors.New("this model is not part of the connection")
	}
	return "", errors.New("choose a model connection in Settings first")
}

// provisionHelper (re)creates the helper's container with a fresh helper
// secret: it writes the helper's config into its state directory and starts
// the pinned image under sandboxd's hardened profile.
func (s *Server) provisionHelper(ctx context.Context, h *store.Helper, pkg helpers.Package) error {
	secret, err := randomSecret()
	if err != nil {
		return err
	}
	apiKey, err := s.Secrets.Open(h.APIKeyCiphertext, h.APIKeyNonce)
	if err != nil {
		return errors.New("the helper's API key cannot be read")
	}
	dir := filepath.Join(s.HelperRoot, h.ID)
	uid, gid := 0, 0
	if parts := strings.SplitN(pkg.User, ":", 2); len(parts) == 2 {
		uid, _ = strconv.Atoi(parts[0])
		gid, _ = strconv.Atoi(parts[1])
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		return fmt.Errorf("prepare the helper's storage: %w", err)
	}
	config, err := helpers.RenderHermesConfig(helpers.HermesConfig{
		ProxyURL: s.AgentProxyURL, HelperID: h.ID, Secret: secret, Model: h.Model})
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(config), 0o600); err != nil {
		return err
	}
	if err := os.Chown(cfgPath, uid, gid); err != nil {
		return err
	}
	if err := s.Store.RotateHelperSecret(ctx, h.ID, hashSecret(secret)); err != nil {
		return err
	}
	h.SecretHash = hashSecret(secret)
	env := []string{"API_SERVER_ENABLED=true", "API_SERVER_HOST=0.0.0.0",
		"API_SERVER_PORT=" + strconv.Itoa(pkg.APIPort), "API_SERVER_KEY=" + string(apiKey)}
	if len(h.TelegramCiphertext) > 0 {
		token, err := s.Secrets.Open(h.TelegramCiphertext, h.TelegramNonce)
		if err != nil {
			return errors.New("the Telegram token cannot be read")
		}
		env = append(env, "TELEGRAM_BOT_TOKEN="+string(token), "TELEGRAM_ALLOWED_USERS="+h.TelegramAllowedUsers)
	}
	name := helperContainer(h.ID)
	_ = s.Docker.Remove(ctx, name)
	_, err = s.Docker.Run(ctx, docker.RunSpec{
		Name: name, Hostname: name, Network: s.Network, Userns: s.Userns, Runtime: s.Runtime,
		ReadOnly: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges"},
		Memory: pkg.Memory, MemorySwap: pkg.Memory, PidsLimit: 1024, ShmSize: pkg.ShmSize,
		// The image's supervisor (s6) executes from /run.
		Tmpfs:   []string{"/run:exec,mode=755,uid=" + strconv.Itoa(uid), "/tmp"},
		User:    pkg.User,
		Restart: "unless-stopped", // always on: survives engine and computer restarts
		Env:     env,
		Volumes: []string{dir + ":" + pkg.StatePath},
		Labels:  []string{"sandboxd.helper=" + h.ID, "sandboxd.helper.package=" + pkg.ID},
		Image:   pkg.Image, Cmd: pkg.Cmd,
	})
	return err
}

func (s *Server) ownHelper(w http.ResponseWriter, r *http.Request) (*store.Helper, bool) {
	h, err := s.Store.GetHelperForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such helper")
		return nil, false
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return nil, false
	}
	return h, true
}

// GET /v1/helpers/{id}
func (s *Server) v1GetHelper(w http.ResponseWriter, r *http.Request) {
	if h, ok := s.ownHelper(w, r); ok {
		writeJSON(w, http.StatusOK, s.helperView(r.Context(), h))
	}
}

// POST /v1/helpers/{id}/start — starts it, re-creating the container (with a
// fresh secret) when it is missing.
func (s *Server) v1StartHelper(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownHelper(w, r)
	if !ok {
		return
	}
	pkg, _ := helpers.Get(h.Package)
	var err error
	if _, ierr := s.Docker.Inspect(r.Context(), helperContainer(h.ID)); ierr != nil {
		err = s.provisionHelper(r.Context(), h, pkg)
	} else {
		err = s.Docker.Start(r.Context(), helperContainer(h.ID))
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not start the helper: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.helperView(r.Context(), h))
}

// POST /v1/helpers/{id}/stop
func (s *Server) v1StopHelper(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownHelper(w, r)
	if !ok {
		return
	}
	if err := s.Docker.Stop(r.Context(), helperContainer(h.ID), 20); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.helperView(r.Context(), h))
}

// DELETE /v1/helpers/{id} — removes the helper, its container and its memory.
func (s *Server) v1DeleteHelper(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownHelper(w, r)
	if !ok {
		return
	}
	if err := s.Docker.Remove(r.Context(), helperContainer(h.ID)); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.Store.DeleteHelper(r.Context(), h.ID); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if s.HelperRoot != "" && h.ID != "" {
		_ = os.RemoveAll(filepath.Join(s.HelperRoot, h.ID))
	}
	w.WriteHeader(http.StatusNoContent)
}

type v1HelperChatReq struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// POST /v1/helpers/{id}/chat — one turn with the helper through its own
// OpenAI-compatible API. sandboxd holds the helper's API key.
func (s *Server) v1HelperChat(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownHelper(w, r)
	if !ok {
		return
	}
	var req v1HelperChatReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil || len(req.Messages) == 0 {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "send at least one message")
		return
	}
	pkg, _ := helpers.Get(h.Package)
	apiKey, err := s.Secrets.Open(h.APIKeyCiphertext, h.APIKeyNonce)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "the helper's API key cannot be read")
		return
	}
	body, _ := json.Marshal(map[string]any{"model": "hermes-agent", "messages": req.Messages, "stream": false})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute) // building an app can take a while
	defer cancel()
	up, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("http://%s:%d/v1/chat/completions", helperContainer(h.ID), pkg.APIPort), bytes.NewReader(body))
	up.Header.Set("Authorization", "Bearer "+string(apiKey))
	up.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(up)
	if err != nil {
		writeV1Err(w, http.StatusBadGateway, "unavailable", "the helper is not answering; start it and try again")
		return
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &out) != nil || len(out.Choices) == 0 {
		writeV1Err(w, http.StatusBadGateway, "helper_error", "the helper could not answer: "+strings.TrimSpace(string(raw[:min(len(raw), 300)])))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reply": out.Choices[0].Message.Content})
}

func randomSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
