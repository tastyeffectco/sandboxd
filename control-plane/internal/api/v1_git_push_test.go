package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/gitimport"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

type fakePusher struct {
	audit         []string
	state         gitimport.RepoState
	count         int
	baselineKnown bool
	outcome       gitimport.PushOutcome
	gotSpec       gitimport.PushSpec
	pushed        bool
}

func (f *fakePusher) AuditConfig(_ context.Context, _ string) ([]string, error) { return f.audit, nil }
func (f *fakePusher) RepoState(_ context.Context, _ string) (gitimport.RepoState, error) {
	return f.state, nil
}
func (f *fakePusher) Unpushed(_ context.Context, _, _ string) (int, bool, error) {
	return f.count, f.baselineKnown, nil
}
func (f *fakePusher) Push(_ context.Context, spec gitimport.PushSpec) gitimport.PushOutcome {
	f.gotSpec = spec
	f.pushed = true
	return f.outcome
}

// pushServer builds a server with Secrets + a fake pusher, a git-imported app
// (with a real sealed credential), and a sandbox in the given status.
func pushServer(t *testing.T, f *fakePusher, sandboxStatus string, withGit bool) (*Server, string) {
	t.Helper()
	s := appsGitServer(t) // Store + Secrets
	s.GitPush = f
	app := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "My App"}
	if withGit {
		cid := seedCred(t, s, "tenantA", "gh") // seals "ghp_pat_secret"
		app.GitRepoURL = nullStr("https://github.com/o/r")
		app.GitBranch = nullStr("main")
		app.GitCredentialID = nullStr(cid)
	}
	if err := s.Store.CreateApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if sandboxStatus != "" {
		sb := &store.Sandbox{ID: newULID(), Status: sandboxStatus, WorkspaceMnt: t.TempDir(),
			AppID: sql.NullString{String: app.ID, Valid: true}}
		if err := s.Store.Create(context.Background(), sb); err != nil {
			t.Fatal(err)
		}
	}
	return s, app.ID
}

func postPush(s *Server, appID, owner, body string) *httptest.ResponseRecorder {
	r := reqAs("POST", "/v1/apps/"+appID+"/git/push", body, owner)
	r.SetPathValue("id", appID)
	w := httptest.NewRecorder()
	s.v1GitPush(w, r)
	return w
}

func okState() gitimport.RepoState {
	return gitimport.RepoState{IsRepo: true, HasHEAD: true, HeadShort: "abc1234"}
}

