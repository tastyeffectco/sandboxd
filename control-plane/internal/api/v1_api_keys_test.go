package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAPIKeysCRUD(t *testing.T) {
	h, _ := authTestServer(t)
	// establish a console session
	w := doAuth(t, h, "POST", "/v1/auth/setup", `{"password":"correcthorse"}`, "", "")
	if w.Code != 204 {
		t.Fatalf("setup: %d", w.Code)
	}
	cookie := sessionFrom(t, w)

	// create → 201 with plaintext key + prefix
	w = doAuth(t, h, "POST", "/v1/api-keys", `{"name":"ci-bot"}`, cookie, "")
	if w.Code != 201 {
		t.Fatalf("create: want 201 got %d %s", w.Code, w.Body.String())
	}
	var created struct{ ID, Name, Prefix, Key string }
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Key, "sk_") || created.Prefix == "" || created.ID == "" {
		t.Fatalf("create payload: %+v", created)
	}

	// list → metadata only, no key/hash
	w = doAuth(t, h, "GET", "/v1/api-keys", "", cookie, "")
	if w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "ci-bot") || strings.Contains(body, created.Key) {
		t.Fatalf("list leaked or missing: %s", body)
	}

	// the created key authenticates as a SERVICE actor → cannot mint keys (403)
	if w := doAuth(t, h, "POST", "/v1/api-keys", `{"name":"evil"}`, "", created.Key); w.Code != 403 {
		t.Fatalf("service actor minting key: want 403 got %d", w.Code)
	}
	// but the created key DOES authorize an ordinary protected route
	if w := doAuth(t, h, "GET", "/v1/apps", "", "", created.Key); w.Code == 401 {
		t.Fatalf("valid api key rejected on /v1/apps: %d", w.Code)
	}

	// duplicate name → 409
	if w := doAuth(t, h, "POST", "/v1/api-keys", `{"name":"ci-bot"}`, cookie, ""); w.Code != 409 {
		t.Fatalf("dup name: want 409 got %d", w.Code)
	}

	// delete → 204, then 404
	if w := doAuth(t, h, "DELETE", "/v1/api-keys/"+created.ID, "", cookie, ""); w.Code != 204 {
		t.Fatalf("delete: want 204 got %d", w.Code)
	}
	if w := doAuth(t, h, "DELETE", "/v1/api-keys/"+created.ID, "", cookie, ""); w.Code != 404 {
		t.Fatalf("re-delete: want 404 got %d", w.Code)
	}
	// the revoked key no longer authenticates
	if w := doAuth(t, h, "GET", "/v1/apps", "", "", created.Key); w.Code != 401 {
		t.Fatalf("revoked key still works: %d", w.Code)
	}
}

func TestAPIKeyScopes(t *testing.T) {
	h, _ := authTestServer(t)
	w := doAuth(t, h, "POST", "/v1/auth/setup", `{"password":"correcthorse"}`, "", "")
	if w.Code != 204 {
		t.Fatalf("setup: %d", w.Code)
	}
	cookie := sessionFrom(t, w)

	// unknown scope → 400
	if w := doAuth(t, h, "POST", "/v1/api-keys", `{"name":"bad","scopes":["root"]}`, cookie, ""); w.Code != 400 {
		t.Fatalf("unknown scope: want 400 got %d %s", w.Code, w.Body.String())
	}

	type created struct {
		ID, Key string
		Scopes  []string
	}
	mint := func(body string) created {
		t.Helper()
		w := doAuth(t, h, "POST", "/v1/api-keys", body, cookie, "")
		if w.Code != 201 {
			t.Fatalf("create %s: want 201 got %d %s", body, w.Code, w.Body.String())
		}
		var c created
		if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	full := mint(`{"name":"full"}`)
	if len(full.Scopes) != 0 {
		t.Fatalf("full key scopes = %v", full.Scopes)
	}
	upg := mint(`{"name":"updater","scopes":["upgrade"]}`)
	if len(upg.Scopes) != 1 || upg.Scopes[0] != "upgrade" {
		t.Fatalf("scoped key scopes = %v", upg.Scopes)
	}

	// list reports scopes
	w = doAuth(t, h, "GET", "/v1/api-keys", "", cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"scopes":["upgrade"]`) || !strings.Contains(w.Body.String(), `"scopes":[]`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}

	// the scoped key reaches the upgrade handlers …
	if w := doAuth(t, h, "GET", "/v1/upgrade", "", "", upg.Key); w.Code != 200 {
		t.Fatalf("scoped GET /v1/upgrade: want 200 got %d %s", w.Code, w.Body.String())
	}
	if w := doAuth(t, h, "POST", "/v1/upgrade", `{"target":"v0.0.0"}`, "", upg.Key); w.Code == 401 || w.Code == 403 {
		t.Fatalf("scoped POST /v1/upgrade must reach the handler, got %d", w.Code)
	}
	// … and nothing else.
	for _, rt := range [][2]string{
		{"GET", "/v1/sandboxes"}, {"GET", "/v1/apps"}, {"GET", "/v1/api-keys"},
		{"POST", "/v1/api-keys"}, {"DELETE", "/v1/api-keys/" + full.ID},
	} {
		w := doAuth(t, h, rt[0], rt[1], "", "", upg.Key)
		if w.Code != 403 {
			t.Fatalf("scoped %s %s: want 403 got %d", rt[0], rt[1], w.Code)
		}
		if !strings.Contains(w.Body.String(), `"code":"forbidden"`) || !strings.Contains(w.Body.String(), "limited to: upgrade") {
			t.Fatalf("scoped %s %s body: %s", rt[0], rt[1], w.Body.String())
		}
	}
	// the full key is unaffected
	if w := doAuth(t, h, "GET", "/v1/apps", "", "", full.Key); w.Code != 200 {
		t.Fatalf("full key /v1/apps: %d", w.Code)
	}
	if w := doAuth(t, h, "GET", "/v1/upgrade", "", "", full.Key); w.Code != 200 {
		t.Fatalf("full key /v1/upgrade: %d", w.Code)
	}
	// the full key was not deleted by the scoped DELETE attempt
	if w := doAuth(t, h, "DELETE", "/v1/api-keys/"+full.ID, "", cookie, ""); w.Code != 204 {
		t.Fatalf("delete full: %d", w.Code)
	}
}
