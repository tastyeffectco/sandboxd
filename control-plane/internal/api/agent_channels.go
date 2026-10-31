package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// Channels are where an agent talks to its owner: WhatsApp (linked by
// scanning a QR code, answering only the owner's own number) and Telegram
// (the owner's bot, answering only the owner). Linking runs in the
// background; the console polls GET .../channels and shows each step.

// channelLink is one channel's state as the console shows it.
type channelLink struct {
	// Status: off | preparing | scan (show the QR code) | waiting (send the
	// bot a message) | connecting | linked | failed.
	Status  string `json:"status"`
	QR      string `json:"qr,omitempty"`       // text to draw as a QR code
	QRImage string `json:"qr_image,omitempty"` // or a ready QR image (data URL)
	Bot     string `json:"bot,omitempty"`      // the Telegram bot's username
	Number  string `json:"number,omitempty"`   // the linked WhatsApp number
	Message string `json:"message,omitempty"`

	cancel context.CancelFunc
}

// channelLinkTimeout bounds one linking attempt: getting the channel ready
// (OpenClaw installs its WhatsApp plugin and restarts, a few minutes on a slow
// computer) and then the scan or first message.
const channelLinkTimeout = 10 * time.Minute

func linkKey(agentID, channel string) string { return agentID + "/" + channel }

// linking reports whether a channel is being linked for the agent.
func (s *Server) linking(agentID, channel string) bool {
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	l := s.channelLinks[linkKey(agentID, channel)]
	return l != nil && l.cancel != nil
}

// setLink updates a link in progress (under the lock) for the console to read.
func (s *Server) setLink(agentID, channel string, update func(*channelLink)) {
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	if l := s.channelLinks[linkKey(agentID, channel)]; l != nil {
		update(l)
	}
}

// startLink registers a new linking attempt, cancelling a previous one.
func (s *Server) startLink(agentID, channel string, first channelLink) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), channelLinkTimeout)
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	if s.channelLinks == nil {
		s.channelLinks = map[string]*channelLink{}
	}
	if old := s.channelLinks[linkKey(agentID, channel)]; old != nil && old.cancel != nil {
		old.cancel()
	}
	first.cancel = cancel
	s.channelLinks[linkKey(agentID, channel)] = &first
	return ctx
}

// endLink finishes a linking attempt: the final state stays visible until the
// next attempt (or is dropped when the channel is linked, as the store says so).
func (s *Server) endLink(agentID, channel string, final channelLink) {
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	if l := s.channelLinks[linkKey(agentID, channel)]; l != nil && l.cancel != nil {
		l.cancel()
	}
	if final.Status == "linked" {
		delete(s.channelLinks, linkKey(agentID, channel))
		return
	}
	s.channelLinks[linkKey(agentID, channel)] = &final
}

func (s *Server) channelView(h *store.AgentInstance, pkg agentcatalog.Package) map[string]any {
	s.channelMu.Lock()
	defer s.channelMu.Unlock()
	view := func(channel string, supported bool, linked channelLink) any {
		if !supported {
			return nil
		}
		if l := s.channelLinks[linkKey(h.ID, channel)]; l != nil && (l.cancel != nil || linked.Status == "off") {
			return *l
		}
		return linked
	}
	wa, tg := channelLink{Status: "off"}, channelLink{Status: "off"}
	if h.WhatsAppNumber != "" {
		wa = channelLink{Status: "linked", Number: h.WhatsAppNumber}
	}
	if len(h.TelegramCiphertext) > 0 {
		tg = channelLink{Status: "linked", Bot: h.TelegramBot}
	}
	return map[string]any{"whatsapp": view("whatsapp", pkg.WhatsApp, wa), "telegram": view("telegram", pkg.Telegram, tg)}
}

// GET /v1/agent-instances/{id}/channels
func (s *Server) v1AgentChannels(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	writeJSON(w, http.StatusOK, s.channelView(h, pkg))
}

