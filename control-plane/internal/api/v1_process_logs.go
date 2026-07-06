// v1_process_logs.go — Phase 7B: read-only tail of a supervised process's log.
// Logs live at ~/.runtimed/<name>.log inside the workspace, which the general
// files API REFUSES (.runtimed/ is a reserved subtree), so this is the only
// way to read them. Sandbox-scoped by id (like the rest of the v1 sandbox API);
// the process name is strictly validated so no path can escape the runtime dir;
// tail/limit only; no write or delete.
package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

const (
	procLogsDefaultTail = 200
	procLogsMaxTail     = 1000
	procLogsReadCap     = 256 * 1024 // never read more than the last 256 KiB
)

// procNameRe matches a valid process name: the same charset runtimed allows for
// worker names plus "web". No path separators, no "." — so "<name>.log" stays
// inside .runtimed/.
var procNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// v1ProcessLogs — GET /v1/sandboxes/{id}/processes/{name}/logs?tail=N.
func (s *Server) v1ProcessLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isULID(id) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request", "invalid sandbox id")
		return
	}
	if _, err := s.Store.Get(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such sandbox")
		return
	} else if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	name := r.PathValue("name")
	if !procNameRe.MatchString(name) {
		writeV1Err(w, http.StatusBadRequest, "invalid_request",
			"invalid process name (allowed: [A-Za-z0-9_-], 1-64 chars)")
		return
	}
	tail := procLogsDefaultTail
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = n
		}
	}
	if tail > procLogsMaxTail {
		tail = procLogsMaxTail
	}

	_, mnt := s.Loopback.Paths(id)
	logPath := filepath.Join(mnt, ".runtimed", name+".log")
	lines, err := tailFile(logPath, tail)
	if errors.Is(err, os.ErrNotExist) {
		// No log file => no such process (or it never started). Treated as
		// not-found so an unknown process name doesn't look like an empty log.
		writeV1Err(w, http.StatusNotFound, "not_found", "no logs for that process")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"process": name, "lines": lines})
}

// tailFile returns the last n lines of a file, reading at most the last
// procLogsReadCap bytes (so a huge log is never read fully).
func tailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := int64(0)
	if fi.Size() > procLogsReadCap {
		start = fi.Size() - procLogsReadCap
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return []string{}, nil
	}
	lines := strings.Split(trimmed, "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:] // drop the partial first line from the byte-cap seek
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
