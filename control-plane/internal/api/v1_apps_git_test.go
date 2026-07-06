package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/idlock"
	"github.com/sandboxd/control-plane/internal/secrets"
	"github.com/sandboxd/control-plane/internal/store"
)

func appsGitServer(t *testing.T) *Server {
	t.Helper()
	cipher, err := secrets.Load("", t.TempDir()+"/secrets.key")
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Store: memStore(t), Secrets: cipher, Locks: idlock.New()}
}

// seedCred stores an owner-scoped git credential and returns its id.
func seedCred(t *testing.T, s *Server, owner, name string) string {
	t.Helper()
	ct, nonce, err := s.Secrets.Seal([]byte("ghp_pat_secret"))
	if err != nil {
		t.Fatal(err)
	}
	g := &store.GitCredential{ID: newULID(), OwnerToken: owner, Name: name, Host: "github.com"}
	if err := s.Store.CreateGitCredential(context.Background(), g, ct, nonce); err != nil {
		t.Fatal(err)
	}
	return g.ID
}

func createApp(s *Server, body, owner string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.v1CreateApp(w, reqAs("POST", "/v1/apps", body, owner))
	return w
}

func TestAppGitImportStoresTokenlessMetadata(t *testing.T) {
	s := appsGitServer(t)
	cid := seedCred(t, s, "tenantA", "gh")
	w := createApp(s, `{"name":"imp","runtime_preset":"nextjs","git":{"repo_url":"https://github.com/org/repo.git","branch":"main","credential_id":"`+cid+`"}}`, "tenantA")
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	g, _ := got["git"].(map[string]any)
	if g == nil || g["repo_url"] != "https://github.com/org/repo.git" || g["branch"] != "main" {
		t.Fatalf("git metadata wrong: %v", got["git"])
	}
	// credential id is referenced; no token anywhere in the response
	if g["credential_id"] != cid {
		t.Errorf("credential_id = %v", g["credential_id"])
	}
	if strings.Contains(w.Body.String(), "ghp_pat_secret") {
		t.Fatal("app response leaked the token")
	}
}

func TestAppGitImportValidation(t *testing.T) {
	s := appsGitServer(t)
	cid := seedCred(t, s, "tenantA", "gh")
	cases := []struct {
		body  string
		owner string
		want  int
		why   string
	}{
		{`{"name":"a","git":{"repo_url":"http://github.com/o/r","credential_id":"` + cid + `"}}`, "tenantA", 400, "non-https url"},
		{`{"name":"a","git":{"repo_url":"https://u:p@github.com/o/r","credential_id":"` + cid + `"}}`, "tenantA", 400, "url with creds"},
		{`{"name":"a","git":{"repo_url":"https://github.com/o/r","branch":"-evil","credential_id":"` + cid + `"}}`, "tenantA", 400, "bad branch"},
		{`{"name":"a","git":{"repo_url":"https://github.com/o/r","credential_id":"01NONEXISTENT"}}`, "tenantA", 404, "unknown credential"},
		{`{"name":"a","git":{"repo_url":"https://github.com/o/r","credential_id":"` + cid + `"}}`, "tenantB", 404, "cross-owner credential"},
		{`{"name":"a","git":{"repo_url":"https://github.com/o/r"}}`, "tenantA", 201, "no credential_id → public tokenless import allowed"},
	}
	for _, c := range cases {
		w := createApp(s, c.body, c.owner)
		if w.Code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.why, w.Code, c.want, w.Body.String())
		}
	}
}

func TestBlankAppFlowStillWorks(t *testing.T) {
	s := appsGitServer(t)
	w := createApp(s, `{"name":"blank","runtime_preset":"react-vite"}`, "t")
	if w.Code != http.StatusCreated {
		t.Fatalf("blank app: got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"git"`) {
		t.Error("blank app should have no git metadata")
	}
}
