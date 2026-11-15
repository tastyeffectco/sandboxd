package api

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// When an agent's AI model fails (its program stopped, a wrong key, no
// credit), agents retry quietly for minutes while the person watches
// "Thinking…". sandboxd's model proxy sees each failure, so a reply that has
// produced nothing while its model keeps failing ends at once, saying what
// to do.

type modelTroubles struct {
	mu sync.Mutex
	m  map[string]modelTrouble // by agent id
}

type modelTrouble struct {
	at       time.Time
	status   int
	failures int // in a row
}

func (t *modelTroubles) record(agentID string, status int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[string]modelTrouble{}
	}
	if status < 400 || status == 499 { // answered (0: no error status), or the agent hung up
		delete(t.m, agentID)
		return
	}
	cur := t.m[agentID]
	t.m[agentID] = modelTrouble{at: time.Now(), status: status, failures: cur.failures + 1}
}

func (t *modelTroubles) get(agentID string) modelTrouble {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.m[agentID]
}

// modelTroubleAfter is the agent's model failing again and again since a
// reply started (two failures in a row: one may be a passing hiccup).
func (s *Server) modelTroubleAfter(agentID string, since time.Time) (int, bool) {
	t := s.modelTrouble.get(agentID)
	return t.status, t.failures >= 2 && t.at.After(since)
}

// watchModel ends a silent reply whose model keeps failing; it reports the
// model's last status through failed.
func (s *Server) watchModel(ctx context.Context, cancel context.CancelFunc, agentID string, since time.Time,
	every time.Duration, silent func() bool, failed func(status int)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if status, bad := s.modelTroubleAfter(agentID, since); bad && silent() {
				failed(status)
				cancel()
				return
			}
		}
	}
}

// agentModelWatch: how often a reply checks on its model (a test shortens it).
var agentModelWatch = 2 * time.Second

func modelFailureText(name, conn string, status int) string {
	if conn == "" {
		conn = "its model connection"
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return name + "'s AI model refused its key. Check the key of " + conn +
			" in Settings → Models, or choose another model for " + name + "."
	case http.StatusTooManyRequests, http.StatusPaymentRequired:
		return name + "'s AI model is busy or out of credit right now. Try again later, or choose another model for " + name + "."
	}
	return name + " can't reach its AI model (" + conn + "). If it runs on this computer, check that it is running; " +
		"or choose another model for " + name + "."
}
