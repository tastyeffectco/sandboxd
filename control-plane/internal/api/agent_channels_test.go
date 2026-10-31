package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

const testBotToken = "123456789:ABCdefGhIJKlmNoPQRsTUVwxYZ0123456789"

func channelTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	// A file, not :memory: — the linking goroutine and the test use separate
	// pool connections, and each in-memory connection is its own database.
	st, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "db.sqlite")+"?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cipher, err := secrets.Load("", filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: st, Secrets: cipher, AgentRoot: t.TempDir(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := st.CreateAgentInstance(context.Background(), &store.AgentInstance{ID: "a1", OwnerToken: "alice", Package: "openclaw",
		Name: "Robin", ConnectionID: "c1", Model: "m1", SecretHash: "h", CreatedAt: "now"}); err != nil {
		t.Fatal(err)
	}
	return s, st
}

func callAs(owner string, handler http.HandlerFunc, method, body string) (int, map[string]any) {
	r := httptest.NewRequest(method, "/v1/agent-instances/a1/channels", strings.NewReader(body))
	r = r.WithContext(auth.WithActor(r.Context(), auth.Actor{Name: owner, Kind: "service"}))
	r.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	handler(w, r)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// fakeTelegram answers getMe, then delivers one private message from user 42.
type fakeTelegram struct {
	mu        sync.Mutex
	delivered bool
	sent      []string
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.URL.Path, "/bot"+testBotToken+"/") {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"ok":false,"description":"Unauthorized"}`)
		return
	}
	switch filepath.Base(r.URL.Path) {
	case "getMe":
		_, _ = io.WriteString(w, `{"ok":true,"result":{"id":1,"is_bot":true,"username":"robin_helper_bot"}}`)
	case "getUpdates":
		if f.delivered || r.Form.Get("timeout") == "0" {
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
			return
		}
		f.delivered = true
		_, _ = io.WriteString(w, `{"ok":true,"result":[
			{"update_id":7,"message":{"from":{"id":99,"is_bot":false},"chat":{"id":-5,"type":"group"}}},
			{"update_id":8,"message":{"from":{"id":42,"is_bot":false,"first_name":"Sam"},"chat":{"id":42,"type":"private"}}}]}`)
	case "sendMessage":
		f.sent = append(f.sent, r.Form.Get("chat_id")+": "+r.Form.Get("text"))
		_, _ = io.WriteString(w, `{"ok":true,"result":{}}`)
	default:
		_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
	}
}

func TestTelegramLinkLearnsTheOwnerFromTheirFirstMessage(t *testing.T) {
	s, st := channelTestServer(t)
	tg := &fakeTelegram{}
	srv := httptest.NewServer(tg)
	defer srv.Close()
	s.TelegramAPI = srv.URL

	if code, out := callAs("alice", s.v1LinkTelegram, "POST", `{"bot_token":"not a token"}`); code != http.StatusBadRequest {
		t.Fatalf("a malformed token: HTTP %d %v", code, out)
	}
	if code, _ := callAs("alice", s.v1LinkTelegram, "POST", `{"bot_token":"123456789:ZZZdefGhIJKlmNoPQRsTUVwxYZ0123456789"}`); code != http.StatusBadRequest {
		t.Fatalf("a token Telegram rejects: HTTP %d", code)
	}
	if code, _ := callAs("mallory", s.v1LinkTelegram, "POST", `{"bot_token":"`+testBotToken+`"}`); code != http.StatusNotFound {
		t.Fatalf("someone else's agent: HTTP %d", code)
	}
	code, out := callAs("alice", s.v1LinkTelegram, "POST", `{"bot_token":"`+testBotToken+`"}`)
	tgView, _ := out["telegram"].(map[string]any)
	if code != http.StatusAccepted || tgView["status"] != "waiting" || tgView["bot"] != "robin_helper_bot" {
		t.Fatalf("link: HTTP %d %v", code, out)
	}

	var h *store.AgentInstance
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		h, _ = st.GetAgentInstanceForOwner(context.Background(), "a1", "alice")
		tg.mu.Lock()
		told := len(tg.sent) > 0
		tg.mu.Unlock()
		if h.TelegramBot != "" && told {
			break
		}
	}
	if h.TelegramBot != "robin_helper_bot" || h.TelegramAllowedUsers != "42" || len(h.TelegramCiphertext) == 0 {
		t.Fatalf("saved: bot=%q users=%q token=%d bytes", h.TelegramBot, h.TelegramAllowedUsers, len(h.TelegramCiphertext))
	}
	if token, _ := s.Secrets.Open(h.TelegramCiphertext, h.TelegramNonce); string(token) != testBotToken {
		t.Error("the bot token was not saved encrypted")
	}
	tg.mu.Lock()
	sent := strings.Join(tg.sent, "\n")
	tg.mu.Unlock()
	if !strings.Contains(sent, "42: Connected! I'm Robin.") {
		t.Errorf("the owner was not told it worked: %q", sent)
	}
	_, out = callAs("alice", s.v1AgentChannels, "GET", "")
	if v, _ := out["telegram"].(map[string]any); v["status"] != "linked" || v["bot"] != "robin_helper_bot" {
		t.Errorf("channels after linking: %v", out)
	}
	if code, _ := callAs("alice", s.v1LinkTelegram, "POST", `{"bot_token":"`+testBotToken+`"}`); code != http.StatusConflict {
		t.Errorf("linking twice: HTTP %d; want 409", code)
	}
}

func TestChannelsStartOff(t *testing.T) {
	s, _ := channelTestServer(t)
	_, out := callAs("alice", s.v1AgentChannels, "GET", "")
	for _, ch := range []string{"whatsapp", "telegram"} {
		if v, _ := out[ch].(map[string]any); v["status"] != "off" {
			t.Errorf("%s: %v", ch, out[ch])
		}
	}
}

func TestWhatsAppNumberFromTheSavedLogin(t *testing.T) {
	dir := t.TempDir()
	for creds, want := range map[string]string{
		`{"me":{"id":"33612345678:12@s.whatsapp.net","name":"Sam"}}`: "33612345678",
		`{"me":{"id":"15551234567@s.whatsapp.net"}}`:                 "15551234567",
		`{"me":{"id":"abc@lid"}}`:                                    "",
		`{"noise":true}`:                                             "",
	} {
		p := filepath.Join(dir, "creds.json")
		if err := os.WriteFile(p, []byte(creds), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := whatsAppNumber(p)
		if got != want || (want == "") != (err != nil) {
			t.Errorf("%s: %q, %v; want %q", creds, got, err, want)
		}
	}
	if _, err := whatsAppNumber(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("a missing login was accepted")
	}
}

func TestHermesPairingEvents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "link.log")
	write := func(s string) {
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"ts":1,"event":"started"}` + "\n" + `{"ts":2,"event":"qr","qr":"first"}` + "\n" + `{"ts":3,"event":"qr","qr":"second"}` + "\n" + `not json` + "\n")
	if ev := lastPairEvents(p); ev.qr != "second" || ev.connected || ev.err != "" {
		t.Errorf("waiting for a scan: %+v", ev)
	}
	write(`{"event":"qr","qr":"a"}` + "\n" + `{"event":"disconnected","reason":515}` + "\n" + `{"event":"connected","user":"x"}` + "\n")
	if ev := lastPairEvents(p); !ev.connected {
		t.Errorf("after the scan: %+v", ev)
	}
	write(`{"event":"error","error":"logged_out"}` + "\n")
	if ev := lastPairEvents(p); ev.err != "logged_out" {
		t.Errorf("a refused link: %+v", ev)
	}
}

