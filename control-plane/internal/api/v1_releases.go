package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/audit"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/docker"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/manifest"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/preset"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/release"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// Publishing an app (ADR 0003, C2/C3).
//
// On the engine where the app is built:  GET /v1/apps/{id}/bundle returns its
// code at the last saved commit. On the server engine: POST
// /v1/apps/{id}/releases takes that bundle and publishes it — unpack, build
// in a one-off container while the live version keeps serving, switch,
// restart, wait for the app to answer, and switch back if it does not.

const (
	maxBundleBytes             = 512 << 20 // upload
	maxUnpackedBytes           = 4 << 30   // file content once unpacked
	defaultReleaseBuildTimeout = 20 * time.Minute
	defaultReleaseStartTimeout = 5 * time.Minute
	keepPreviousRelease        = 2 // plus the active one: the last three versions
	logTailBytes               = 16 << 10
)

var reCommit = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// Overridable in tests.
var (
	releasePoll         = 2 * time.Second
	releaseBuildTimeout = defaultReleaseBuildTimeout
	releaseStartTimeout = defaultReleaseStartTimeout
	bundleExclude       = []string{":(exclude)BRAIN.md", ":(exclude).claude", ":(exclude).opencode", ":(exclude).env", ":(exclude).env.*"}
)

type v1Release struct {
	ID          string `json:"id"`
	Commit      string `json:"commit"`
	Status      string `json:"status"`
	Active      bool   `json:"active"`
	Message     string `json:"message"`
	CreatedAt   string `json:"created_at"`
	ActivatedAt string `json:"activated_at,omitempty"`
	Log         string `json:"log,omitempty"`
}

type v1Releases struct {
	Releases []v1Release `json:"releases"`
	// URL is where the published app is reached ("" before the first publish).
	URL       string   `json:"url"`
	Hosts     []string `json:"hosts"`
	SandboxID string   `json:"sandbox_id,omitempty"`
}

func v1ReleaseFromRow(r *store.Release) v1Release {
	out := v1Release{ID: r.ID, Commit: r.Commit, Status: r.Status, Active: r.Status == store.ReleaseActive,
		Message: r.Message, CreatedAt: r.CreatedAt.Format(time.RFC3339)}
	if r.ActivatedAt.Valid {
		out.ActivatedAt = time.Unix(r.ActivatedAt.Int64, 0).UTC().Format(time.RFC3339)
	}
	return out
}

// --- bundle (the engine you build on) ---------------------------------

