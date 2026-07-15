// In-sandbox terminal — GET /v1/sandboxes/{id}/terminal (WebSocket).
//
// Bridges a browser terminal (xterm.js) to an interactive shell running INSIDE
// the sandbox as uid 1000 in the app workspace. This runs in the SAME locked-down
// container the coding agent already executes arbitrary code in
// (--dangerously-skip-permissions), so it adds NO new trust boundary — it's just
// a direct interface to what an authenticated operator can already do. Auth is
// enforced by the middleware before the upgrade; the sandbox is tenant-scoped.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Same-origin only (the console is served same-origin as the API). A missing
	// Origin means a non-browser client (e.g. a CLI) — auth still gates it.
	CheckOrigin: func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" {
			return true
		}
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	},
}

// termResize is the only control message the client sends as a TEXT frame;
// keystrokes come as BINARY frames.
type termResize struct {
	Resize *struct {
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	} `json:"resize"`
}

func (s *Server) v1Terminal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isULID(id) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such sandbox")
		return
	}
	sb, err := s.Store.Get(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeV1Err(w, http.StatusNotFound, "not_found", "no such sandbox")
		return
	}
	if err != nil {
		writeV1Err(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if sb.Status != "running" {
		writeV1Err(w, http.StatusConflict, "conflict", "sandbox is "+sb.Status+" — start it to open a terminal")
		return
	}

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already responded on failure.
	}
	defer conn.Close()

	// Keep the sandbox warm for the length of the session (mirrors handleExec).
	if s.Inflight != nil {
		s.Inflight.Enter(id)
		defer s.Inflight.Exit(id)
	}

	// Interactive login shell as uid 1000 in the app workspace. Prefer bash,
	// fall back to sh for a minimal base image.
	ptmx, cmd, err := s.Docker.ExecTTY("s-"+id, "sandbox", "/home/sandbox/workspace/app",
		[]string{"/bin/sh", "-c", "command -v bash >/dev/null && exec bash -l || exec sh"})
	if err != nil {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n[sandboxd] could not open terminal: "+err.Error()+"\r\n"))
		return
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}()

	// PTY output → WebSocket (binary frames). Closing conn on EOF unblocks the
	// reader loop below.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, rerr := ptmx.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				_ = conn.Close()
				return
			}
		}
	}()

	// WebSocket → PTY. Binary = keystrokes (stdin); text = a JSON resize control.
	for {
		mt, data, rerr := conn.ReadMessage()
		if rerr != nil {
			return // client gone → deferred teardown kills the shell
		}
		switch mt {
		case websocket.BinaryMessage:
			if _, werr := ptmx.Write(data); werr != nil {
				return
			}
		case websocket.TextMessage:
			var ctl termResize
			if json.Unmarshal(data, &ctl) == nil && ctl.Resize != nil {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: ctl.Resize.Cols, Rows: ctl.Resize.Rows})
			}
		}
	}
}