func TestBrokenChannelsAreTurnedOffBeforeTheAgentStarts(t *testing.T) {
	s, st := channelTestServer(t)
	tg := &fakeTelegram{}
	srv := httptest.NewServer(tg)
	s.TelegramAPI = srv.URL
	ctx := context.Background()
	pkg := agentcatalog.OpenClaw
	link := func(token string) *store.AgentInstance {
		ct, nonce, _ := s.Secrets.Seal([]byte(token))
		_ = st.SetAgentInstanceTelegram(ctx, "a1", ct, nonce, "robin_bot", "42")
		_ = st.SetAgentInstanceWhatsApp(ctx, "a1", "33612345678")
		h, _ := st.GetAgentInstanceForOwner(ctx, "a1", "alice")
		return h
	}

	// Both work: the WhatsApp login is on disk and Telegram knows the token.
	creds := filepath.Join(s.AgentRoot, "a1", filepath.FromSlash(pkg.WhatsAppCreds))
	_ = os.MkdirAll(filepath.Dir(creds), 0o700)
	_ = os.WriteFile(creds, []byte(`{"me":{"id":"33612345678:1@s.whatsapp.net"}}`), 0o600)
	h := link(testBotToken)
	s.dropBrokenChannels(ctx, h, pkg)
	if h.WhatsAppNumber == "" || len(h.TelegramCiphertext) == 0 {
		t.Fatal("working channels were turned off")
	}

	// Telegram cannot be reached (offline): keep it, it is not the token's fault.
	srv.Close()
	s.dropBrokenChannels(ctx, h, pkg)
	if len(h.TelegramCiphertext) == 0 {
		t.Error("an unreachable Telegram turned the channel off")
	}

	// The login is gone and the token was revoked: both are turned off, saved.
	srv = httptest.NewServer(tg)
	defer srv.Close()
	s.TelegramAPI = srv.URL
	_ = os.Remove(creds)
	h = link("123456789:REVOKEDrevokedREVOKEDrevokedREVOKED")
	s.dropBrokenChannels(ctx, h, pkg)
	saved, _ := st.GetAgentInstanceForOwner(ctx, "a1", "alice")
	if h.WhatsAppNumber != "" || saved.WhatsAppNumber != "" {
		t.Error("WhatsApp stayed on without its login")
	}
	if len(h.TelegramCiphertext) != 0 || len(saved.TelegramCiphertext) != 0 || saved.TelegramBot != "" {
		t.Error("Telegram stayed on with a revoked token")
	}
}
