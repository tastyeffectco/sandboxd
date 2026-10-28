package agentauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testJWT(exp int64) string {
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp))) + ".test"
}

func TestCodexRefreshSerializesAndPreservesMetadata(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["refresh_token"] != "refresh-old" {
			t.Error("wrong refresh request")
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": testJWT(time.Now().Add(time.Hour).Unix())})
	}))
	defer server.Close()
	old := codexTokenURL
	codexTokenURL = server.URL
	defer func() { codexTokenURL = old }()
	st := NewStore(t.TempDir())
	raw := []byte(fmt.Sprintf(`{"custom":"preserved","tokens":{"access_token":%q,"refresh_token":"refresh-old","account_id":"account","id_token":"id"}}`, testJWT(1)))
	if err := st.ImportCredential("codex", ".codex/auth.json", raw); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := st.RefreshCodex(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("refresh calls=%d", calls.Load())
	}
	result, _ := os.ReadFile(filepath.Join(st.Dir("codex"), ".codex/auth.json"))
	for _, value := range []string{"preserved", "refresh-old", "account", "id_token"} {
		if !strings.Contains(string(result), value) {
			t.Errorf("lost %s", value)
		}
	}
}

func TestRefreshCannotUndoDisconnectOrReconnect(t *testing.T) {
	for _, action := range []string{"disconnect", "reconnect"} {
		t.Run(action, func(t *testing.T) {
			st := NewStore(t.TempDir())
			o := NewOAuth(st)
			if err := o.write(claudeCred{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: 1}); err != nil {
				t.Fatal(err)
			}
			started, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				<-release
				json.NewEncoder(w).Encode(map[string]any{"access_token": "stale-result", "expires_in": 3600})
			}))
			defer server.Close()
			old := oauthTokenURL
			oauthTokenURL = server.URL
			defer func() { oauthTokenURL = old }()
			result := make(chan error, 1)
			go func() { result <- o.Refresh() }()
			<-started
			if action == "disconnect" {
				if err := st.Delete("claude-code"); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := o.write(claudeCred{AccessToken: "new-account", ExpiresAt: 4102444800000}); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if err := <-result; err == nil {
				t.Fatal("stale refresh must fail")
			}
			if action == "disconnect" && st.Connected("claude-code") {
				t.Fatal("resurrected deleted account")
			}
			if action == "reconnect" {
				cred, _ := o.read()
				if cred.AccessToken != "new-account" {
					t.Fatal("overwrote new account")
				}
			}
		})
	}
}

func TestImportedRefreshPreservesOtherProvidersAndRotatedTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"access_token": testJWT(time.Now().Add(time.Hour).Unix()), "refresh_token": "rotated", "expires_in": 3600})
	}))
	defer server.Close()
	old := codexTokenURL
	codexTokenURL = server.URL
	defer func() { codexTokenURL = old }()
	st := NewStore(t.TempDir())
	rel, _ := CredentialFile("opencode")
	if err := st.ImportCredential("opencode", rel, []byte(`{"openai":{"type":"oauth","access":"old","refresh":"original","expires":1,"accountId":"same-account","custom":"keep"},"opencode":{"type":"api","key":"keep-key"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := st.RefreshImported(context.Background(), "opencode"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(st.Dir("opencode"), rel))
	for _, value := range []string{"rotated", "same-account", "keep-key", "custom"} {
		if !strings.Contains(string(raw), value) {
			t.Errorf("lost %s", value)
		}
	}
}

func TestRefreshErrorsNeverExposeProviderBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret-refresh-token", 401) }))
	defer server.Close()
	old := codexTokenURL
	codexTokenURL = server.URL
	defer func() { codexTokenURL = old }()
	_, err := refreshCodexTokens(context.Background(), "secret-refresh-token")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestReconnectNeverTemporarilyLooksLikeFreeTier(t *testing.T) {
	st := NewStore(t.TempDir())
	if err := st.ImportCredential("opencode", APIKeyFile, []byte("old-key")); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		defer close(done)
		for i := 0; i < 60; i++ {
			if err := st.ImportCredential("opencode", APIKeyFile, []byte("new-key")); err != nil {
				result <- err
				return
			}
		}
		result <- nil
	}()
	for {
		if st.Method("opencode") != "api_key" {
			t.Error("reconnect exposed an unconnected state that could route a paid request to the free tier")
			break
		}
		select {
		case <-done:
			if err := <-result; err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
	}
	<-done
}
