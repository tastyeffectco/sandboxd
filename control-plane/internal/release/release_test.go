package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type entry struct {
	name, body, link string
	typ              byte
	mode             int64
}

func bundle(t *testing.T, entries ...entry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	// git archive starts with a pax global header carrying the commit.
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header",
		PAXRecords: map[string]string{"comment": "0123abc"}}); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: e.mode, Linkname: e.link, Size: int64(len(e.body))}
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = zw.Close()
	return &buf
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUnpack(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "r1")
	b := bundle(t,
		entry{name: "src/", typ: tar.TypeDir},
		entry{name: "src/index.js", body: "hi"},
		entry{name: "run.sh", body: "#!/bin/sh", mode: 0o4755},
		entry{name: "src/link.js", typ: tar.TypeSymlink, link: "index.js"},
		entry{name: "dev", typ: tar.TypeChar},
	)
	if err := Unpack(b, dest, 1<<20); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dest, "src/link.js")) != "hi" {
		t.Error("link not followed")
	}
	fi, _ := os.Stat(filepath.Join(dest, "run.sh"))
	if fi.Mode().Perm() != 0o755 || fi.Mode()&os.ModeSetuid != 0 {
		t.Errorf("mode %v", fi.Mode())
	}
	if _, err := os.Lstat(filepath.Join(dest, "dev")); err == nil {
		t.Error("a device was created")
	}
}

func TestUnpackRefusesEscapes(t *testing.T) {
	for name, b := range map[string]func(t *testing.T) *bytes.Buffer{
		"dotdot":   func(t *testing.T) *bytes.Buffer { return bundle(t, entry{name: "../evil", body: "x"}) },
		"absolute": func(t *testing.T) *bytes.Buffer { return bundle(t, entry{name: "/etc/evil", body: "x"}) },
		"nested":   func(t *testing.T) *bytes.Buffer { return bundle(t, entry{name: "a/../../evil", body: "x"}) },
		"link out": func(t *testing.T) *bytes.Buffer {
			return bundle(t, entry{name: "l", typ: tar.TypeSymlink, link: "../../etc"})
		},
		"link abs": func(t *testing.T) *bytes.Buffer {
			return bundle(t, entry{name: "l", typ: tar.TypeSymlink, link: "/etc"})
		},
		"through link": func(t *testing.T) *bytes.Buffer {
			return bundle(t, entry{name: "l", typ: tar.TypeSymlink, link: "."}, entry{name: "l/../../evil", body: "x"})
		},
	} {
		root := t.TempDir()
		err := Unpack(b(t), filepath.Join(root, "r"), 1<<20)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, serr := os.Stat(filepath.Join(root, "evil")); serr == nil {
			t.Errorf("%s: wrote outside the release", name)
		}
	}
	if err := Unpack(bundle(t, entry{name: "big", body: "0123456789"}), filepath.Join(t.TempDir(), "r"), 5); !errors.Is(err, ErrTooLarge) {
		t.Errorf("size limit: %v", err)
	}
}

func TestDataSurvivesReleases(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	paths := []string{"data", "prisma/dev.db", "uploads"}
	// Release 1 ships a seed file in data/ and nothing else.
	if err := Unpack(bundle(t, entry{name: "data/seed.json", body: "v1"}, entry{name: "app.js", body: "1"}), l.Dir("r1"), 1<<20); err != nil {
		t.Fatal(err)
	}
	if err := LinkData(l, "r1", paths); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(l.DataDir(), "data/seed.json")) != "v1" {
		t.Error("first release did not seed appdata")
	}
	// The app writes data through the links.
	if err := os.WriteFile(filepath.Join(l.Dir("r1"), "data", "users.db"), []byte("alice"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.Dir("r1"), "prisma", "dev.db"), []byte("rows"), 0o644); err != nil {
		t.Fatalf("a file data path must be creatable through its link: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(l.DataDir(), "uploads")); err != nil || !fi.IsDir() {
		t.Error("an extension-less data path is a folder")
	}
	// Release 2 ships a different seed: existing data wins.
	if err := Unpack(bundle(t, entry{name: "data/seed.json", body: "v2"}), l.Dir("r2"), 1<<20); err != nil {
		t.Fatal(err)
	}
	if err := LinkData(l, "r2", paths); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(l.Dir("r2"), "data", "users.db")); got != "alice" {
		t.Errorf("data lost across releases: %q", got)
	}
	if got := read(t, filepath.Join(l.Dir("r2"), "data", "seed.json")); got != "v1" {
		t.Errorf("a release overwrote data: %q", got)
	}
	if got := read(t, filepath.Join(l.Dir("r2"), "prisma", "dev.db")); got != "rows" {
		t.Errorf("file data lost: %q", got)
	}
	link, _ := os.Readlink(filepath.Join(l.Dir("r2"), "prisma", "dev.db"))
	if filepath.IsAbs(link) {
		t.Errorf("links must be relative (host and container paths differ): %q", link)
	}
}

func TestDataPathThroughAShippedLinkIsRefused(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(l.Root, "outside"), 0o755); err != nil {
		t.Fatal(err)
	}
	// "data" in the bundle is a link inside the release, to ".": fine for Unpack,
	// but data/x must not be resolved through it to somewhere unexpected.
	if err := Unpack(bundle(t, entry{name: "data", typ: tar.TypeSymlink, link: "."}), l.Dir("r1"), 1<<20); err != nil {
		t.Fatal(err)
	}
	if err := LinkData(l, "r1", []string{"data"}); err != nil {
		t.Fatal(err) // the shipped link itself is replaced by the data link
	}
	if fi, _ := os.Lstat(filepath.Join(l.Dir("r1"), "data")); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("data is not a link")
	}
}

func TestSwitchAndPrune(t *testing.T) {
	l := Layout{Root: t.TempDir()}
	// A new production sandbox has an empty app folder.
	if err := os.MkdirAll(l.AppLink(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"r1", "r2", "r3"} {
		if err := Unpack(bundle(t, entry{name: "v", body: id}), l.Dir(id), 1<<20); err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(filepath.Dir(l.LogPath(id)), 0o755)
		_ = os.WriteFile(l.LogPath(id), []byte("log"), 0o644)
	}
	if Active(l) != "" {
		t.Error("nothing is active yet")
	}
	if err := Switch(l, "r1"); err != nil {
		t.Fatal(err)
	}
	if err := Switch(l, "r2"); err != nil {
		t.Fatal(err)
	}
	if Active(l) != "r2" || read(t, filepath.Join(l.AppLink(), "v")) != "r2" {
		t.Errorf("active %q", Active(l))
	}
	if err := Switch(l, "nope"); err == nil || Active(l) != "r2" {
		t.Error("switching to a missing release must change nothing")
	}
	if err := Prune(l, map[string]bool{"r2": true, "r3": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.Dir("r1")); err == nil {
		t.Error("r1 kept")
	}
	if _, err := os.Stat(l.LogPath("r1")); err == nil {
		t.Error("r1 log kept")
	}
	if _, err := os.Stat(l.Dir("r3")); err != nil {
		t.Error("r3 removed")
	}
}
