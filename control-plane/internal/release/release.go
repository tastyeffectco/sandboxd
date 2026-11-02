// Package release lays out the releases of a published app on disk
// (ADR 0003, C2/C3). Inside a production sandbox's workspace (mounted at
// /home/sandbox):
//
//	releases/<id>/       one unpacked, built release per id
//	releases/.logs/<id>.log
//	appdata/<path>       the manifest's data: paths, kept across releases
//	workspace/app ->     ../releases/<active id>  (what runtimed serves)
//
// Every link is relative, so it resolves the same on the host and in the
// container. Nothing here talks to Docker or the store.
package release

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Layout is one production workspace.
type Layout struct {
	Root string // the workspace mount on the host (/home/sandbox inside)
}

func (l Layout) ReleasesDir() string      { return filepath.Join(l.Root, "releases") }
func (l Layout) Dir(id string) string     { return filepath.Join(l.ReleasesDir(), id) }
func (l Layout) LogPath(id string) string { return filepath.Join(l.ReleasesDir(), ".logs", id+".log") }
func (l Layout) DataDir() string          { return filepath.Join(l.Root, "appdata") }
func (l Layout) AppLink() string          { return filepath.Join(l.Root, "workspace", "app") }
func (l Layout) ContainerDir(id string) string {
	return "/home/sandbox/releases/" + id
}

// ErrTooLarge is returned when a bundle unpacks to more than the limit.
var ErrTooLarge = errors.New("the app is too large to publish")

// Unpack extracts a gzipped tar (a `git archive --format=tar.gz`) into dest,
// which must not exist yet. It accepts directories, regular files and
// symlinks that stay inside dest; it never writes outside dest, keeps no
// owner or setuid bits, and stops after maxBytes of file content.
func Unpack(r io.Reader, dest string, maxBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		return err
	}
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("not a release bundle: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("damaged release bundle: %w", err)
		}
		name, ok := cleanEntry(h.Name)
		if !ok {
			return fmt.Errorf("release bundle has an unsafe path: %q", h.Name)
		}
		if name == "" {
			continue // the archive root itself
		}
		target := filepath.Join(dest, name)
		if err := parentsInside(dest, target); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if total > maxBytes {
				return ErrTooLarge
			}
			mode := os.FileMode(0o644)
			if h.Mode&0o111 != 0 {
				mode = 0o755
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(f, io.LimitReader(tr, h.Size))
			if err := f.Close(); cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return cerr
			}
		case tar.TypeSymlink:
			if !linkInside(dest, target, h.Linkname) {
				return fmt.Errorf("release bundle has a link that leaves the app: %q -> %q", h.Name, h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
			// git archive records the commit in a pax global header.
		default:
			// Devices, fifos, hard links: never needed by an app; skip.
		}
	}
}

// cleanEntry normalizes an archive path; ok is false for anything absolute
// or climbing out.
func cleanEntry(name string) (string, bool) {
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return "", false
	}
	clean := filepath.Clean(name)
	if clean == "." {
		return "", true
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// linkInside reports whether a symlink at target with the given link text
// resolves inside dest (lexically; links to links are checked when created).
func linkInside(dest, target, link string) bool {
	if link == "" || filepath.IsAbs(link) {
		return false
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(target), link))
	return resolved == dest || strings.HasPrefix(resolved, dest+string(filepath.Separator))
}

// parentsInside refuses a target whose existing parent directories resolve
// (through symlinks) outside root.
func parentsInside(root, target string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	for {
		if _, err := os.Lstat(dir); err == nil {
			break
		}
		if dir == root || len(dir) <= len(root) {
			return nil
		}
		dir = filepath.Dir(dir)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return fmt.Errorf("release path %q leaves the app", target)
	}
	return nil
}

// LinkData replaces each data path in the release with a link to its
// persistent copy under appdata. The first release seeds appdata from what
// it ships at that path (e.g. an initial SQLite file); later releases keep
// the existing data and drop what they ship there. A path that exists in
// neither is created as a folder, unless its name has a file extension
// (prisma/dev.db): then only its folder is made, and the app creates the
// file through the link.
func LinkData(l Layout, releaseID string, paths []string) error {
	rel := l.Dir(releaseID)
	for _, p := range paths {
		inRelease := filepath.Join(rel, filepath.FromSlash(p))
		kept := filepath.Join(l.DataDir(), filepath.FromSlash(p))
		if err := parentsInside(rel, inRelease); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(inRelease), 0o755); err != nil {
			return err
		}
		if err := parentsInside(rel, inRelease); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
			return err
		}
		if _, err := os.Lstat(kept); errors.Is(err, os.ErrNotExist) {
			fi, lerr := os.Lstat(inRelease)
			switch {
			case lerr == nil && fi.Mode()&os.ModeSymlink == 0:
				if err := os.Rename(inRelease, kept); err != nil {
					return err
				}
			case filepath.Ext(p) == "":
				if err := os.MkdirAll(kept, 0o755); err != nil {
					return err
				}
			}
		} else if err != nil {
			return err
		}
		if err := os.RemoveAll(inRelease); err != nil {
			return err
		}
		link, err := filepath.Rel(filepath.Dir(inRelease), kept)
		if err != nil {
			return err
		}
		if err := os.Symlink(link, inRelease); err != nil {
			return err
		}
	}
	return nil
}

// Active returns the id of the release workspace/app points at, or "".
func Active(l Layout) string {
	link, err := os.Readlink(l.AppLink())
	if err != nil {
		return ""
	}
	if id := filepath.Base(link); filepath.Clean(link) == filepath.Join("..", "releases", id) {
		return id
	}
	return ""
}

// Switch points workspace/app at release id, atomically. A plain folder in
// its place (the empty app of a new production sandbox) is removed.
func Switch(l Layout, id string) error {
	if _, err := os.Stat(l.Dir(id)); err != nil {
		return fmt.Errorf("release %s is not on this server: %w", id, err)
	}
	link := l.AppLink()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(link); err == nil && fi.IsDir() {
		if err := os.RemoveAll(link); err != nil {
			return err
		}
	}
	next := filepath.Join(filepath.Dir(link), ".app-next")
	_ = os.Remove(next)
	if err := os.Symlink(filepath.Join("..", "releases", id), next); err != nil {
		return err
	}
	return os.Rename(next, link)
}

// Prune removes release folders and logs whose id is not in keep.
func Prune(l Layout, keep map[string]bool) error {
	entries, err := os.ReadDir(l.ReleasesDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || keep[name] {
			continue
		}
		errs = append(errs, os.RemoveAll(filepath.Join(l.ReleasesDir(), name)))
	}
	if logs, err := os.ReadDir(filepath.Join(l.ReleasesDir(), ".logs")); err == nil {
		for _, e := range logs {
			if id := strings.TrimSuffix(e.Name(), ".log"); !keep[id] {
				_ = os.Remove(filepath.Join(l.ReleasesDir(), ".logs", e.Name()))
			}
		}
	}
	return errors.Join(errs...)
}

// Tail returns at most n bytes from the end of the file at path ("" if unreadable).
func Tail(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > n {
		_, _ = f.Seek(fi.Size()-n, io.SeekStart)
	}
	b, _ := io.ReadAll(io.LimitReader(f, n))
	return string(b)
}

// Sorted returns the keys of m in order (for stable logs and tests).
func Sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
