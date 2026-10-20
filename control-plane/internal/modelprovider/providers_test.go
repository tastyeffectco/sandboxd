package modelprovider

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func testService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(root, "db.sqlite"), "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cipher, err := secrets.Load("", filepath.Join(root, "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(st, cipher)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.transport.CloseIdleConnections)
	return s, st
}

func TestCredentialsAndCapabilityLifecycle(t *testing.T) {
	s, st := testService(t)
	ctx := context.Background()
	key := "a-secret-that-must-stay-on-the-control-plane"
	in := Input{Name: "Local gateway", Protocol: OpenAI, BaseURL: "http://localhost:1234/v1",
		Models: []Model{{ID: "team/model:latest"}}, APIKey: &key}
	c, err := s.Save(ctx, "", in)
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.GetModelProvider(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), key) || strings.Contains(row.ConfigJSON, key) ||
		strings.Contains(string(row.KeyCiphertext), key) || len(row.KeyNonce) == 0 {
		t.Fatal("key was not encrypted/redacted")
	}
	conn, err := s.Connection(ctx, c.ID, "", "http://proxy:9100", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Model.ID != "team/model:latest" || strings.Contains(conn.Token, key) {
		t.Fatal("incorrect task configuration")
	}
	g, err := s.verify(conn.Token)
	if err != nil || g.Model != conn.Model.ID {
		t.Fatal("capability could not be verified", err)
	}
	if _, err := s.verify(conn.Token + "tampered"); err == nil {
		t.Fatal("accepted tampered capability")
	}
	if _, err := s.Connection(ctx, c.ID, "other", "http://proxy:9100", time.Minute); err == nil {
		t.Fatal("accepted unconfigured model")
	}
	in.APIKey = nil
	c, err = s.Save(ctx, c.ID, in)
	if err != nil || !c.HasAPIKey {
		t.Fatal("update lost stored key", err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/providers/"+c.ID+"/v1/chat/completions", strings.NewReader(`{"model":"team/model:latest"}`))
	r.Header.Set("Authorization", "Bearer "+conn.Token)
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("old capability survived update: %d", w.Code)
	}
	empty := ""
	in.APIKey = &empty
	c, err = s.Save(ctx, c.ID, in)
	if err != nil || c.HasAPIKey {
		t.Fatal("could not remove key", err)
	}
	conn, err = s.Connection(ctx, c.ID, "", "http://proxy:9100", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("POST", "/providers/"+c.ID+"/v1/chat/completions", strings.NewReader(`{"model":"team/model:latest"}`))
	r.Header.Set("Authorization", "Bearer "+conn.Token)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("capability survived deletion: %d", w.Code)
	}
}

func TestProxyProtocolsAndBoundaries(t *testing.T) {
	for _, protocol := range []string{OpenAI, Anthropic} {
		t.Run(protocol, func(t *testing.T) {
			s, _ := testService(t)
			key := "upstream-only-secret"
			requests := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				wantPath := "/gateway/v1/chat/completions"
				if protocol == Anthropic {
					wantPath = "/gateway/v1/messages"
				}
				if r.URL.Path != wantPath {
					t.Errorf("path=%s", r.URL.Path)
				}
				if r.Header.Get("Cookie") != "" || r.Header.Get("X-Forwarded-Host") != "" {
					t.Error("forwarded client headers")
				}
				if protocol == OpenAI && r.Header.Get("Authorization") != "Bearer "+key {
					t.Error("missing upstream auth")
				}
				if protocol == Anthropic && (r.Header.Get("X-Api-Key") != key || r.Header.Get("Authorization") != "") {
					t.Error("bad anthropic auth")
				}
				body, _ := io.ReadAll(r.Body)
				if strings.Contains(string(body), "other-model") {
					t.Error("duplicate JSON model was forwarded")
				}
				w.Header().Set("Set-Cookie", key)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"text\":\"hello\"}\n\n")
			}))
			defer upstream.Close()
			c, err := s.Save(context.Background(), "", Input{Name: "Gateway", Protocol: protocol, BaseURL: upstream.URL + "/gateway/v1", APIKey: &key, Models: []Model{{ID: "team/model"}}})
			if err != nil {
				t.Fatal(err)
			}
			conn, err := s.Connection(context.Background(), c.ID, "", "http://proxy", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			path := "/providers/" + c.ID + "/v1/chat/completions"
			if protocol == Anthropic {
				path = "/providers/" + c.ID + "/v1/messages"
			}
			call := func(path, body, token string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", path, strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("Cookie", "private-client-cookie")
				r.Header.Set("X-Forwarded-Host", "evil.invalid")
				w := httptest.NewRecorder()
				s.ServeHTTP(w, r)
				return w
			}
			w := call(path, `{"model":"other-model","model":"team/model","messages":[],"stream":true}`, conn.Token)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "hello") || w.Header().Get("Set-Cookie") != "" {
				t.Fatalf("bad stream: %d %s", w.Code, w.Body.String())
			}
			for _, bad := range []struct{ path, body, token string }{
				{path, `{"model":"other"}`, conn.Token},
				{path, `{"model":"team/model","api_base":"http://evil.invalid"}`, conn.Token},
				{path + "?api_key=override", `{"model":"team/model"}`, conn.Token},
				{"/providers/other/v1/messages", `{"model":"team/model"}`, conn.Token},
				{path, `{"model":"team/model"}`, ""},
			} {
				if w := call(bad.path, bad.body, bad.token); w.Code < 400 {
					t.Fatalf("accepted invalid request: %+v", bad)
				}
			}
			if requests != 1 {
				t.Fatalf("invalid request reached upstream: %d", requests)
			}
		})
	}
}

