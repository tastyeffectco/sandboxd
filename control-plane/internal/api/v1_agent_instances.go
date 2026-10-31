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

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/docker"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelprovider"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// Agents are operator agents (e.g. Hermes) the owner adds. Each runs as its
// own hardened container from a pinned image, keeps its state in
// <data>/agents/<id>, reaches its model and sandboxd's app tools only through
// the proxy with its agent secret, and is chatted with through sandboxd.

type v1AgentInstance struct {
	ID           string `json:"id"`
	Package      string `json:"package"`
	Name         string `json:"name"`
	ConnectionID string `json:"connection_id"`
	Model        string `json:"model"`
	Status       string `json:"status"` // running | starting | stopped | missing
	Telegram     bool   `json:"telegram"`
	TelegramBot  string `json:"telegram_bot,omitempty"`
	WhatsApp     string `json:"whatsapp,omitempty"` // the linked number
	AppsToday    int    `json:"apps_today"`
	AppsPerDay   int    `json:"apps_per_day"`
	CreatedAt    string `json:"created_at"`
}

// agentAppsPerDay caps how many apps one agent may create per day.
const agentAppsPerDay = 10

func agentContainer(id string) string { return "a-" + id }

func (s *Server) agentInstanceView(ctx context.Context, h *store.AgentInstance) v1AgentInstance {
	status := "missing"
	if s.Docker != nil {
		if c, err := s.Docker.Inspect(ctx, agentContainer(h.ID)); err == nil {
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
	return v1AgentInstance{ID: h.ID, Package: h.Package, Name: h.Name, ConnectionID: h.ConnectionID, Model: h.Model,
		Status: status, Telegram: len(h.TelegramCiphertext) > 0, TelegramBot: h.TelegramBot,
		WhatsApp: h.WhatsAppNumber, AppsToday: today, AppsPerDay: agentAppsPerDay,
		CreatedAt: h.CreatedAt}
}

// GET /v1/agent-catalog
func (s *Server) v1ListAgentCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"packages": agentcatalog.List()})
}