// reprovisionAgent re-creates the agent's container from its latest saved
// settings (channels included). One at a time: linking runs in the background.
func (s *Server) reprovisionAgent(ctx context.Context, id, owner string) error {
	if s.Docker == nil {
		return errors.New("agents are not available on this engine")
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	h, err := s.Store.GetAgentInstanceForOwner(ctx, id, owner)
	if err != nil {
		return err
	}
	pkg, _ := agentcatalog.Get(h.Package)
	return s.provisionAgent(ctx, h, pkg)
}

// dropBrokenChannels turns off, before the agent starts, a channel that can
// no longer connect: a WhatsApp login that is gone, or a bot token Telegram
// rejects (revoked). Hermes stops altogether when a channel fails to connect;
// this way the agent keeps working and the owner sees the channel as not
// connected, ready to link again. A Telegram that cannot be reached (offline)
// is kept: that is not the token's fault.
func (s *Server) dropBrokenChannels(ctx context.Context, h *store.AgentInstance, pkg agentcatalog.Package) {
	if h.WhatsAppNumber != "" && pkg.WhatsAppCreds != "" {
		creds := filepath.Join(s.AgentRoot, h.ID, filepath.FromSlash(pkg.WhatsAppCreds))
		if _, err := os.Stat(creds); errors.Is(err, os.ErrNotExist) {
			s.Log.Warn("the agent's WhatsApp login is gone; unlinking WhatsApp", "agent", h.ID)
			if s.Store.SetAgentInstanceWhatsApp(ctx, h.ID, "") == nil {
				h.WhatsAppNumber = ""
			}
		}
	}
	if len(h.TelegramCiphertext) > 0 && s.Secrets != nil {
		token, err := s.Secrets.Open(h.TelegramCiphertext, h.TelegramNonce)
		if err != nil {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := s.telegramCall(cctx, string(token), "getMe", nil, nil); errors.Is(err, errTelegramToken) {
			s.Log.Warn("Telegram rejects the agent's bot token; disconnecting Telegram", "agent", h.ID)
			if s.Store.SetAgentInstanceTelegram(ctx, h.ID, nil, nil, "", "") == nil {
				h.TelegramCiphertext, h.TelegramNonce, h.TelegramBot, h.TelegramAllowedUsers = nil, nil, "", ""
			}
		}
	}
}

// --- WhatsApp ---

// POST /v1/agent-instances/{id}/channels/whatsapp — start linking: the
// console shows the QR code to scan in WhatsApp → Linked devices.
func (s *Server) v1LinkWhatsApp(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	switch {
	case !pkg.WhatsApp:
		writeV1Err(w, http.StatusBadRequest, "invalid_request", pkg.Label+" cannot use WhatsApp yet")
		return
	case h.WhatsAppNumber != "":
		writeV1Err(w, http.StatusConflict, "conflict", "WhatsApp is already linked. Unlink it first to use another number.")
		return
	}
	if s.linking(h.ID, "whatsapp") { // a second click follows the attempt under way
		writeJSON(w, http.StatusAccepted, s.channelView(h, pkg))
		return
	}
	if c, err := s.Docker.Inspect(r.Context(), agentContainer(h.ID)); err != nil || !c.State.Running {
		writeV1Err(w, http.StatusConflict, "conflict", "Start the agent first, then link WhatsApp.")
		return
	}
	ctx := s.startLink(h.ID, "whatsapp", channelLink{Status: "preparing",
		Message: "Getting WhatsApp ready. The first time takes a minute or two."})
	go s.linkWhatsApp(ctx, h, pkg)
	writeJSON(w, http.StatusAccepted, s.channelView(h, pkg))
}

func (s *Server) linkWhatsApp(ctx context.Context, h *store.AgentInstance, pkg agentcatalog.Package) {
	dir := filepath.Join(s.AgentRoot, h.ID)
	credsPath := filepath.Join(dir, filepath.FromSlash(pkg.WhatsAppCreds))
	_ = os.RemoveAll(filepath.Dir(credsPath)) // a half-finished earlier login
	var err error
	switch pkg.ID {
	case agentcatalog.Hermes.ID:
		err = s.pairHermesWhatsApp(ctx, h, dir)
	case agentcatalog.OpenClaw.ID:
		err = s.pairOpenClawWhatsApp(ctx, h)
	default:
		err = errors.New(pkg.Label + " cannot use WhatsApp yet")
	}
	if err == nil {
		var number string
		if number, err = whatsAppNumber(credsPath); err == nil {
			s.setLink(h.ID, "whatsapp", func(l *channelLink) {
				*l = channelLink{Status: "connecting", Number: number, cancel: l.cancel,
					Message: "Linked. Turning WhatsApp on for your agent…"}
			})
			if err = s.Store.SetAgentInstanceWhatsApp(context.Background(), h.ID, number); err == nil {
				s.endLink(h.ID, "whatsapp", channelLink{Status: "linked", Number: number})
				err = s.reprovisionAgent(context.Background(), h.ID, h.OwnerToken)
				if err != nil {
					s.Log.Warn("agent restart after WhatsApp link failed", "agent", h.ID, "err", err.Error())
				}
				return
			}
		}
	}
	msg := err.Error()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		msg = "The QR code was not scanned in time. Try again when your phone is at hand."
	} else if errors.Is(ctx.Err(), context.Canceled) {
		return // cancelled by the owner, or replaced by a new attempt
	}
	s.Log.Warn("WhatsApp link failed", "agent", h.ID, "err", err.Error())
	s.endLink(h.ID, "whatsapp", channelLink{Status: "failed", Message: msg})
	if pkg.ID == agentcatalog.OpenClaw.ID { // unload the plugin loaded for linking
		_ = s.reprovisionAgent(context.Background(), h.ID, h.OwnerToken)
	}
}

// pairHermesWhatsApp runs Hermes's own WhatsApp bridge in pairing mode inside
// the agent's container. It reports each QR code as a JSON line, written to a
// file in the agent's state directory that sandboxd reads from the host.
func (s *Server) pairHermesWhatsApp(ctx context.Context, h *store.AgentInstance, dir string) error {
	const logName = ".sandboxd-whatsapp-link.log"
	logPath := filepath.Join(dir, logName)
	_ = os.Remove(logPath)
	script := `set -e
cd "$(python3 -c 'from gateway.platforms.whatsapp_common import resolve_whatsapp_bridge_dir as r; print(r())')"
[ -d node_modules ] || npm install --silent --no-audit --no-fund >/dev/null 2>&1
WHATSAPP_MODE=self-chat WHATSAPP_DM_POLICY=pairing exec timeout ` + strconv.Itoa(int(channelLinkTimeout.Seconds())) +
		` node bridge.js --pair-only --pair-json --session ` + agentcatalog.Hermes.StatePath + `/platforms/whatsapp/session > ` +
		agentcatalog.Hermes.StatePath + "/" + logName + ` 2>&1`
	done := make(chan error, 1)
	go func() {
		res, err := s.Docker.Exec(ctx, agentContainer(h.ID), []string{"sh", "-c", script})
		if err == nil && res.ExitCode != 0 {
			err = fmt.Errorf("WhatsApp setup stopped (exit %d)", res.ExitCode)
		}
		done <- err
	}()
	defer func() { // stop the bridge when the owner cancels or time runs out
		if ctx.Err() != nil {
			_, _ = s.Docker.Exec(context.Background(), agentContainer(h.ID), []string{"pkill", "-f", "bridge.js --pair-only"})
		}
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			if ev := lastPairEvents(logPath); ev.connected {
				return nil
			} else if ev.err != "" {
				return errors.New("WhatsApp did not accept the link: " + ev.err)
			}
			if err == nil {
				err = errors.New("WhatsApp setup ended before the QR code was scanned")
			}
			return err
		case <-tick.C:
			ev := lastPairEvents(logPath)
			switch {
			case ev.connected:
				// The bridge saves the login and exits; done follows.
				s.setLink(h.ID, "whatsapp", func(l *channelLink) {
					l.Status, l.QR, l.Message = "connecting", "", "Scanned. Finishing the link…"
				})
			case ev.err != "":
				return errors.New("WhatsApp did not accept the link: " + ev.err)
			case ev.qr != "":
				s.setLink(h.ID, "whatsapp", func(l *channelLink) {
					l.Status, l.QR, l.Message = "scan", ev.qr, ""
				})
			}
		}
	}
}