// v1AppBundle — GET /v1/apps/{id}/bundle: the app's tracked files at its last
// commit, as tar.gz. Agent notes and .env files are left out. Headers carry
// the commit and how many changes are not saved in it yet.
func (s *Server) v1AppBundle(w http.ResponseWriter, r *http.Request) {
	app, sb, ok := s.appAndSandbox(w, r)
	if !ok {
		return
	}
	if sb == nil {
		writeV1Err(w, http.StatusConflict, "conflict", "this app has no workspace yet")
		return
	}
	if sb.Mode == store.ModeProduction {
		writeV1Err(w, http.StatusConflict, "conflict", "this is a published copy; publish from the app you build")
		return
	}
	dir := filepath.Join(sb.WorkspaceMnt, "workspace", "app")
	commit, err := hostGit(r.Context(), dir, "rev-parse", "HEAD")
	if err != nil {
		writeV1Err(w, http.StatusConflict, "nothing_saved", "this app has no saved version yet: save your changes first")
		return
	}
	commit = strings.TrimSpace(commit)
	dirty := 0
	if st, err := hostGit(r.Context(), dir, "status", "--porcelain"); err == nil {
		for _, l := range strings.Split(st, "\n") {
			if strings.TrimSpace(l) != "" {
				dirty++
			}
		}
	}
	args := append([]string{"archive", "--format=tar.gz", commit, "--", "."}, bundleExclude...)
	cmd := gitCommand(r.Context(), dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "git: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.tar.gz"`, app.ID, commit[:12]))
	w.Header().Set("X-Sandboxd-Commit", commit)
	w.Header().Set("X-Sandboxd-Uncommitted-Changes", strconv.Itoa(dirty))
	_, _ = io.Copy(w, out)
	if err := cmd.Wait(); err != nil {
		s.Log.Warn("bundle: git archive failed", "app", app.ID, "err", err.Error(), "stderr", clipEnd(stderr.String(), 500))
	}
}

func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	// The workspace belongs to the sandbox user, not to sandboxd: allow it
	// explicitly, and keep any system/global config out.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "safe.directory=" + dir, "-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	return cmd
}

func hostGit(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitCommand(ctx, dir, args...).Output()
	return string(out), err
}

// --- releases (the server engine) -------------------------------------

// appAndSandbox loads the tenant's app and its current sandbox (nil if none).
func (s *Server) appAndSandbox(w http.ResponseWriter, r *http.Request) (*store.App, *store.Sandbox, bool) {
	app, err := s.Store.GetAppForOwner(r.Context(), r.PathValue("id"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such app")
		return nil, nil, false
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return nil, nil, false
	}
	sb, err := s.Store.CurrentSandboxForApp(r.Context(), app.ID)
	if errors.Is(err, store.ErrNotFound) {
		return app, nil, true
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return nil, nil, false
	}
	return app, sb, true
}

// lockRelease allows one publish or go-back per app at a time.
func (s *Server) lockRelease(appID string) bool {
	s.releaseMu.Lock()
	defer s.releaseMu.Unlock()
	if s.releasing == nil {
		s.releasing = map[string]bool{}
	}
	if s.releasing[appID] {
		return false
	}
	s.releasing[appID] = true
	return true
}

func (s *Server) unlockRelease(appID string) {
	s.releaseMu.Lock()
	delete(s.releasing, appID)
	s.releaseMu.Unlock()
}

// v1CreateRelease — POST /v1/apps/{id}/releases?commit=<sha>. Body: a bundle.
func (s *Server) v1CreateRelease(w http.ResponseWriter, r *http.Request) {
	app, sb, ok := s.appAndSandbox(w, r)
	if !ok {
		return
	}
	if sb != nil && sb.Mode != store.ModeProduction {
		writeV1Err(w, http.StatusConflict, "conflict",
			"this app is built here, so it cannot also be published here; publish it from your computer to a server")
		return
	}
	commit := r.URL.Query().Get("commit")
	if commit != "" && !reCommit.MatchString(commit) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "commit must be a git commit id")
		return
	}
	if !s.lockRelease(app.ID) {
		writeV1Err(w, http.StatusConflict, "busy", "this app is already being published; wait for it to finish")
		return
	}
	unlock := true
	defer func() {
		if unlock {
			s.unlockRelease(app.ID)
		}
	}()

	if sb == nil {
		var err error
		if sb, err = s.createProductionSandbox(r, app); err != nil {
			writeV1Err(w, http.StatusInternalServerError, "internal", "could not prepare the app on this server: "+err.Error())
			return
		}
	}
	l := release.Layout{Root: sb.WorkspaceMnt}
	rel := &store.Release{ID: strings.ToLower(newULID()), AppID: app.ID, OwnerToken: app.OwnerToken, Commit: commit, Status: store.ReleaseBuilding}
	if err := os.MkdirAll(filepath.Dir(l.LogPath(rel.ID)), 0o755); err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxBundleBytes)
	if err := release.Unpack(body, l.Dir(rel.ID), maxUnpackedBytes); err != nil {
		_ = os.RemoveAll(l.Dir(rel.ID))
		msg := err.Error()
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) || errors.Is(err, release.ErrTooLarge) {
			msg = "the app is too large to publish (over 512 MB to send or 4 GB unpacked)"
		}
		writeV1Err(w, http.StatusBadRequest, "invalid_bundle", msg)
		return
	}
	if err := s.Store.CreateRelease(r.Context(), rel); err != nil {
		_ = os.RemoveAll(l.Dir(rel.ID))
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.auditAction(r, audit.Entry{Action: "release.create", Target: app.ID, Detail: map[string]any{"release": rel.ID, "commit": commit}})
	unlock = false
	go func() {
		defer s.unlockRelease(app.ID)
		s.publish(context.Background(), app, sb, rel)
	}()
	writeJSON(w, http.StatusAccepted, v1ReleaseFromRow(rel))
}

// v1ListReleases — GET /v1/apps/{id}/releases.
func (s *Server) v1ListReleases(w http.ResponseWriter, r *http.Request) {
	app, sb, ok := s.appAndSandbox(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.ListReleases(r.Context(), app.ID, tenantToken(r))
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := v1Releases{Releases: []v1Release{}, Hosts: []string{}}
	for _, rel := range rows {
		out.Releases = append(out.Releases, v1ReleaseFromRow(rel))
	}
	if sb != nil && sb.Mode == store.ModeProduction {
		out.SandboxID = sb.ID
		out.Hosts = append(out.Hosts, sb.PublicHosts...)
		if len(sb.PublicHosts) > 0 {
			out.URL = s.publicURL(sb.PublicHosts[0])
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// v1GetRelease — GET /v1/apps/{id}/releases/{rid}: one release and the end of its log.
func (s *Server) v1GetRelease(w http.ResponseWriter, r *http.Request) {
	app, sb, ok := s.appAndSandbox(w, r)
	if !ok {
		return
	}
	rel, err := s.Store.GetRelease(r.Context(), app.ID, r.PathValue("rid"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such release")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := v1ReleaseFromRow(rel)
	if sb != nil {
		out.Log = release.Tail(release.Layout{Root: sb.WorkspaceMnt}.LogPath(rel.ID), logTailBytes)
	}
	writeJSON(w, http.StatusOK, out)
}

// v1ActivateRelease — POST /v1/apps/{id}/releases/{rid}/activate: go back to
// (or forward to) a version that was live before.
func (s *Server) v1ActivateRelease(w http.ResponseWriter, r *http.Request) {
	app, sb, ok := s.appAndSandbox(w, r)
	if !ok {
		return
	}
	rel, err := s.Store.GetRelease(r.Context(), app.ID, r.PathValue("rid"), tenantToken(r))
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such release")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if rel.Status == store.ReleaseActive {
		writeJSON(w, http.StatusOK, v1ReleaseFromRow(rel))
		return
	}
	if rel.Status != store.ReleasePrevious || sb == nil || sb.Mode != store.ModeProduction {
		writeV1Err(w, http.StatusConflict, "conflict", "only a version that was live before can be brought back")
		return
	}
	if _, err := os.Stat(release.Layout{Root: sb.WorkspaceMnt}.Dir(rel.ID)); err != nil {
		writeV1Err(w, http.StatusConflict, "conflict", "this version is no longer kept on the server")
		return
	}
	if !s.lockRelease(app.ID) {
		writeV1Err(w, http.StatusConflict, "busy", "this app is already being published; wait for it to finish")
		return
	}
	s.auditAction(r, audit.Entry{Action: "release.activate", Target: app.ID, Detail: map[string]any{"release": rel.ID}})
	if err := s.Store.SetReleaseStatus(r.Context(), rel.ID, store.ReleaseStarting, "Going back to this version"); err != nil {
		s.unlockRelease(app.ID)
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	rel.Status = store.ReleaseStarting
	go func() {
		defer s.unlockRelease(app.ID)
		s.goLive(context.Background(), app, sb, rel, store.ReleasePrevious)
	}()
	writeJSON(w, http.StatusAccepted, v1ReleaseFromRow(rel))
}

// --- the pipeline ---------------------------------------------------------

// createProductionSandbox makes the app's sandbox on this server, through
// the normal create path, in production mode with a public hostname.
func (s *Server) createProductionSandbox(r *http.Request, app *store.App) (*store.Sandbox, error) {
	host, err := s.newPublicHost(r.Context(), app.Name)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{
		"ports":        []int{manifest.DefaultWebPort},
		"app_id":       app.ID,
		"template":     "blank",
		"visibility":   "public",
		"idle_policy":  "always_on",
		"mode":         store.ModeProduction,
		"public_hosts": []string{host},
		"external": map[string]string{
			"user_id":    app.ExternalUserID.String,
			"project_id": app.ExternalProjectID.String,
		},
	})
	code, resp := s.delegate(r, s.handleCreate, "POST", "/sandbox", nil, body)
	if code/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(resp, &e)
		return nil, fmt.Errorf("HTTP %d: %s", code, e.Error)
	}
	return s.Store.CurrentSandboxForApp(r.Context(), app.ID)
}

var reHostLabel = regexp.MustCompile(`[^a-z0-9]+`)

// reservedHosts are first labels sandboxd itself uses under the domain.
var reservedHosts = map[string]bool{"api": true, "console": true, "preview": true, "www": true, "traefik": true}

// newPublicHost picks <name>.<domain> for a new published app, unique on this server.
func (s *Server) newPublicHost(ctx context.Context, name string) (string, error) {
	base := strings.Trim(reHostLabel.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(base) > 40 {
		base = strings.Trim(base[:40], "-")
	}
	if base == "" || reservedHosts[base] || strings.HasPrefix(base, "s-") {
		base = "app-" + base
		base = strings.Trim(base, "-")
	}
	domain := s.PreviewDomain
	if domain == "" {
		domain = "localhost"
	}
	for i := 1; i < 1000; i++ {
		label := base
		if i > 1 {
			label = fmt.Sprintf("%s-%d", base, i)
		}
		host := label + "." + domain
		used, err := s.Store.PublicHostInUse(ctx, host, "")
		if err != nil {
			return "", err
		}
		if !used {
			return host, nil
		}
	}
	return "", errors.New("no free address for this app")
}

// publicURL is the browser URL of a published app's hostname.
func (s *Server) publicURL(host string) string {
	scheme, defaultPort := s.previewScheme()
	if s.PreviewURLScheme != "" {
		defaultPort = s.PublicHTTPPort
	}
	if p := s.PublicHTTPPort; p != "" && p != defaultPort {
		host += ":" + p
	}
	return scheme + "://" + host
}

// releaseLog appends a line to a release's log.
func releaseLog(l release.Layout, id, format string, a ...any) {
	f, err := os.OpenFile(l.LogPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[sandboxd] "+format+"\n", a...)
}

// publish builds a freshly unpacked release and makes it live.
func (s *Server) publish(ctx context.Context, app *store.App, sb *store.Sandbox, rel *store.Release) {
	l := release.Layout{Root: sb.WorkspaceMnt}
	// Create the log before handing the files to the sandbox user, so the
	// build container can append to it.
	releaseLog(l, rel.ID, "received version %s", rel.Commit)
	fail := func(msg string) {
		releaseLog(l, rel.ID, "%s", msg)
		_ = os.RemoveAll(l.Dir(rel.ID))
		if err := s.Store.SetReleaseStatus(ctx, rel.ID, store.ReleaseFailed, msg); err != nil {
			s.Log.Warn("release: record failure", "release", rel.ID, "err", err.Error())
		}
	}
	raw, _ := os.ReadFile(filepath.Join(l.Dir(rel.ID), manifest.File))
	m, err := manifest.Parse(raw)
	if err != nil || m == nil {
		m = &manifest.Manifest{}
		if len(raw) > 0 {
			fail("sandbox.yaml could not be read: " + oneLineErr(err))
			return
		}
	}
	if res := manifest.Validate(raw); len(raw) > 0 && !res.Valid {
		fail("sandbox.yaml has problems: " + strings.Join(res.Errors, "; "))
		return
	}
	start, build := m.StartCommand(), m.ReleaseCommand()
	if start == "" && m.Web != nil {
		if pStart, pBuild, ok := preset.Production(m.Web.Command); ok {
			start = pStart
			if build == "" {
				build = pBuild
			}
		}
	}
	if m.Web != nil && start == "" {
		fail("This app does not say how to start in production. Ask your agent: \"add web.start to sandbox.yaml\", then publish again.")
		return
	}
	if p := m.WebPort(); p != 0 && sb.WebPort.Valid && int(sb.WebPort.Int64) != p {
		fail(fmt.Sprintf("This version listens on port %d, but the published app uses port %d. Set web.port back to %d in sandbox.yaml.", p, sb.WebPort.Int64, sb.WebPort.Int64))
		return
	}
	if err := release.LinkData(l, rel.ID, m.DataPaths()); err != nil {
		fail("could not keep the app's data: " + err.Error())
		return
	}
	if s.Loopback != nil {
		for _, dir := range []string{l.Dir(rel.ID), l.DataDir(), filepath.Dir(l.LogPath(rel.ID))} {
			if err := s.Loopback.NormalizeOwnership(dir); err != nil {
				fail("could not prepare the files: " + err.Error())
				return
			}
		}
	}
	if build != "" {
		releaseLog(l, rel.ID, "building: %s", build)
		if code, err := s.runReleaseBuild(ctx, sb, l, rel.ID, build); err != nil || code != 0 {
			msg := fmt.Sprintf("The build failed (exit code %d). The live version is still running; see the log for details.", code)
			if err != nil {
				msg = "The build could not run: " + err.Error() + ". The live version is still running."
			}
			fail(msg)
			return
		}
	}
	if err := s.Store.SetReleaseStatus(ctx, rel.ID, store.ReleaseStarting, "Starting the new version"); err != nil {
		fail(err.Error())
		return
	}
	s.goLive(ctx, app, sb, rel, store.ReleaseFailed)
}

// goLive switches the app to rel, restarts it and waits for it to answer. If
// it does not, the previous release is put back and rel gets failStatus
// (failed for a new release, previous for a go-back).
func (s *Server) goLive(ctx context.Context, app *store.App, sb *store.Sandbox, rel *store.Release, failStatus string) {
	l := release.Layout{Root: sb.WorkspaceMnt}
	before := release.Active(l)
	if err := release.Switch(l, rel.ID); err != nil {
		_ = s.Store.SetReleaseStatus(ctx, rel.ID, failStatus, "could not switch to this version: "+err.Error())
		return
	}
	releaseLog(l, rel.ID, "starting the new version")
	err := s.restartProduction(ctx, sb)
	if err == nil {
		err = s.waitProductionReady(ctx, sb)
	}
	if err != nil {
		releaseLog(l, rel.ID, "%s", err.Error())
		if web := release.Tail(filepath.Join(sb.WorkspaceMnt, ".runtimed", "web.log"), 4<<10); web != "" {
			releaseLog(l, rel.ID, "end of the app's output:\n%s", web)
		}
		msg := "The new version did not start, so nothing changed for your visitors."
		if before != "" && before != rel.ID {
			if serr := release.Switch(l, before); serr == nil {
				_ = s.restartProduction(ctx, sb)
				msg = "The new version did not start, so the previous version was put back."
			}
		}
		if failStatus == store.ReleaseFailed {
			_ = os.RemoveAll(l.Dir(rel.ID))
		}
		_ = s.Store.SetReleaseStatus(ctx, rel.ID, failStatus, msg)
		return
	}
	if err := s.Store.ActivateRelease(ctx, app.ID, rel.ID); err != nil {
		s.Log.Warn("release: activate", "release", rel.ID, "err", err.Error())
	}
	releaseLog(l, rel.ID, "live")
	s.pruneReleases(ctx, app, l)
}

// runReleaseBuild runs build.release for a release in a one-off container
// with the same image and protections as the app, logging to the release log.
func (s *Server) runReleaseBuild(ctx context.Context, sb *store.Sandbox, l release.Layout, id, command string) (int, error) {
	if s.Docker == nil {
		return -1, errors.New("Docker is not available")
	}
	ctx, cancel := context.WithTimeout(ctx, releaseBuildTimeout)
	defer cancel()
	name := "r-" + id
	script := fmt.Sprintf("cd %s && ( %s ) >> /home/sandbox/releases/.logs/%s.log 2>&1", l.ContainerDir(id), command, id)
	volumes := []string{sb.WorkspaceMnt + ":/home/sandbox"}
	if s.DNSResolvConf != "" {
		volumes = append(volumes, s.DNSResolvConf+":/etc/resolv.conf:ro")
	}
	_ = s.Docker.Remove(ctx, name)
	if _, err := s.Docker.Run(ctx, docker.RunSpec{
		Name:        name,
		Hostname:    name,
		Network:     s.Network,
		Userns:      s.Userns,
		Runtime:     s.Runtime,
		ReadOnly:    true,
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges"},
		CPUShares:   100,
		Memory:      "4g",
		MemorySwap:  "4g",
		PidsLimit:   1024,
		Ulimits:     []string{"nofile=65536:65536"},
		Tmpfs:       []string{"/tmp:size=1g", "/var/tmp:size=128m"},
		Env:         []string{"CI=true"},
		Volumes:     volumes,
		Labels:      []string{"sandboxd.release=" + id, "sandboxd.app=" + sb.AppID.String},
		Image:       s.Image,
		Cmd:         []string{"bash", "-lc", script},
	}); err != nil {
		return -1, err
	}
	defer func() { _ = s.Docker.Remove(context.Background(), name) }()
	for {
		cj, err := s.Docker.Inspect(ctx, name)
		if err != nil {
			return -1, err
		}
		if !cj.State.Running && cj.State.Status != "created" {
			return cj.State.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return -1, errors.New("it took longer than 20 minutes")
		case <-time.After(releasePoll):
		}
	}
}

// restartProduction restarts the app's container (recreating it if it is gone).
func (s *Server) restartProduction(ctx context.Context, sb *store.Sandbox) error {
	if s.Docker == nil {
		return errors.New("Docker is not available")
	}
	name := "s-" + sb.ID
	if _, err := s.Docker.Inspect(ctx, name); errors.Is(err, docker.ErrNotFound) {
		if s.Recreate == nil {
			return errors.New("the app's container is missing")
		}
		return s.Recreate(ctx, sb)
	}
	if err := s.Docker.Stop(ctx, name, 20); err != nil && !errors.Is(err, docker.ErrNotFound) {
		return err
	}
	return s.Docker.Start(ctx, name)
}

// waitProductionReady waits for runtimed to report the app up: the web
// process answering its health check, or (worker-only) every process running.
func (s *Server) waitProductionReady(ctx context.Context, sb *store.Sandbox) error {
	ctx, cancel := context.WithTimeout(ctx, releaseStartTimeout)
	defer cancel()
	client := runtime.NewClient(filepath.Join(sb.WorkspaceMnt, ".runtimed", "sock"))
	bootedAfter := time.Now().Add(-2 * time.Second)
	for {
		if st, err := client.Status(ctx); err == nil && st.Runtimed.BootedAt.After(bootedAfter) {
			switch st.Preview.Status {
			case runtime.PreviewReady:
				return nil
			case runtime.PreviewNone:
				up := len(st.Processes) > 0
				for _, p := range st.Processes {
					up = up && p.Running
				}
				if up {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("the app did not answer within 5 minutes")
		case <-time.After(releasePoll):
		}
	}
}

// pruneReleases keeps the active release and the last previous ones.
func (s *Server) pruneReleases(ctx context.Context, app *store.App, l release.Layout) {
	rows, err := s.Store.ListReleases(ctx, app.ID, app.OwnerToken)
	if err != nil {
		return
	}
	keep := map[string]bool{}
	previous := 0
	failed := 0
	for _, r := range rows { // newest first
		switch r.Status {
		case store.ReleaseActive, store.ReleaseBuilding, store.ReleaseStarting:
			keep[r.ID] = true
		case store.ReleasePrevious:
			if previous < keepPreviousRelease {
				keep[r.ID] = true
				previous++
			} else {
				_ = s.Store.DeleteRelease(ctx, r.ID)
			}
		case store.ReleaseFailed:
			if failed++; failed > 10 {
				_ = s.Store.DeleteRelease(ctx, r.ID)
			} else {
				keep[r.ID] = true // its log stays readable; its folder is already gone
			}
		}
	}
	if err := release.Prune(l, keep); err != nil {
		s.Log.Warn("release: prune", "app", app.ID, "err", err.Error())
	}
}

func oneLineErr(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), "\n", " ")
}

// clipEnd keeps the last n bytes of s.
func clipEnd(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
