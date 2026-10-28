package agentauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var codexTokenURL = "https://auth.openai.com/oauth/token"

const codexClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

// RefreshCodex keeps OAuth refresh material on the control plane. Call before
// account discovery and inference so a newly imported expired login is handled
// immediately rather than waiting for a periodic background worker.
func (s *Store) RefreshCodex(ctx context.Context) error {
	if s.Method("codex") != "oauth" {
		return nil
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	rel := ".codex/auth.json"
	raw, err := os.ReadFile(filepath.Join(s.Dir("codex"), rel))
	if err != nil {
		return fmt.Errorf("Codex login is unavailable; reconnect")
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return fmt.Errorf("invalid Codex login; reconnect")
	}
	var tokens map[string]string
	if json.Unmarshal(doc["tokens"], &tokens) != nil || tokens["access_token"] == "" {
		return fmt.Errorf("invalid Codex subscription login; reconnect")
	}
	expiry := tokenExpiry(tokens["access_token"])
	if expiry == 0 || time.Now().Add(2*time.Minute).Unix() < expiry {
		return nil
	}
	if tokens["refresh_token"] == "" {
		return fmt.Errorf("Codex subscription login expired; reconnect")
	}
	next, err := refreshCodexTokens(ctx, tokens["refresh_token"])
	if err != nil {
		return err
	}
	tokens["access_token"] = next.AccessToken
	if next.RefreshToken != "" {
		tokens["refresh_token"] = next.RefreshToken
	}
	if next.IDToken != "" {
		tokens["id_token"] = next.IDToken
	}
	doc["tokens"], _ = json.Marshal(tokens)
	doc["last_refresh"], _ = json.Marshal(time.Now().UTC().Format(time.RFC3339))
	updated, _ := json.Marshal(doc)
	return s.replaceCredential("codex", rel, raw, updated)
}

type refreshedCodexTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func refreshCodexTokens(ctx context.Context, refresh string) (refreshedCodexTokens, error) {
	payload, _ := json.Marshal(map[string]string{"grant_type": "refresh_token", "refresh_token": refresh, "client_id": codexClientID})
	req, err := http.NewRequestWithContext(ctx, "POST", codexTokenURL, bytes.NewReader(payload))
	if err != nil {
		return refreshedCodexTokens{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return refreshedCodexTokens{}, fmt.Errorf("could not refresh Codex subscription; retry or reconnect")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return refreshedCodexTokens{}, fmt.Errorf("Codex subscription refresh failed (HTTP %d); reconnect", response.StatusCode)
	}
	var next refreshedCodexTokens
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&next) != nil || next.AccessToken == "" {
		return refreshedCodexTokens{}, fmt.Errorf("invalid Codex refresh response; reconnect")
	}
	return next, nil
}

func tokenExpiry(token string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return 0
	}
	return claims.Exp
}