type pairEvents struct {
	qr, err   string
	connected bool
}

// lastPairEvents reads the Hermes bridge's JSON lines: the latest QR code,
// whether the phone connected, and the first error.
func lastPairEvents(path string) pairEvents {
	var out pairEvents
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		var ev struct{ Event, QR, Error string }
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "qr":
			out.qr = ev.QR
		case "connected":
			out.connected = true
		case "error":
			if out.err == "" {
				out.err = ev.Error
			}
		}
	}
	return out
}

// pairOpenClawWhatsApp installs OpenClaw's WhatsApp plugin into the agent's
// state (first time only), restarts the agent with it loaded, then asks the
// gateway for QR codes until the phone links.
func (s *Server) pairOpenClawWhatsApp(ctx context.Context, h *store.AgentInstance) error {
	name := agentContainer(h.ID)
	if matches, _ := filepath.Glob(filepath.Join(s.AgentRoot, h.ID, "npm", "projects", "openclaw-whatsapp-*")); len(matches) == 0 {
		if err := s.installOpenClawWhatsApp(ctx, h); err != nil {
			return err
		}
	}
	s.setLink(h.ID, "whatsapp", func(l *channelLink) {
		l.Message = "Starting your agent with WhatsApp. This takes a minute or two."
	})
	if err := s.reprovisionAgent(ctx, h.ID, h.OwnerToken); err != nil { // now with the plugin loaded
		return err
	}
	if err := s.waitAgentGateway(ctx, name, 18789); err != nil {
		return err
	}
	call := func(method string, params map[string]any, timeout time.Duration) (map[string]any, error) {
		p, _ := json.Marshal(params)
		res, err := s.Docker.Exec(ctx, name, []string{"node", "/app/openclaw.mjs", "gateway", "call", method, "--json",
			"--params", string(p), "--timeout", strconv.Itoa(int(timeout.Milliseconds()))})
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		if i := strings.Index(res.Stdout, "{"); i >= 0 {
			_ = json.Unmarshal([]byte(res.Stdout[i:]), &out)
		}
		if e, ok := out["error"].(map[string]any); ok {
			return nil, fmt.Errorf("%v", e["message"])
		}
		return out, nil
	}
	qr := ""
	for ctx.Err() == nil {
		if qr == "" {
			out, err := call("web.login.start", map[string]any{"channel": "whatsapp"}, time.Minute)
			if err != nil {
				return err
			}
			if qr, _ = out["qrDataUrl"].(string); qr == "" {
				return fmt.Errorf("WhatsApp did not send a QR code: %v", out["message"])
			}
			s.setLink(h.ID, "whatsapp", func(l *channelLink) { l.Status, l.QRImage, l.Message = "scan", qr, "" })
		}
		out, err := call("web.login.wait", map[string]any{"channel": "whatsapp", "timeoutMs": 20000, "currentQrDataUrl": qr},
			40*time.Second)
		if err != nil {
			return err
		}
		if connected, _ := out["connected"].(bool); connected {
			return nil
		}
		if next, _ := out["qrDataUrl"].(string); next != "" { // a fresh code replaces the expired one
			qr = next
			s.setLink(h.ID, "whatsapp", func(l *channelLink) { l.QRImage = qr })
			continue
		}
		if msg, _ := out["message"].(string); !strings.Contains(msg, "Still waiting") {
			qr = "" // the login ended (e.g. expired): ask for a new code
		}
	}
	return ctx.Err()
}

