package api

import (
	"archive/zip"
	"bytes"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/loopback"
)

// planted secret bytes we assert never leak through the file API.
const secretMarker = "SANDBOXD_API_TOKENS=default=sk_TOPSECRET"

// fileSymlinkServer builds a Server backed by a temp workspace, plants a leaf
// symlink and an intermediate-directory symlink that escape the app dir, and a
// normal file that must still be readable.
func fileSymlinkServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	lm := &loopback.Manager{Root: root}
	s := &Server{Loopback: lm, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	id := newULID()
	_, mnt := lm.Paths(id)
	appDir := filepath.Join(mnt, appSubdir)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A secret OUTSIDE the workspace (stands in for /proc/self/environ etc.).
	outside := t.TempDir()
	secret := filepath.Join(outside, "environ")
	if err := os.WriteFile(secret, []byte(secretMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	// Leaf symlink: app/leak -> outside/environ
	if err := os.Symlink(secret, filepath.Join(appDir, "leak")); err != nil {
		t.Fatal(err)
	}
	// Intermediate symlink: app/escape -> outside/  (then request escape/environ)
	if err := os.Symlink(outside, filepath.Join(appDir, "escape")); err != nil {
		t.Fatal(err)
	}
	// A normal file that MUST still read fine.
	if err := os.WriteFile(filepath.Join(appDir, "ok.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return s, id
}

func getContent(t *testing.T, s *Server, id, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/v1/sandboxes/"+id+"/files/content?path="+url.QueryEscape(path), nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.v1FileContent(w, r)
	return w
}

func TestFileContentDoesNotFollowSymlinks(t *testing.T) {
	s, id := fileSymlinkServer(t)

	// leaf symlink escape must NOT leak the secret
	if w := getContent(t, s, id, "leak"); w.Code == 200 && strings.Contains(w.Body.String(), secretMarker) {
		t.Fatalf("LEAK via leaf symlink: code=%d body=%q", w.Code, w.Body.String())
	}
	// intermediate-component symlink escape must NOT leak either
	if w := getContent(t, s, id, "escape/environ"); w.Code == 200 && strings.Contains(w.Body.String(), secretMarker) {
		t.Fatalf("LEAK via intermediate symlink: code=%d body=%q", w.Code, w.Body.String())
	}
	// a normal in-workspace file must still read
	if w := getContent(t, s, id, "ok.txt"); w.Code != 200 || w.Body.String() != "hello" {
		t.Fatalf("normal read broken: code=%d body=%q", w.Code, w.Body.String())
	}
}

func TestExportDoesNotFollowSymlinks(t *testing.T) {
	s, id := fileSymlinkServer(t)
	r := httptest.NewRequest("GET", "/v1/sandboxes/"+id+"/export", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.v1Export(w, r)
	if w.Code != 200 {
		t.Fatalf("export code=%d", w.Code)
	}
	body := w.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var sawOK bool
	for _, f := range zr.File {
		if f.Name == "ok.txt" {
			sawOK = true
		}
		if f.Name == "leak" {
			t.Fatal("LEAK: export included the symlink entry 'leak'")
		}
		rc, err := f.Open() // DECOMPRESS and inspect the actual content
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(rc)
		rc.Close()
		if bytes.Contains(content, []byte(secretMarker)) {
			t.Fatalf("LEAK: export entry %q contains the symlink target's secret", f.Name)
		}
	}
	if !sawOK {
		t.Fatal("export dropped the normal file ok.txt")
	}
}

func TestListDoesNotFollowSymlinkedDir(t *testing.T) {
	s, id := fileSymlinkServer(t)
	// Both recursive and non-recursive listing INTO the symlinked dir must not
	// expose the outside file. (Non-recursive os.ReadDir follows a symlinked dir;
	// recursive WalkDir does not descend it — cover both.)
	for _, rec := range []string{"true", "false"} {
		r := httptest.NewRequest("GET", "/v1/sandboxes/"+id+"/files?path=escape&recursive="+rec, nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.v1ListFiles(w, r)
		if w.Code == 200 && strings.Contains(w.Body.String(), "environ") {
			t.Fatalf("LEAK: list (recursive=%s) followed the symlinked dir: %s", rec, w.Body.String())
		}
	}
}
