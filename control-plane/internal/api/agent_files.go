package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// Files the person attaches in an agent's chat are saved in the agent's own
// uploads folder, where its tools can open them, and the agent is told where
// they are. Every agent works this way, whatever its model can read.

const (
	agentFileMax         = 25 << 20 // per file
	agentFilesPerMessage = 10
)

type agentAttachment struct {
	ID   string `json:"id"`   // the stored file name
	Name string `json:"name"` // what the person called it
	Type string `json:"type"`
	Size int64  `json:"size"`
}

var unsafeName = regexp.MustCompile(`[^\p{L}\p{N}._ -]+`)
var fileID = regexp.MustCompile(`^[0-9A-Za-z]{26}-[^/\\]{1,120}$`)

// cleanFileName keeps a readable, safe file name.
func cleanFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Trim(unsafeName.ReplaceAllString(name, "_"), " .")
	if r := []rune(name); len(r) > 100 {
		ext := filepath.Ext(name)
		name = string(r[:100-len([]rune(ext))]) + ext
	}
	if name == "" || name == "_" {
		name = "file"
	}
	return name
}

func (s *Server) agentUploads(h *store.AgentInstance) (string, agentcatalog.Package, error) {
	pkg, ok := agentcatalog.Get(h.Package)
	if !ok || pkg.UploadsDir == "" || s.AgentRoot == "" {
		return "", pkg, errors.New("this agent can't receive files")
	}
	return filepath.Join(s.AgentRoot, h.ID, filepath.FromSlash(pkg.UploadsDir)), pkg, nil
}

// POST /v1/agent-instances/{id}/files?name=report.pdf — the body is the file.
func (s *Server) v1UploadAgentFile(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	dir, pkg, err := s.agentUploads(h)
	if err != nil {
		writeV1Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if r.ContentLength > agentFileMax {
		writeV1Err(w, http.StatusRequestEntityTooLarge, "too_large", "Files can be up to 25 MB.")
		return
	}
	name := cleanFileName(r.URL.Query().Get("name"))
	uid, gid := agentOwner(pkg.User)
	for _, d := range []string{filepath.Dir(dir), dir} {
		if err := os.MkdirAll(d, 0o700); err == nil {
			_ = os.Chown(d, uid, gid)
		}
	}
	id := newULID() + "-" + name
	if !fileID.MatchString(id) {
		id = newULID() + "-file"
	}
	dst := filepath.Join(dir, id)
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", "the file could not be saved")
		return
	}
	n, err := io.Copy(f, io.LimitReader(r.Body, agentFileMax+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil || n > agentFileMax || n == 0 {
		_ = os.Remove(dst)
		switch {
		case n > agentFileMax:
			writeV1Err(w, http.StatusRequestEntityTooLarge, "too_large", "Files can be up to 25 MB.")
		case n == 0 && err == nil:
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "This file is empty.")
		default:
			writeV1Err(w, http.StatusBadRequest, "invalid_request", "The file did not arrive. Try again.")
		}
		return
	}
	_ = os.Chown(dst, uid, gid)
	typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if typ == "" {
		typ = "application/octet-stream"
	}
	writeJSON(w, http.StatusCreated, agentAttachment{ID: id, Name: name, Type: typ, Size: n})
}

// GET /v1/agent-instances/{id}/files/{file} — a file the person attached.
func (s *Server) v1GetAgentFile(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	dir, _, err := s.agentUploads(h)
	id := r.PathValue("file")
	if err != nil || !fileID.MatchString(id) || id != filepath.Base(id) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	f, err := os.Open(filepath.Join(dir, id))
	if err != nil {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	defer f.Close()
	name := id[27:]
	typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	// Pictures show in the conversation; anything else is a download.
	if strings.HasPrefix(typ, "image/") && typ != "image/svg+xml" {
		w.Header().Set("Content-Type", typ)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", time.Time{}, f)
}

// takeAttachments checks the files a message refers to.
func (s *Server) takeAttachments(h *store.AgentInstance, ids []string) ([]agentAttachment, error) {
	if len(ids) > agentFilesPerMessage {
		return nil, fmt.Errorf("send up to %d files at a time", agentFilesPerMessage)
	}
	if len(ids) == 0 {
		return []agentAttachment{}, nil
	}
	dir, _, err := s.agentUploads(h)
	if err != nil {
		return nil, err
	}
	out := make([]agentAttachment, 0, len(ids))
	for _, id := range ids {
		if !fileID.MatchString(id) || id != filepath.Base(id) {
			return nil, errors.New("a file is missing; attach it again")
		}
		st, err := os.Stat(filepath.Join(dir, id))
		if err != nil || !st.Mode().IsRegular() {
			return nil, errors.New("a file is missing; attach it again")
		}
		name := id[27:]
		typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if typ == "" {
			typ = "application/octet-stream"
		}
		out = append(out, agentAttachment{ID: id, Name: name, Type: typ, Size: st.Size()})
	}
	return out, nil
}

// withAttachments is the message as the agent reads it: the text, then where
// the attached files are on its own computer.
func withAttachments(content, attachments string, pkg agentcatalog.Package) string {
	var files []agentAttachment
	if json.Unmarshal([]byte(attachments), &files) != nil || len(files) == 0 || pkg.UploadsDir == "" {
		return content
	}
	var b strings.Builder
	b.WriteString(content)
	if content != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("[Attached files, saved on your computer where your tools can open them:")
	for _, f := range files {
		fmt.Fprintf(&b, "\n- %s (%s, %s)", path.Join(pkg.StatePath, pkg.UploadsDir, f.ID), f.Name, humanSize(f.Size))
	}
	b.WriteString("]")
	return b.String()
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

func agentOwner(user string) (int, int) {
	var uid, gid int
	if _, err := fmt.Sscanf(user, "%d:%d", &uid, &gid); err != nil {
		return 0, 0
	}
	return uid, gid
}