// installOpenClawWhatsApp installs OpenClaw's WhatsApp plugin into the
// agent's state. The running agent cannot: its config is read-only to it. So
// the agent stops, and a one-off container from the same image, under the
// same profile, installs the plugin (which records itself in the state).
func (s *Server) installOpenClawWhatsApp(ctx context.Context, h *store.AgentInstance) error {
	pkg := agentcatalog.OpenClaw
	const logName = ".sandboxd-whatsapp-install.log"
	s.setLink(h.ID, "whatsapp", func(l *channelLink) { l.Message = "Installing WhatsApp for your agent. This takes a minute." })
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	if err := s.Docker.Stop(ctx, agentContainer(h.ID), 20); err != nil {
		return err
	}
	var env []string
	for _, e := range pkg.Env {
		if !strings.HasPrefix(e, "OPENCLAW_CONFIG_READONLY=") {
			env = append(env, e)
		}
	}
	name := agentContainer(h.ID) + "-setup"
	_ = s.Docker.Remove(ctx, name)
	defer func() { _ = s.Docker.Remove(context.Background(), name) }()
	spec := s.agentRunSpec(h, pkg, name, env, []string{"sh", "-c",
		"cd /app && exec node openclaw.mjs plugins install @openclaw/whatsapp --accept-capabilities" +
			" --acknowledge-install-policy-warning --no-enable > " + pkg.StatePath + "/" + logName + " 2>&1"})
	spec.Labels = []string{"sandboxd.agent.setup=" + h.ID}
	if _, err := s.Docker.Run(ctx, spec); err != nil {
		return err
	}
	for {
		c, err := s.Docker.Inspect(ctx, name)
		if err != nil {
			return err
		}
		if !c.State.Running {
			if c.State.ExitCode == 0 {
				return nil
			}
			out, _ := os.ReadFile(filepath.Join(s.AgentRoot, h.ID, logName))
			s.Log.Warn("OpenClaw WhatsApp plugin install failed", "agent", h.ID, "exit", c.State.ExitCode, "out", tail(string(out), 500))
			return errors.New("WhatsApp could not be installed for your agent. Check your internet connection and try again.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// waitAgentGateway waits until the agent's gateway accepts connections.
func (s *Server) waitAgentGateway(ctx context.Context, name string, port int) error {
	probe := `require("net").connect(` + strconv.Itoa(port) + `,"127.0.0.1").on("connect",()=>process.exit(0)).on("error",()=>process.exit(1))`
	for ctx.Err() == nil {
		if res, err := s.Docker.Exec(ctx, name, []string{"node", "-e", probe}); err == nil && res.ExitCode == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
	return ctx.Err()
}

// whatsAppNumber reads the linked number from a WhatsApp login (Baileys'
// creds.json: me.id is "<number>:<device>@s.whatsapp.net").
func whatsAppNumber(credsPath string) (string, error) {
	b, err := os.ReadFile(credsPath)
	if err != nil {
		return "", errors.New("the WhatsApp link was not saved; try again")
	}
	var creds struct {
		Me struct{ ID string } `json:"me"`
	}
	if err := json.Unmarshal(b, &creds); err != nil || creds.Me.ID == "" {
		return "", errors.New("the WhatsApp link was not saved; try again")
	}
	number := creds.Me.ID
	if i := strings.IndexAny(number, ":@"); i >= 0 {
		number = number[:i]
	}
	if !regexp.MustCompile(`^\d{6,15}$`).MatchString(number) {
		return "", errors.New("the linked WhatsApp account has no phone number")
	}
	return number, nil
}

// DELETE /v1/agent-instances/{id}/channels/whatsapp — cancel linking, or
// unlink: the agent forgets the WhatsApp login and stops answering there.
func (s *Server) v1UnlinkWhatsApp(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	wasLinking := s.linking(h.ID, "whatsapp")
	s.channelMu.Lock()
	if l := s.channelLinks[linkKey(h.ID, "whatsapp")]; l != nil && l.cancel != nil {
		l.cancel()
	}
	delete(s.channelLinks, linkKey(h.ID, "whatsapp"))
	s.channelMu.Unlock()
	if h.WhatsAppNumber == "" && !wasLinking {
		writeJSON(w, http.StatusOK, s.channelView(h, pkg))
		return
	}
	if pkg.ID == agentcatalog.OpenClaw.ID && h.WhatsAppNumber != "" { // also unlinks the phone's "Linked devices" entry
		_, _ = s.Docker.Exec(r.Context(), agentContainer(h.ID), []string{"node", "/app/openclaw.mjs", "gateway", "call",
			"channels.logout", "--json", "--params", `{"channel":"whatsapp"}`})
	}
	if pkg.WhatsAppCreds != "" {
		_ = os.RemoveAll(filepath.Dir(filepath.Join(s.AgentRoot, h.ID, filepath.FromSlash(pkg.WhatsAppCreds))))
	}
	if err := s.Store.SetAgentInstanceWhatsApp(r.Context(), h.ID, ""); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	h.WhatsAppNumber = ""
	if err := s.reprovisionAgent(r.Context(), h.ID, h.OwnerToken); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not restart the agent: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.channelView(h, pkg))
}

// --- Telegram ---

var telegramTokenRE = regexp.MustCompile(`^\d{5,15}:[A-Za-z0-9_-]{30,64}$`)

// errTelegramToken: Telegram does not know the bot token (wrong, or revoked).
var errTelegramToken = errors.New("Telegram does not know this bot token. Copy it again from @BotFather.")

func (s *Server) telegramAPI() string {
	if s.TelegramAPI != "" {
		return strings.TrimRight(s.TelegramAPI, "/")
	}
	return "https://api.telegram.org"
}

// telegramCall calls the Bot API and decodes its result.
func (s *Server) telegramCall(ctx context.Context, token, method string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.telegramAPI()+"/bot"+token+"/"+method,
		strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("Telegram could not be reached. Check your internet connection.")
	}
	defer resp.Body.Close()
	var body struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return err
	}
	if !body.OK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNotFound {
			return errTelegramToken
		}
		return errors.New("Telegram said: " + body.Description)
	}
	if out != nil {
		return json.Unmarshal(body.Result, out)
	}
	return nil
}

// POST /v1/agent-instances/{id}/channels/telegram {bot_token} — checks the
// token, then waits for the owner's first message to the bot: that tells
// sandboxd who the owner is, so the agent answers only them.
func (s *Server) v1LinkTelegram(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	var req struct {
		BotToken string `json:"bot_token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}
	token := strings.TrimSpace(req.BotToken)
	switch {
	case !pkg.Telegram:
		writeV1Err(w, http.StatusBadRequest, "invalid_request", pkg.Label+" cannot use Telegram yet")
		return
	case len(h.TelegramCiphertext) > 0:
		writeV1Err(w, http.StatusConflict, "conflict", "Telegram is already connected. Disconnect it first to use another bot.")
		return
	case !telegramTokenRE.MatchString(token):
		writeV1Err(w, http.StatusBadRequest, "invalid_request",
			"That does not look like a bot token. It looks like 123456789:ABC… and comes from @BotFather.")
		return
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := s.telegramCall(r.Context(), token, "getMe", nil, &me); err != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	ctx := s.startLink(h.ID, "telegram", channelLink{Status: "waiting", Bot: me.Username,
		Message: "Open @" + me.Username + " in Telegram and send it any message, so it knows it's you."})
	go s.linkTelegram(ctx, h, token, me.Username)
	writeJSON(w, http.StatusAccepted, s.channelView(h, pkg))
}

func (s *Server) linkTelegram(ctx context.Context, h *store.AgentInstance, token, bot string) {
	fail := func(msg string) {
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		s.endLink(h.ID, "telegram", channelLink{Status: "failed", Bot: bot, Message: msg})
	}
	// A bot that delivers to a webhook cannot be read here; the agent polls too.
	_ = s.telegramCall(ctx, token, "deleteWebhook", nil, nil)
	offset := 0
	for ctx.Err() == nil {
		var updates []struct {
			UpdateID int `json:"update_id"`
			Message  *struct {
				From struct {
					ID    int64  `json:"id"`
					IsBot bool   `json:"is_bot"`
					Name  string `json:"first_name"`
				} `json:"from"`
				Chat struct {
					ID   int64  `json:"id"`
					Type string `json:"type"`
				} `json:"chat"`
			} `json:"message"`
		}
		err := s.telegramCall(ctx, token, "getUpdates",
			url.Values{"offset": {strconv.Itoa(offset)}, "timeout": {"20"}, "allowed_updates": {`["message"]`}}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			fail(err.Error())
			return
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			m := u.Message
			if m == nil || m.Chat.Type != "private" || m.From.IsBot {
				continue
			}
			// Acknowledge the message so the agent does not answer it twice.
			_ = s.telegramCall(context.Background(), token, "getUpdates", url.Values{"offset": {strconv.Itoa(offset)}, "timeout": {"0"}}, nil)
			user := strconv.FormatInt(m.From.ID, 10)
			ct, nonce, err := s.Secrets.Seal([]byte(token))
			if err == nil {
				err = s.Store.SetAgentInstanceTelegram(context.Background(), h.ID, ct, nonce, bot, user)
			}
			if err != nil {
				fail("could not save the bot: " + err.Error())
				return
			}
			s.endLink(h.ID, "telegram", channelLink{Status: "linked", Bot: bot})
			_ = s.telegramCall(context.Background(), token, "sendMessage", url.Values{"chat_id": {strconv.FormatInt(m.Chat.ID, 10)},
				"text": {"Connected! I'm " + h.Name + ". Give me a minute to start, then ask me anything."}}, nil)
			if err := s.reprovisionAgent(context.Background(), h.ID, h.OwnerToken); err != nil {
				s.Log.Warn("agent restart after Telegram link failed", "agent", h.ID, "err", err.Error())
			}
			return
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		fail("No message reached @" + bot + " in time. Send it a message, then try again.")
	}
}

// DELETE /v1/agent-instances/{id}/channels/telegram — cancel linking, or
// disconnect the bot.
func (s *Server) v1UnlinkTelegram(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	pkg, _ := agentcatalog.Get(h.Package)
	s.channelMu.Lock()
	if l := s.channelLinks[linkKey(h.ID, "telegram")]; l != nil && l.cancel != nil {
		l.cancel()
	}
	delete(s.channelLinks, linkKey(h.ID, "telegram"))
	s.channelMu.Unlock()
	if len(h.TelegramCiphertext) == 0 {
		writeJSON(w, http.StatusOK, s.channelView(h, pkg))
		return
	}
	if err := s.Store.SetAgentInstanceTelegram(r.Context(), h.ID, nil, nil, "", ""); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	h.TelegramCiphertext, h.TelegramBot = nil, ""
	if err := s.reprovisionAgent(r.Context(), h.ID, h.OwnerToken); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "could not restart the agent: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.channelView(h, pkg))
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
