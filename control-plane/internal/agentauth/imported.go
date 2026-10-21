package agentauth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RefreshImported renews supported CLI logins without giving sandbox processes
// access to refresh tokens. Unknown OAuth providers must be reconnected through
// their own CLI; they are never sent to another provider's token endpoint.
func (s *Store) RefreshImported(ctx context.Context, agent string) error {
	if (agent != "pi" && agent != "opencode") || s.Method(agent) != "oauth" {
		return nil
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	rel, _ := CredentialFile(agent)
	before, err := os.ReadFile(filepath.Join(s.Dir(agent), rel))
	if err != nil {
		return fmt.Errorf("account login unavailable; reconnect %s", agent)
	}
	var accounts map[string]map[string]json.RawMessage
	if json.Unmarshal(before, &accounts) != nil {
		return fmt.Errorf("invalid account login; reconnect %s", agent)
	}
	for name, doc := range accounts {
		if (name == "openai-codex" && agent != "pi") || (name != "openai-codex" && name != "openai" && name != "anthropic") {
			continue
		}
		var kind, access, refresh string
		var expires int64
		_ = json.Unmarshal(doc["type"], &kind)
		if kind != "oauth" {
			continue
		}
		_ = json.Unmarshal(doc["access"], &access)
		_ = json.Unmarshal(doc["refresh"], &refresh)
		_ = json.Unmarshal(doc["expires"], &expires)
		if expires == 0 {
			expires = tokenExpiry(access) * 1000
		}
		if expires == 0 || expires > time.Now().Add(2*time.Minute).UnixMilli() {
			continue
		}
		if refresh == "" {
			return fmt.Errorf("%s login expired; reconnect %s", name, agent)
		}
		var nextAccess, nextRefresh string
		var nextExpires int64
		switch {
		case name == "openai-codex" || (agent == "opencode" && name == "openai"):
			next, err := refreshCodexTokens(ctx, refresh)
			if err != nil {
				return err
			}
			nextAccess, nextRefresh = next.AccessToken, next.RefreshToken
			nextExpires = tokenExpiry(nextAccess) * 1000
			if next.ExpiresIn > 0 {
				nextExpires = time.Now().Add(time.Duration(next.ExpiresIn) * time.Second).UnixMilli()
			}
		case name == "anthropic":
			next, err := NewOAuth(s).exchange(map[string]string{"grant_type": "refresh_token", "refresh_token": refresh, "client_id": oauthClientID})
			if err != nil {
				return err
			}
			nextAccess, nextRefresh, nextExpires = next.AccessToken, next.RefreshToken, next.ExpiresAt
		default:
			// Unsupported accounts are not part of sandboxd's model catalog.
			if name != "openai" {
				continue
			}
			return fmt.Errorf("%s login expired; reconnect using %s and import the updated login", name, agent)
		}
		doc["access"], _ = json.Marshal(nextAccess)
		if nextRefresh != "" {
			doc["refresh"], _ = json.Marshal(nextRefresh)
		}
		doc["expires"], _ = json.Marshal(nextExpires)
		// Persist each rotated token immediately so a later provider failure cannot
		// lose a successfully renewed login. Compare-and-replace protects reconnects.
		after, _ := json.Marshal(accounts)
		if err := s.replaceCredential(agent, rel, before, after); err != nil {
			return err
		}
		before = after
	}
	return nil
}