func TestProxyErrorsDoNotLeakCredentialsOrRedirect(t *testing.T) {
	s, _ := testService(t)
	key := "secret-from-upstream-error"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://evil.invalid/"+key)
		w.WriteHeader(307)
		io.WriteString(w, key)
	}))
	defer upstream.Close()
	c, err := s.Save(context.Background(), "", Input{Name: "Bad gateway", Protocol: OpenAI, BaseURL: upstream.URL, APIKey: &key, Models: []Model{{ID: "model"}}})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := s.Connection(context.Background(), c.ID, "", "http://proxy", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/providers/"+c.ID+"/v1/chat/completions", strings.NewReader(`{"model":"model"}`))
	r.Header.Set("Authorization", "Bearer "+conn.Token)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 400 || strings.Contains(w.Body.String(), key) || w.Header().Get("Location") != "" {
		t.Fatal("redirect/error leaked", w)
	}
}

func TestEndpointValidation(t *testing.T) {
	for _, address := range []string{"http://169.254.169.254/v1", "http://100.100.100.200/v1", "http://[fe80::1]/v1", "http://0.0.0.0/v1", "http://user:secret@localhost/v1", "https://host/v1?key=secret", "file:///tmp/secret", "http://[::]/v1", "http://224.0.0.1/v1"} {
		in := Input{Name: "test", Protocol: OpenAI, BaseURL: address, Models: []Model{{ID: "model"}}}
		if validate(&in) == nil {
			t.Errorf("accepted %s", address)
		}
	}
	for _, address := range []string{"http://127.0.0.1:11434/v1", "http://[::1]:1234/v1", "http://192.168.1.10:8000/v1", "https://gateway.example/prefix/v1"} {
		in := Input{Name: "test", Protocol: Anthropic, BaseURL: address, Models: []Model{{ID: "model"}}}
		if err := validate(&in); err != nil {
			t.Errorf("rejected %s: %v", address, err)
		}
	}
	for _, address := range []string{"169.254.169.254:80", "[fe80::1]:80", "100.100.100.200:80"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, err := safeDial(ctx, "tcp", address)
		cancel()
		if err == nil {
			conn.Close()
			t.Errorf("dialed forbidden address %s", address)
		}
	}
	if !allowedIP(net.ParseIP("::ffff:127.0.0.1")) || allowedIP(net.ParseIP("::ffff:169.254.169.254")) {
		t.Fatal("incorrect mapped IPv4 handling")
	}
}

func TestCapabilityDeadlineCancelsUpstream(t *testing.T) {
	s, _ := testService(t)
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer upstream.Close()
	c, err := s.Save(context.Background(), "", Input{Name: "Slow local model", Protocol: OpenAI,
		BaseURL: upstream.URL + "/v1", Models: []Model{{ID: "model"}}})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := s.Connection(context.Background(), c.ID, "", "http://proxy", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/providers/"+c.ID+"/v1/chat/completions", strings.NewReader(`{"model":"model","stream":true}`))
	r.Header.Set("Authorization", "Bearer "+conn.Token)
	s.ServeHTTP(httptest.NewRecorder(), r)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("capability expiry did not cancel upstream inference")
	}
	if _, err := s.verify(conn.Token); err == nil {
		t.Fatal("expired capability remained usable")
	}
}

func TestProbeUsesSelectedModelAndRedactsUpstreamErrors(t *testing.T) {
	for _, protocol := range []string{OpenAI, Anthropic} {
		t.Run(protocol, func(t *testing.T) {
			s, _ := testService(t)
			key := "probe-secret"
			var reject atomic.Bool
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				var payload struct {
					Model  string `json:"model"`
					Stream bool   `json:"stream"`
				}
				json.NewDecoder(r.Body).Decode(&payload)
				if payload.Model != "second/model" || payload.Stream {
					t.Error("probe did not use the selected model and non-streaming mode")
				}
				if reject.Load() {
					http.Error(w, key, http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if protocol == Anthropic {
					if r.URL.Path != "/v1/messages" || r.Header.Get("X-Api-Key") != key {
						t.Error("incorrect Anthropic probe route/auth")
					}
					io.WriteString(w, `{"content":[{"type":"text","text":"OK"}]}`)
				} else {
					if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+key {
						t.Error("incorrect OpenAI probe route/auth")
					}
					io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
				}
			}))
			defer upstream.Close()
			c, err := s.Save(context.Background(), "", Input{Name: "Probe", Protocol: protocol,
				BaseURL: upstream.URL + "/v1", APIKey: &key, Models: []Model{{ID: "first/model"}, {ID: "second/model"}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Probe(context.Background(), c.ID, "second/model"); err != nil {
				t.Fatal(err)
			}
			reject.Store(true)
			if err := s.Probe(context.Background(), c.ID, "second/model"); err == nil || strings.Contains(err.Error(), key) {
				t.Fatal("probe leaked an upstream error or reported success")
			}
			if err := s.Probe(context.Background(), c.ID, "missing"); err == nil {
				t.Fatal("probe accepted an unknown model")
			}
			if requests.Load() != 2 {
				t.Fatalf("unexpected inference requests: %d", requests.Load())
			}
		})
	}
}
