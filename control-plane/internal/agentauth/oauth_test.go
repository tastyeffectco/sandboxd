package agentauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestStartBuildsPKCEURL(t *testing.T) {
	o := NewOAuth(NewStore(t.TempDir()))
	raw, err := o.Start()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"code": "true", "response_type": "code", "code_challenge_method": "S256",
		"client_id": oauthClientID,
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q; want %q", k, q.Get(k), want)
		}
	}
	if q.Get("code_challenge") == "" || q.Get("state") == "" {
		t.Error("missing PKCE challenge/state")
	}
	if len(o.pending) != 1 {
		t.Errorf("Start should stash one pending verifier, got %d", len(o.pending))
	}
}

// Finish exchanges the code against the token endpoint and writes the credential
// in the CLI's claudeAiOauth shape; the code_verifier from Start is sent.
func TestFinishExchangeWritesCredential(t *testing.T) {
	var sentVerifier, sentGrant string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		sentVerifier, sentGrant = body["code_verifier"], body["grant_type"]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "AT-123", "refresh_token": "RT-456", "expires_in": 3600,
		})
	}))
	defer ts.Close()
	old := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = old }()

	st := NewStore(t.TempDir())
	o := NewOAuth(st)
	authURL, _ := o.Start()
	state := mustQuery(t, authURL, "state")

	if err := o.Finish("THECODE#" + state); err != nil {
		t.Fatal(err)
	}
	if sentGrant != "authorization_code" || sentVerifier == "" {
		t.Errorf("exchange sent grant=%q verifier=%q", sentGrant, sentVerifier)
	}
	// Stored in the CLI shape with the returned token.
	got, err := o.read()
	if err != nil || got.AccessToken != "AT-123" || got.RefreshToken != "RT-456" {
		t.Fatalf("credential not written: %+v err=%v", got, err)
	}
	if !st.Connected("claude-code") {
		t.Error("claude-code should read as connected after finish")
	}
}

// Refresh renews an expired token using the stored refresh token, keeping the
// old refresh token when the endpoint omits a rotated one.
func TestRefreshRenewsExpired(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "AT-NEW", "expires_in": 3600})
	}))
	defer ts.Close()
	old := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = old }()

	st := NewStore(t.TempDir())
	o := NewOAuth(st)
	// Seed an already-expired credential with a refresh token.
	_ = o.write(claudeCred{AccessToken: "AT-OLD", RefreshToken: "RT-KEEP", ExpiresAt: 1})
	if err := o.Refresh(); err != nil {
		t.Fatal(err)
	}
	got, _ := o.read()
	if got.AccessToken != "AT-NEW" {
		t.Errorf("access token not refreshed: %q", got.AccessToken)
	}
	if got.RefreshToken != "RT-KEEP" {
		t.Errorf("refresh token should be preserved when endpoint omits it: %q", got.RefreshToken)
	}
}

func TestRefreshNoopWhenFresh(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("token endpoint must not be called while the token is fresh")
	}))
	defer ts.Close()
	old := oauthTokenURL
	oauthTokenURL = ts.URL
	defer func() { oauthTokenURL = old }()

	o := NewOAuth(NewStore(t.TempDir()))
	// Far-future expiry → Refresh is a no-op.
	_ = o.write(claudeCred{AccessToken: "AT", RefreshToken: "RT", ExpiresAt: 4102444800000})
	if err := o.Refresh(); err != nil {
		t.Fatal(err)
	}
}

func mustQuery(t *testing.T, raw, key string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	v := u.Query().Get(key)
	if v == "" {
		t.Fatalf("no %s in %s", key, raw)
	}
	if strings.Contains(v, " ") {
		t.Fatalf("unexpected space in %s", key)
	}
	return v
}
