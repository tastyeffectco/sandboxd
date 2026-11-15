package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// An agent's scheduled jobs and background work run without anyone in the
// chat. They reach the person through the message_user tool, which adds the
// message to the agent's conversation in sandboxd, whichever agent it is.

const (
	agentNotesPerHour = 30
	agentNoteMax      = 8000
)

type agentNotes struct {
	mu   sync.Mutex
	sent map[string][]time.Time
}

// take reports whether the agent may send one more message now.
func (n *agentNotes) take(agentID string, at time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.sent == nil {
		n.sent = map[string][]time.Time{}
	}
	recent := n.sent[agentID][:0]
	for _, t := range n.sent[agentID] {
		if at.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	if len(recent) >= agentNotesPerHour {
		n.sent[agentID] = recent
		return false
	}
	n.sent[agentID] = append(recent, at)
	return true
}

func (s *Server) agentMessageUser(ctx context.Context, h *store.AgentInstance, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("write the message in text")
	}
	if err := s.postAgentMessage(ctx, h, text, nil); err != nil {
		return "", err
	}
	return "Sent. The user sees it in their chat with you.", nil
}

// postAgentMessage adds what the agent sent on its own to its conversation.
func (s *Server) postAgentMessage(ctx context.Context, h *store.AgentInstance, text string, files []agentAttachment) error {
	if r := []rune(text); len(r) > agentNoteMax {
		text = string(r[:agentNoteMax]) + "…"
	}
	if !s.agentNotes.take(h.ID, time.Now()) {
		return errors.New("too many messages this hour; send one summary later")
	}
	attached := "[]"
	if len(files) > 0 {
		b, _ := json.Marshal(files)
		attached = string(b)
	}
	m := &store.AgentMessage{ID: newULID(), AgentID: h.ID, Role: "assistant", Content: text,
		Steps: "[]", State: "done", Channel: "agent", CreatedAt: now(), Attachments: attached}
	m.UpdatedAt = m.CreatedAt
	return s.Store.AddAgentMessage(ctx, m)
}

// agentShareFile copies a file the agent made, from its own folder, to the
// files of its conversation and posts it there for the person to download.
// Only files under the agent's data folder (its volume) can be shared.
func (s *Server) agentShareFile(ctx context.Context, h *store.AgentInstance, file, note string) (string, error) {
	dir, pkg, err := s.agentUploads(h)
	if err != nil {
		return "", err
	}
	root := filepath.Join(s.AgentRoot, h.ID)
	src, err := agentPath(root, pkg.StatePath, file)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(src)
	switch {
	case err != nil || !st.Mode().IsRegular():
		return "", fmt.Errorf("there is no file at %s", file)
	case st.Size() == 0:
		return "", errors.New("this file is empty")
	case st.Size() > agentFileMax:
		return "", errors.New("files can be up to 25 MB")
	}
	name := cleanFileName(path.Base(file))
	id := newULID() + "-" + name
	if !fileID.MatchString(id) {
		id = newULID() + "-file"
	}
	makeUploadsDir(dir, pkg.User)
	if err := copyOwned(src, filepath.Join(dir, id), pkg.User); err != nil {
		return "", errors.New("the file could not be copied; try again")
	}
	typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if typ == "" {
		typ = "application/octet-stream"
	}
	if note = strings.TrimSpace(note); note == "" {
		note = "Here is " + name + "."
	}
	if err := s.postAgentMessage(ctx, h, note, []agentAttachment{{ID: id, Name: name, Type: typ, Size: st.Size()}}); err != nil {
		return "", err
	}
	return "Shared. The user can download " + name + " from their chat with you.", nil
}

// agentPath turns a path on the agent's computer into the same file in its
// folder on the host, refusing anything outside its data folder (also
// through links).
func agentPath(root, statePath, file string) (string, error) {
	outside := fmt.Errorf("only files in %s can be shared; save it there first", statePath)
	clean := path.Clean("/" + strings.TrimPrefix(file, "/"))
	if !path.IsAbs(file) {
		return "", fmt.Errorf("give the file's full path, in %s", statePath)
	}
	rel, ok := strings.CutPrefix(clean, strings.TrimRight(statePath, "/")+"/")
	if !ok || rel == "" {
		return "", outside
	}
	real, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", fmt.Errorf("there is no file at %s", file)
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil || !strings.HasPrefix(real, base+string(filepath.Separator)) {
		return "", outside
	}
	return real, nil
}

// copyOwned copies a file, the copy owned by the agent's user.
func copyOwned(src, dst, user string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(in, agentFileMax)); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	uid, gid := agentOwner(user)
	if err := os.Chown(dst, uid, gid); err != nil && os.Geteuid() == 0 {
		return err
	}
	return nil
}

// agentInbox takes what an agent's scheduled job produced, from the agent's
// own webhook delivery (OpenClaw's automations), and puts it in the chat.
// It reads {"text"} and the common names agents use for a job's output.
func (s *Server) agentInbox(w http.ResponseWriter, r *http.Request, h *store.AgentInstance) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}
	text := inboxText(body)
	if text == "" {
		// Nothing to say (OpenClaw skips empty results; be safe anyway).
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := s.agentMessageUser(r.Context(), h, text); err != nil {
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func inboxText(body []byte) string {
	var event map[string]any
	if json.Unmarshal(body, &event) != nil {
		return strings.TrimSpace(string(body))
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := event[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	text := str("text", "message", "notify", "summary", "output", "result")
	if failure := str("error"); failure != "" {
		text = strings.TrimSpace("A scheduled job failed: " + failure + "\n\n" + text)
	}
	if name := str("jobName", "name"); name != "" && text != "" {
		text = "**" + name + "**\n\n" + text
	}
	return text
}
