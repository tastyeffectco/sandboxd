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
