// v1_git_push.go — Git B2: push locally-committed workspace changes to the app's
// remote. Host-side/control-plane only (never in-sandbox): the token is decrypted
// HERE and reaches git only via gitimport's host-checking askpass. The push
// target comes from app.git_repo_url + app.git_credential_id metadata — NEVER the
// repo's .git/config origin. New branch only, no force, no fetch/pull, no deepen.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/events"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/gitimport"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// pushRunner is the host-side git seam (real impl = gitimport.Runner; faked in tests).
type pushRunner interface {
	AuditConfig(ctx context.Context, appDir string) ([]string, error)
	RepoState(ctx context.Context, appDir string) (gitimport.RepoState, error)
	Unpushed(ctx context.Context, appDir, importBranch string) (int, bool, error)
	Push(ctx context.Context, spec gitimport.PushSpec) gitimport.PushOutcome
}

func (s *Server) pusher() pushRunner {
	if s.GitPush != nil {
		return s.GitPush
	}
	return gitimport.Runner{}
}

type v1GitPushReq struct {
	Branch string `json:"branch"`
}

type v1GitPushResp struct {
	Pushed       bool   `json:"pushed"`
	Reason       string `json:"reason,omitempty"`
	Branch       string `json:"branch,omitempty"`
	RemoteURL    string `json:"remote_url,omitempty"` // tokenless
	Commits      int    `json:"commits,omitempty"`
	HeadDetached bool   `json:"head_detached,omitempty"`
}

// reservedDefaultBranches are remote default-branch names we refuse to push to in
// B2 (new-branch only). develop/trunk are NOT reserved unless they equal the
// import branch (handled separately).
var reservedDefaultBranches = map[string]bool{"main": true, "master": true}

// POST /v1/apps/{id}/git/push
func (s *Server) v1GitPush(w http.ResponseWriter, r *http.Request) {
	app, err := s.Store.GetAppForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	var req v1GitPushReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid json: "+err.Error())
		return
	}

	unavail := func(reason string) { writeJSON(w, http.StatusOK, v1GitPushResp{Pushed: false, Reason: reason}) }

	// 1. Git metadata is the source of truth (never .git/config origin).
	if !app.GitRepoURL.Valid || app.GitRepoURL.String == "" || gitimport.ValidateRepoURL(app.GitRepoURL.String) != nil {
		unavail("no_repo_url")
		return
	}
	repoURL := app.GitRepoURL.String
	if !app.GitCredentialID.Valid || app.GitCredentialID.String == "" {
		unavail("no_credential")
		return
	}
	importBranch := "main"
	if app.GitBranch.Valid && app.GitBranch.String != "" {
		importBranch = app.GitBranch.String
	}

	// 2. Workspace (host-side; works whether the sandbox runs or is stopped).
	sb, err := s.Store.CurrentSandboxForApp(r.Context(), app.ID)
	if err != nil {
		unavail("no_workspace")
		return
	}
	// Acquire the SAME per-workspace git mutation lock as commit, so a push can't
	// overlap a commit (which would let it publish a stale HEAD). Everything
	// below — RepoState/HEAD, audit, unpushed, and the push itself — runs under
	// the lock, so the HEAD we push is the one we evaluated.
	s.Locks.Lock(gitLockKey(sb.ID))
	defer s.Locks.Unlock(gitLockKey(sb.ID))
	appDir := filepath.Join(sb.WorkspaceMnt, "workspace", "app")
	runner := s.pusher()

	// 3. Repo state + UNSAFE-CONFIG audit (before any network/credential use).
	state, err := runner.RepoState(r.Context(), appDir)
	if err != nil {
		unavail("push_failed")
		return
	}
	if !state.IsRepo {
		unavail("not_a_git_repo")
		return
	}
	if !state.HasHEAD {
		unavail("empty_repo_unsupported")
		return
	}
	if flagged, err := runner.AuditConfig(r.Context(), appDir); err != nil {
		unavail("push_failed")
		return
	} else if len(flagged) > 0 {
		unavail("unsafe_repo_config")
		return
	}

	// 4. Target branch (NEW branch only; reject import + main/master).
	target := strings.TrimSpace(req.Branch)
	if target == "" {
		target = defaultPushBranch(app.Name, state.HeadShort)
	}
	if gitimport.ValidateBranch(target) != nil {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid branch name")
		return
	}
	if strings.EqualFold(target, importBranch) || reservedDefaultBranches[strings.ToLower(target)] {
		unavail("refuses_default_branch")
		return
	}

	// 5. Unpushed commits (no network). Conservative when baseline is absent.
	commits, _, err := runner.Unpushed(r.Context(), appDir, importBranch)
	if err != nil {
		unavail("push_failed")
		return
	}
	if commits == 0 {
		unavail("no_local_commits")
		return
	}

	// 6. Decrypt the owner-scoped credential — ONLY here, in the push path.
	if s.Secrets == nil {
		writeV1Err(w, http.StatusServiceUnavailable, "unavailable", "credential store not configured")
		return
	}
	enc, nonce, found, err := s.Store.GetGitCredentialSecret(r.Context(), tenantToken(r), app.GitCredentialID.String)
	if err != nil {
		unavail("push_failed")
		return
	}
	if !found {
		unavail("credential_not_found")
		return
	}
	token, derr := s.Secrets.Open(enc, nonce)
	if derr != nil {
		unavail("push_failed")
		return
	}

	// 7. Push (host-checking askpass; tokenless URL; no force; no deepen).
	outcome := runner.Push(r.Context(), gitimport.PushSpec{
		RepoURL:    repoURL,
		Target:     target,
		Token:      string(token),
		AppDir:     appDir,
		ExpectHost: gitimport.HostOf(repoURL),
	})
	for i := range token { // best-effort scrub
		token[i] = 0
	}

	if !outcome.OK {
		s.auditAction(r, audit.Entry{Action: "git.push.failed", Target: app.ID})
		s.recordEvent(r, events.Event{Type: events.GitRepoPushFailed, Severity: events.SeverityWarning,
			Message: "Git push failed", AppID: app.ID, SandboxID: sb.ID,
			Payload: map[string]any{"repo_url": repoURL, "branch": target, "reason": outcome.Reason}})
		unavail(outcome.Reason)
		return
	}
	s.auditAction(r, audit.Entry{Action: "git.push", Target: app.ID})
	s.recordEvent(r, events.Event{Type: events.GitRepoPushed, Severity: events.SeverityInfo,
		Message: "Pushed to " + target, AppID: app.ID, SandboxID: sb.ID,
		Payload: map[string]any{"repo_url": repoURL, "branch": target, "commits": commits}})
	writeJSON(w, http.StatusOK, v1GitPushResp{
		Pushed: true, Branch: target, RemoteURL: repoURL, Commits: commits, HeadDetached: state.Detached,
	})
}

// defaultPushBranch builds a safe NEW branch name: sandboxd/<slug>-<shortsha>.
func defaultPushBranch(appName, headShort string) string {
	slug := slugify(appName)
	if slug == "" {
		slug = "app"
	}
	if headShort == "" {
		headShort = "work"
	}
	return "sandboxd/" + slug + "-" + headShort
}

// slugify lowercases and keeps [a-z0-9-], collapsing the rest to single dashes.
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
			prevDash = false
		} else if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
		if b.Len() >= 32 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}