func TestPushSuccess(t *testing.T) {
	f := &fakePusher{state: okState(), count: 2, outcome: gitimport.PushOutcome{OK: true}}
	s, appID := pushServer(t, f, "running", true)
	w := postPush(s, appID, "tenantA", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var resp v1GitPushResp
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Pushed || resp.Commits != 2 || resp.RemoteURL != "https://github.com/o/r" {
		t.Fatalf("resp = %+v", resp)
	}
	if !strings.HasPrefix(resp.Branch, "sandboxd/my-app-") {
		t.Errorf("default branch = %q", resp.Branch)
	}
	// the credential was decrypted ONLY in the push path and reached the pusher
	if f.gotSpec.Token != "ghp_pat_secret" {
		t.Errorf("pusher did not receive the decrypted token")
	}
	if f.gotSpec.ExpectHost != "github.com" {
		t.Errorf("ExpectHost = %q", f.gotSpec.ExpectHost)
	}
	// the token must never appear in the response
	if strings.Contains(w.Body.String(), "ghp_pat_secret") {
		t.Fatal("token leaked into the push response")
	}
}

func TestPushWorksWhenSandboxStopped(t *testing.T) {
	f := &fakePusher{state: okState(), count: 1, outcome: gitimport.PushOutcome{OK: true}}
	s, appID := pushServer(t, f, "stopped", true)
	w := postPush(s, appID, "tenantA", `{}`)
	var resp v1GitPushResp
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Pushed {
		t.Errorf("push should work with a stopped sandbox: %s", w.Body.String())
	}
}

func TestPushPreflightReasons(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(f *fakePusher)
		body   string
		git    bool
		status string
		want   string
	}{
		{"no_repo_url", func(f *fakePusher) { f.state = okState() }, `{}`, false, "running", "no_repo_url"},
		{"not_a_git_repo", func(f *fakePusher) { f.state = gitimport.RepoState{IsRepo: false} }, `{}`, true, "running", "not_a_git_repo"},
		{"empty_repo", func(f *fakePusher) { f.state = gitimport.RepoState{IsRepo: true, HasHEAD: false} }, `{}`, true, "running", "empty_repo_unsupported"},
		{"unsafe_config", func(f *fakePusher) { f.state = okState(); f.audit = []string{"url.x.insteadof"} }, `{}`, true, "running", "unsafe_repo_config"},
		{"no_local_commits", func(f *fakePusher) { f.state = okState(); f.count = 0 }, `{}`, true, "running", "no_local_commits"},
		{"refuses_main", func(f *fakePusher) { f.state = okState(); f.count = 1 }, `{"branch":"main"}`, true, "running", "refuses_default_branch"},
		{"refuses_import_branch", func(f *fakePusher) { f.state = okState(); f.count = 1 }, `{"branch":"main"}`, true, "running", "refuses_default_branch"},
		{"no_workspace", func(f *fakePusher) { f.state = okState() }, `{}`, true, "", "no_workspace"},
		{"push_branch_exists", func(f *fakePusher) {
			f.state = okState()
			f.count = 1
			f.outcome = gitimport.PushOutcome{Reason: "branch_exists"}
		}, `{}`, true, "running", "branch_exists"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakePusher{}
			c.setup(f)
			s, appID := pushServer(t, f, c.status, c.git)
			w := postPush(s, appID, "tenantA", c.body)
			var resp v1GitPushResp
			json.Unmarshal(w.Body.Bytes(), &resp)
			if resp.Pushed || resp.Reason != c.want {
				t.Errorf("got pushed=%v reason=%q want %q (%s)", resp.Pushed, resp.Reason, c.want, w.Body.String())
			}
		})
	}
}

func TestPushCredentialAndAuthGuards(t *testing.T) {
	// credential id set but the row doesn't exist -> credential_not_found
	f := &fakePusher{state: okState(), count: 1}
	s := appsGitServer(t)
	s.GitPush = f
	app := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "imp",
		GitRepoURL: nullStr("https://github.com/o/r"), GitBranch: nullStr("main"),
		GitCredentialID: nullStr("01MISSINGCRED")}
	s.Store.CreateApp(context.Background(), app)
	sb := &store.Sandbox{ID: newULID(), Status: "running", WorkspaceMnt: t.TempDir(),
		AppID: sql.NullString{String: app.ID, Valid: true}}
	s.Store.Create(context.Background(), sb)
	w := postPush(s, app.ID, "tenantA", `{}`)
	if !strings.Contains(w.Body.String(), "credential_not_found") {
		t.Errorf("expected credential_not_found: %s", w.Body.String())
	}
	// the fake pusher must NOT have been called (we never reached the push)
	if f.pushed {
		t.Error("push ran despite missing credential")
	}
}

func TestPushOwnerAndValidation(t *testing.T) {
	f := &fakePusher{state: okState(), count: 1, outcome: gitimport.PushOutcome{OK: true}}
	s, appID := pushServer(t, f, "running", true)

	// cross-owner -> 404
	if w := postPush(s, appID, "tenantB", `{}`); w.Code != http.StatusNotFound {
		t.Errorf("cross-owner: got %d", w.Code)
	}
	// invalid branch ref -> 400
	if w := postPush(s, appID, "tenantA", `{"branch":"../evil"}`); w.Code != http.StatusBadRequest {
		t.Errorf("invalid branch: got %d", w.Code)
	}
	// no credential associated -> no_credential
	app2 := &store.App{ID: newULID(), OwnerToken: "tenantA", Name: "x", GitRepoURL: nullStr("https://github.com/o/r")}
	s.Store.CreateApp(context.Background(), app2)
	sb := &store.Sandbox{ID: newULID(), Status: "running", WorkspaceMnt: t.TempDir(),
		AppID: sql.NullString{String: app2.ID, Valid: true}}
	s.Store.Create(context.Background(), sb)
	if w := postPush(s, app2.ID, "tenantA", `{}`); !strings.Contains(w.Body.String(), "no_credential") {
		t.Errorf("expected no_credential: %s", w.Body.String())
	}
}
