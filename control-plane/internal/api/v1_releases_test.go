package api

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/auth"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/docker"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/release"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/runtime"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// The test binary doubles as a fake docker CLI: with SANDBOXD_FAKE_DOCKER
// set to a state dir, it handles run/inspect/start/stop/rm. A release build
// container (r-*) really runs its script, with /home/sandbox mapped to the
// workspace on the host.
func TestMain(m *testing.M) {
	if dir := os.Getenv("SANDBOXD_FAKE_DOCKER"); dir != "" {
		os.Exit(fakeDockerMain(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

const fakeImage = "sandboxd/test-image"

func fakeDockerMain(dir string, args []string) int {
	logCall := func() {
		f, _ := os.OpenFile(filepath.Join(dir, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		fmt.Fprintln(f, strings.Join(args, " "))
		f.Close()
	}
	logCall()
	if len(args) == 0 {
		return 2
	}
	name := args[len(args)-1]
	switch args[0] {
	case "run":
		var mnt string
		for i, a := range args {
			if a == "--name" {
				name = args[i+1]
			}
			if a == "-v" && strings.HasSuffix(args[i+1], ":/home/sandbox") {
				mnt = strings.TrimSuffix(args[i+1], ":/home/sandbox")
			}
		}
		for i, a := range args {
			if a == fakeImage && strings.HasPrefix(name, "r-") {
				script := strings.ReplaceAll(args[i+3], "/home/sandbox", mnt)
				code := 0
				if err := exec.Command("bash", "-c", script).Run(); err != nil {
					code = 1
					if ee, ok := err.(*exec.ExitError); ok {
						code = ee.ExitCode()
					}
				}
				_ = os.WriteFile(filepath.Join(dir, name+".exit"), []byte(fmt.Sprint(code)), 0o644)
			}
		}
		fmt.Println("0123456789abcdef")
	case "inspect":
		if strings.HasPrefix(name, "r-") {
			b, err := os.ReadFile(filepath.Join(dir, name+".exit"))
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error: No such object: "+name)
				return 1
			}
			fmt.Printf(`{"Id":"x","State":{"Status":"exited","Running":false,"ExitCode":%s}}`+"\n", b)
			return 0
		}
		fmt.Println(`{"Id":"x","State":{"Status":"running","Running":true,"Pid":1}}`)
	case "start", "stop", "rm":
	default:
		fmt.Fprintln(os.Stderr, "fake docker: unsupported", args[0])
		return 1
	}
	return 0
}

// fakeReleaseRuntimed answers /status on the workspace's runtimed socket: ready,
// unless the active release ships a file named "broken".
func fakeReleaseRuntimed(t *testing.T, l release.Layout) {
	t.Helper()
	sock := filepath.Join(l.Root, ".runtimed", "sock")
	if err := os.MkdirAll(filepath.Dir(sock), 0o755); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := runtime.Status{Runtimed: runtime.RuntimedInfo{BootedAt: time.Now()}}
		st.Preview.Status = runtime.PreviewReady
		if _, err := os.Stat(filepath.Join(l.AppLink(), "broken")); err == nil {
			st.Preview.Status = runtime.PreviewError
		}
		_ = json.NewEncoder(w).Encode(st)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

const goodManifest = `version: 1
web:
  command: "pnpm dev"
  start: "node server.js"
  port: 3000
build:
  release: "echo built > built.txt"
data: [data]
`

type releaseEnv struct {
	s     *Server
	st    *store.Store
	app   *store.App
	sb    *store.Sandbox
	l     release.Layout
	state string
}

func newReleaseEnv(t *testing.T) *releaseEnv {
	t.Helper()
	releasePoll = 10 * time.Millisecond
	t.Cleanup(func() { releasePoll = 2 * time.Second; releaseStartTimeout = defaultReleaseStartTimeout })
	st, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "db.sqlite")+"?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	state := t.TempDir()
	t.Setenv("SANDBOXD_FAKE_DOCKER", state)
	self, _ := os.Executable()
	s := &Server{Store: st, Docker: &docker.Client{Bin: self}, Image: fakeImage, PreviewDomain: "1-2-3-4.sslip.io",
		PreviewURLScheme: "https", Log: testLogger()}
	ctx := context.Background()
	app := &store.App{ID: newULID(), OwnerToken: cfgTenant, Name: "My Shop"}
	if err := st.CreateApp(ctx, app); err != nil {
		t.Fatal(err)
	}
	// /tmp-based so the runtimed socket path stays short.
	root, err := os.MkdirTemp("", "rel")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	sb := &store.Sandbox{ID: newULID(), Status: "running", Image: fakeImage, WorkspaceMnt: root, MemoryHigh: "4G",
		Ports: []int{3000}, WebPort: sql.NullInt64{Int64: 3000, Valid: true}, Visibility: "public", IdlePolicy: "always_on",
		AppID: sql.NullString{String: app.ID, Valid: true}, Mode: store.ModeProduction, PublicHosts: []string{"my-shop.1-2-3-4.sslip.io"}}
	if err := st.Create(ctx, sb); err != nil {
		t.Fatal(err)
	}
	l := release.Layout{Root: root}
	_ = os.MkdirAll(l.AppLink(), 0o755)
	fakeReleaseRuntimed(t, l)
	return &releaseEnv{s: s, st: st, app: app, sb: sb, l: l, state: state}
}

func (e *releaseEnv) call(t *testing.T, method, path string, body []byte, h http.HandlerFunc, pv map[string]string) (int, []byte) {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r = r.WithContext(auth.WithActor(r.Context(), auth.Actor{Name: cfgTenant, Kind: "service"}))
	r.SetPathValue("id", e.app.ID)
	for k, v := range pv {
		r.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w.Code, w.Body.Bytes()
}

// publish sends a bundle and waits for the release to finish.
func (e *releaseEnv) publish(t *testing.T, files map[string]string) v1Release {
	t.Helper()
	code, body := e.call(t, "POST", "/v1/apps/x/releases?commit=abc1234", tgz(t, files), e.s.v1CreateRelease, nil)
	if code != http.StatusAccepted {
		t.Fatalf("publish: HTTP %d %s", code, body)
	}
	var rel v1Release
	_ = json.Unmarshal(body, &rel)
	return e.wait(t, rel.ID)
}

func (e *releaseEnv) wait(t *testing.T, id string) v1Release {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		_, body := e.call(t, "GET", "/", nil, e.s.v1GetRelease, map[string]string{"rid": id})
		var rel v1Release
		_ = json.Unmarshal(body, &rel)
		if rel.Status != store.ReleaseBuilding && rel.Status != store.ReleaseStarting {
			e.s.releaseMu.Lock()
			busy := e.s.releasing[e.app.ID]
			e.s.releaseMu.Unlock()
			if !busy {
				return rel
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("release %s did not finish", id)
	return v1Release{}
}

func TestPublishGoLiveRollBackAndGoBack(t *testing.T) {
	e := newReleaseEnv(t)

	r1 := e.publish(t, map[string]string{"sandbox.yaml": goodManifest, "server.js": "v1", "data/seed.json": "seed"})
	if r1.Status != store.ReleaseActive || r1.Commit != "abc1234" {
		t.Fatalf("first release: %+v", r1)
	}
	if release.Active(e.l) != r1.ID {
		t.Fatalf("app not switched: %q", release.Active(e.l))
	}
	if b, _ := os.ReadFile(filepath.Join(e.l.Dir(r1.ID), "built.txt")); strings.TrimSpace(string(b)) != "built" {
		t.Error("build.release did not run in the release folder")
	}
	if b, _ := os.ReadFile(filepath.Join(e.l.AppLink(), "data", "seed.json")); string(b) != "seed" {
		t.Error("data not linked")
	}
	calls, _ := os.ReadFile(filepath.Join(e.state, "calls"))
	if !strings.Contains(string(calls), "stop --time 20 s-"+e.sb.ID) || !strings.Contains(string(calls), "start s-"+e.sb.ID) {
		t.Errorf("the app was not restarted:\n%s", calls)
	}
	if !strings.Contains(string(calls), "--read-only") || !strings.Contains(string(calls), "--cap-drop=ALL") {
		t.Errorf("the build container is not locked down:\n%s", calls)
	}

	// A version that does not start is rolled back.
	releaseStartTimeout = 300 * time.Millisecond
	r2 := e.publish(t, map[string]string{"sandbox.yaml": goodManifest, "server.js": "v2", "broken": ""})
	releaseStartTimeout = defaultReleaseStartTimeout
	if r2.Status != store.ReleaseFailed || !strings.Contains(r2.Message, "previous version was put back") {
		t.Fatalf("broken release: %+v", r2)
	}
	if release.Active(e.l) != r1.ID {
		t.Errorf("rollback: active %q", release.Active(e.l))
	}
	if !strings.Contains(r2.Log, "did not answer") {
		t.Errorf("the log should say why: %q", r2.Log)
	}

	// A failing build never touches the live version.
	r3 := e.publish(t, map[string]string{"sandbox.yaml": strings.Replace(goodManifest, "echo built > built.txt", "exit 3", 1)})
	if r3.Status != store.ReleaseFailed || !strings.Contains(r3.Message, "exit code 3") || release.Active(e.l) != r1.ID {
		t.Fatalf("failed build: %+v active=%s", r3, release.Active(e.l))
	}
	if _, err := os.Stat(e.l.Dir(r3.ID)); err == nil {
		t.Error("a failed release folder was kept")
	}

	// No production start: a plain-language failure.
	r4 := e.publish(t, map[string]string{"sandbox.yaml": "version: 1\nweb:\n  command: python app.py\n  port: 3000\n"})
	if r4.Status != store.ReleaseFailed || !strings.Contains(r4.Message, "web.start") {
		t.Fatalf("missing start: %+v", r4)
	}

	// Another port than the published one is refused.
	r5 := e.publish(t, map[string]string{"sandbox.yaml": strings.Replace(goodManifest, "port: 3000", "port: 8000", 1)})
	if r5.Status != store.ReleaseFailed || !strings.Contains(r5.Message, "port 8000") {
		t.Fatalf("port change: %+v", r5)
	}

	// A good second version, then go back to the first.
	r6 := e.publish(t, map[string]string{"sandbox.yaml": goodManifest, "server.js": "v6"})
	if r6.Status != store.ReleaseActive {
		t.Fatalf("r6: %+v", r6)
	}
	code, body := e.call(t, "POST", "/", nil, e.s.v1ActivateRelease, map[string]string{"rid": r1.ID})
	if code != http.StatusAccepted {
		t.Fatalf("go back: HTTP %d %s", code, body)
	}
	if back := e.wait(t, r1.ID); back.Status != store.ReleaseActive || release.Active(e.l) != r1.ID {
		t.Fatalf("go back: %+v active=%s", back, release.Active(e.l))
	}
	if code, _ := e.call(t, "POST", "/", nil, e.s.v1ActivateRelease, map[string]string{"rid": r3.ID}); code != http.StatusConflict {
		t.Errorf("activating a failed release: HTTP %d", code)
	}

	_, body = e.call(t, "GET", "/", nil, e.s.v1ListReleases, nil)
	var list v1Releases
	_ = json.Unmarshal(body, &list)
	if list.URL != "https://my-shop.1-2-3-4.sslip.io" || len(list.Releases) != 6 {
		t.Errorf("list: url=%q %d releases", list.URL, len(list.Releases))
	}
	actives := 0
	for _, r := range list.Releases {
		if r.Active {
			actives++
		}
	}
	if actives != 1 {
		t.Errorf("%d active releases", actives)
	}
}

func TestOnlyTheLastThreeVersionsAreKept(t *testing.T) {
	e := newReleaseEnv(t)
	var ids []string
	for i := 0; i < 5; i++ {
		r := e.publish(t, map[string]string{"sandbox.yaml": goodManifest, "server.js": fmt.Sprint(i)})
		if r.Status != store.ReleaseActive {
			t.Fatalf("release %d: %+v", i, r)
		}
		ids = append(ids, r.ID)
	}
	for i, id := range ids {
		_, err := os.Stat(e.l.Dir(id))
		if kept := err == nil; kept != (i >= 2) {
			t.Errorf("release %d kept=%v", i, kept)
		}
	}
	rows, _ := e.st.ListReleases(context.Background(), e.app.ID, cfgTenant)
	if len(rows) != 3 {
		t.Errorf("%d release rows; want 3", len(rows))
	}
}

func TestPublishRefusals(t *testing.T) {
	e := newReleaseEnv(t)
	if code, _ := e.call(t, "POST", "/v1/apps/x/releases?commit=nothex!", tgz(t, nil), e.s.v1CreateRelease, nil); code != http.StatusBadRequest {
		t.Errorf("bad commit: HTTP %d", code)
	}
	if code, _ := e.call(t, "POST", "/v1/apps/x/releases", []byte("not a bundle"), e.s.v1CreateRelease, nil); code != http.StatusBadRequest {
		t.Errorf("garbage body: HTTP %d", code)
	}
	if !e.s.lockRelease(e.app.ID) {
		t.Fatal("lock")
	}
	if code, _ := e.call(t, "POST", "/v1/apps/x/releases", tgz(t, map[string]string{"a": "b"}), e.s.v1CreateRelease, nil); code != http.StatusConflict {
		t.Errorf("concurrent publish: HTTP %d", code)
	}
	e.s.unlockRelease(e.app.ID)

	// An app being built on this engine is not a publish target.
	dev := &store.App{ID: newULID(), OwnerToken: cfgTenant, Name: "dev"}
	_ = e.st.CreateApp(context.Background(), dev)
	_ = e.st.Create(context.Background(), &store.Sandbox{ID: newULID(), Status: "running", Image: fakeImage, WorkspaceMnt: t.TempDir(),
		MemoryHigh: "4G", AppID: sql.NullString{String: dev.ID, Valid: true}})
	r := httptest.NewRequest("POST", "/", bytes.NewReader(tgz(t, nil)))
	r = r.WithContext(auth.WithActor(r.Context(), auth.Actor{Name: cfgTenant, Kind: "service"}))
	r.SetPathValue("id", dev.ID)
	w := httptest.NewRecorder()
	e.s.v1CreateRelease(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("publishing onto a dev app: HTTP %d", w.Code)
	}
}

func TestNewPublicHost(t *testing.T) {
	e := newReleaseEnv(t)
	ctx := context.Background()
	for name, want := range map[string]string{
		"Café Menu!":               "caf-menu.1-2-3-4.sslip.io",
		"api":                      "app-api.1-2-3-4.sslip.io",
		"!!!":                      "app.1-2-3-4.sslip.io",
		"My Shop":                  "my-shop-2.1-2-3-4.sslip.io", // my-shop is taken by the env's app
		strings.Repeat("long", 20): strings.Repeat("long", 10) + ".1-2-3-4.sslip.io",
	} {
		got, err := e.s.newPublicHost(ctx, name)
		if err != nil || got != want {
			t.Errorf("%q: %q %v; want %q", name, got, err, want)
		}
	}
}

func TestBundleIsTheSavedCodeWithoutAgentNotes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	st, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "db.sqlite")+"?_fk=1", "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{Store: st, Log: testLogger()}
	ctx := context.Background()
	app := &store.App{ID: newULID(), OwnerToken: cfgTenant, Name: "notes"}
	_ = st.CreateApp(ctx, app)
	mnt := t.TempDir()
	dir := filepath.Join(mnt, "workspace", "app")
	_ = os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	_ = st.Create(ctx, &store.Sandbox{ID: newULID(), Status: "running", Image: "img", WorkspaceMnt: mnt, MemoryHigh: "4G",
		AppID: sql.NullString{String: app.ID, Valid: true}})

	get := func() (int, http.Header, []byte) {
		r := httptest.NewRequest("GET", "/", nil)
		r = r.WithContext(auth.WithActor(r.Context(), auth.Actor{Name: cfgTenant, Kind: "service"}))
		r.SetPathValue("id", app.ID)
		w := httptest.NewRecorder()
		s.v1AppBundle(w, r)
		return w.Code, w.Header(), w.Body.Bytes()
	}
	if code, _, _ := get(); code != http.StatusConflict {
		t.Errorf("no commit yet: HTTP %d", code)
	}
	for name, body := range map[string]string{"src/index.js": "x", "BRAIN.md": "notes", ".env": "SECRET=1", ".opencode/a": "b", "sandbox.yaml": "version: 1"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", "-A")
	git("commit", "-qm", "v1")
	_ = os.WriteFile(filepath.Join(dir, "src", "index.js"), []byte("unsaved"), 0o644)

	code, h, body := get()
	if code != http.StatusOK || len(h.Get("X-Sandboxd-Commit")) != 40 || h.Get("X-Sandboxd-Uncommitted-Changes") != "1" {
		t.Fatalf("bundle: HTTP %d %v", code, h)
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	got := map[string]string{}
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hd.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			got[hd.Name] = string(b)
		}
	}
	if got["src/index.js"] != "x" || got["sandbox.yaml"] == "" {
		t.Errorf("bundle content: %v", got)
	}
	for _, left := range []string{"BRAIN.md", ".env", ".opencode/a"} {
		if _, ok := got[left]; ok {
			t.Errorf("%s must not be published", left)
		}
	}
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