// GET /v1/agent-instances
func (s *Server) v1ListAgentInstances(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListAgentInstancesForOwner(r.Context(), tenantToken(r))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]v1AgentInstance, 0, len(rows))
	for _, h := range rows {
		out = append(out, s.agentInstanceView(r.Context(), h))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

type v1CreateAgentInstanceReq struct {
	Package              string `json:"package"`
	Name                 string `json:"name"`
	ConnectionID         string `json:"connection_id"`
	Model                string `json:"model"`
	TelegramBotToken     string `json:"telegram_bot_token"`
	TelegramAllowedUsers string `json:"telegram_allowed_users"`
}

// POST /v1/agent-instances — add an agent and start it.
func (s *Server) v1CreateAgentInstance(w http.ResponseWriter, r *http.Request) {
	if s.Docker == nil || s.Secrets == nil || s.ModelProviders == nil || s.AgentProxyURL == "" || s.AgentRoot == "" {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "agents are not available on this engine")
		return
	}
	var req v1CreateAgentInstanceReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	pkg, ok := agentcatalog.Get(req.Package)
	if !ok {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "unknown agent")
		return
	}
	model, err := s.agentModel(r.Context(), req.ConnectionID, req.Model)
	if err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	users := strings.TrimSpace(req.TelegramAllowedUsers)
	token := strings.TrimSpace(req.TelegramBotToken)
	if token != "" && !pkg.Telegram {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", pkg.Label+" cannot use Telegram yet")
		return
	}
	if token != "" && users == "" {
		writeV1Err(w, http.StatusBadRequest, "invalid_request",
			"add your Telegram user ID so only you can talk to the agent")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = pkg.Label
	}
	h := &store.AgentInstance{ID: strings.ToLower(newULID()), OwnerToken: tenantToken(r), Package: pkg.ID, Name: name,
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
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not protect the agent's keys")
		return
	}
	h.SecretHash = hashSecret("pending-" + h.ID) // replaced when the container is created
	if err := s.Store.CreateAgentInstance(r.Context(), h); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.provisionAgent(r.Context(), h, pkg); err != nil {
		s.Log.Warn("agent start failed", "agent", h.ID, "err", err.Error())
		writeJSON(w, http.StatusAccepted, map[string]any{"agent": s.agentInstanceView(r.Context(), h),
			"warning": "The agent was added but could not start: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"agent": s.agentInstanceView(r.Context(), h)})
}

// agentModel checks the connection speaks the OpenAI chat protocol (what the
// agents use) and resolves its model.
func (s *Server) agentModel(ctx context.Context, connectionID, model string) (string, error) {
	conns, err := s.ModelProviders.List(ctx)
	if err != nil {
		return "", err
	}
	for _, c := range conns {
		if c.ID != connectionID {
			continue
		}
		if c.Protocol != modelprovider.OpenAI {
			return "", errors.New("choose an OpenAI-compatible model connection for this agent")
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

// provisionAgent (re)creates the agent's container with a fresh agent
// secret: it writes the agent's config into its state directory and starts
// the pinned image under sandboxd's hardened profile.
func (s *Server) provisionAgent(ctx context.Context, h *store.AgentInstance, pkg agentcatalog.Package) error {
	secret, err := randomSecret()
	if err != nil {
		return err
	}
	apiKey, err := s.Secrets.Open(h.APIKeyCiphertext, h.APIKeyNonce)
	if err != nil {
		return errors.New("the agent's API key cannot be read")
	}
	dir := filepath.Join(s.AgentRoot, h.ID)
	uid, gid := 0, 0
	if parts := strings.SplitN(pkg.User, ":", 2); len(parts) == 2 {
		uid, _ = strconv.Atoi(parts[0])
		gid, _ = strconv.Atoi(parts[1])
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		return fmt.Errorf("prepare the agent's storage: %w", err)
	}
	s.dropBrokenChannels(ctx, h, pkg)
	cfg := agentcatalog.Config{ProxyURL: s.AgentProxyURL, AgentID: h.ID, Secret: secret, Model: h.Model}
	var telegramToken []byte
	if len(h.TelegramCiphertext) > 0 && pkg.Telegram {
		if telegramToken, err = s.Secrets.Open(h.TelegramCiphertext, h.TelegramNonce); err != nil {
			return errors.New("the Telegram token cannot be read")
		}
		cfg.TelegramUsers = splitList(h.TelegramAllowedUsers)
	}
	if pkg.WhatsApp {
		cfg.WhatsAppNumber = h.WhatsAppNumber
		cfg.WhatsApp = h.WhatsAppNumber != "" || s.linking(h.ID, "whatsapp")
	}
	config, err := pkg.Render(cfg)
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dir, pkg.ConfigFile)
	if err := os.WriteFile(cfgPath, []byte(config), 0o600); err != nil {
		return err
	}
	if err := os.Chown(cfgPath, uid, gid); err != nil {
		return err
	}
	if err := s.Store.RotateAgentInstanceSecret(ctx, h.ID, hashSecret(secret)); err != nil {
		return err
	}
	h.SecretHash = hashSecret(secret)
	env := append(append([]string{}, pkg.Env...),
		pkg.APIKeyEnv+"="+string(apiKey), agentcatalog.SecretEnv+"="+secret)
	if telegramToken != nil {
		env = append(env, "TELEGRAM_BOT_TOKEN="+string(telegramToken))
	}
	if pkg.ChannelEnv != nil {
		env = append(env, pkg.ChannelEnv(cfg)...)
	}
	name := agentContainer(h.ID)
	_ = s.Docker.Remove(ctx, name)
	spec := s.agentRunSpec(h, pkg, name, env, pkg.Cmd)
	spec.Restart = "unless-stopped" // always on: survives engine and computer restarts
	spec.Labels = []string{"sandboxd.agent=" + h.ID, "sandboxd.agent.package=" + pkg.ID}
	_, err = s.Docker.Run(ctx, spec)
	return err
}

// agentRunSpec is sandboxd's hardened profile for an agent's containers (the
// agent itself, and one-off setup steps on its state): non-root, no
// capabilities, read-only root, no privilege escalation, bounded memory.
func (s *Server) agentRunSpec(h *store.AgentInstance, pkg agentcatalog.Package, name string, env, cmd []string) docker.RunSpec {
	uid := strings.SplitN(pkg.User, ":", 2)[0]
	tmpfs := []string{"/tmp"}
	if pkg.ExecRun { // a supervisor such as s6 executes from /run
		tmpfs = append(tmpfs, "/run:exec,mode=755,uid="+uid)
	}
	return docker.RunSpec{
		Name: name, Hostname: name, Network: s.Network, Userns: s.Userns, Runtime: s.Runtime,
		ReadOnly: true, CapDrop: []string{"ALL"}, SecurityOpt: []string{"no-new-privileges"},
		Memory: pkg.Memory, MemorySwap: pkg.Memory, PidsLimit: 1024, ShmSize: pkg.ShmSize,
		Tmpfs:   tmpfs,
		User:    pkg.User,
		Env:     env,
		Volumes: []string{filepath.Join(s.AgentRoot, h.ID) + ":" + pkg.StatePath},
		Image:   pkg.Image, Cmd: cmd,
	}
}

func (s *Server) ownAgentInstance(w http.ResponseWriter, r *http.Request) (*store.AgentInstance, bool) {
	h, err := s.Store.GetAgentInstanceForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such agent")
		return nil, false
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return nil, false
	}
	return h, true
}

// GET /v1/agent-instances/{id}
func (s *Server) v1GetAgentInstance(w http.ResponseWriter, r *http.Request) {
	if h, ok := s.ownAgentInstance(w, r); ok {
		writeJSON(w, http.StatusOK, s.agentInstanceView(r.Context(), h))
	}
}

// POST /v1/agent-instances/{id}/start — starts it, re-creating the container (with a
// fresh secret) when it is missing.
func (s *Server) v1StartAgentInstance(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	var err error
	if _, ierr := s.Docker.Inspect(r.Context(), agentContainer(h.ID)); ierr != nil {
		err = s.provisionAgent(r.Context(), h, pkg)
	} else {
		err = s.Docker.Start(r.Context(), agentContainer(h.ID))
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not start the agent: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.agentInstanceView(r.Context(), h))
}

// POST /v1/agent-instances/{id}/stop
func (s *Server) v1StopAgentInstance(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	if err := s.Docker.Stop(r.Context(), agentContainer(h.ID), 20); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.agentInstanceView(r.Context(), h))
}

// DELETE /v1/agent-instances/{id} — removes the agent, its container and its memory.
func (s *Server) v1DeleteAgentInstance(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	if err := s.Docker.Remove(r.Context(), agentContainer(h.ID)); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.Store.DeleteAgentInstance(r.Context(), h.ID); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if s.AgentRoot != "" && h.ID != "" {
		_ = os.RemoveAll(filepath.Join(s.AgentRoot, h.ID))
	}
	w.WriteHeader(http.StatusNoContent)
}

type v1AgentChatReq struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// POST /v1/agent-instances/{id}/chat — one turn with the agent through its own
// OpenAI-compatible API. sandboxd holds the agent's API key.
func (s *Server) v1AgentChat(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	var req v1AgentChatReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req); err != nil || len(req.Messages) == 0 {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "send at least one message")
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	apiKey, err := s.Secrets.Open(h.APIKeyCiphertext, h.APIKeyNonce)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "the agent's API key cannot be read")
		return
	}
	body, _ := json.Marshal(map[string]any{"model": pkg.ChatModel, "messages": req.Messages, "stream": false})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute) // building an app can take a while
	defer cancel()
	up, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("http://%s:%d/v1/chat/completions", agentContainer(h.ID), pkg.APIPort), bytes.NewReader(body))
	up.Header.Set("Authorization", "Bearer "+string(apiKey))
	up.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(up)
	if err != nil {
		writeV1Err(w, http.StatusBadGateway, "unavailable", "the agent is not answering; start it and try again")
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
		writeV1Err(w, http.StatusBadGateway, "agent_error", "the agent could not answer: "+strings.TrimSpace(string(raw[:min(len(raw), 300)])))
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
