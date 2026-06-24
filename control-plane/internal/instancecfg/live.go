// Package instancecfg holds the runtime-editable lifecycle tunables (Phase 8B)
// in a concurrency-safe value that the idle reaper and keepalive path read live,
// and PATCH /v1/settings updates. It contains ONLY safe operational timers —
// never secrets, auth, networking, or egress.
package instancecfg

import (
	"sync"
	"time"
)

// Snapshot is a plain copy of the editable lifecycle settings.
type Snapshot struct {
	IdleEnabled          bool
	IdleThresholdSeconds int
	KeepaliveMaxSeconds  int
}

// Live is the shared, mutable holder. Construct with New; read via the
// accessors (used hot by the reaper/keepalive path); update via Set.
type Live struct {
	mu   sync.RWMutex
	snap Snapshot
}

func New(s Snapshot) *Live { return &Live{snap: s} }

func (l *Live) Snapshot() Snapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.snap
}

func (l *Live) Set(s Snapshot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.snap = s
}

// IdleEnabled reports whether the idle reaper should reap (hot-read each tick).
func (l *Live) IdleEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.snap.IdleEnabled
}

// IdleThreshold is the live idle threshold (hot-read each tick).
func (l *Live) IdleThreshold() time.Duration {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return time.Duration(l.snap.IdleThresholdSeconds) * time.Second
}

// KeepaliveMax is the live max keepalive window the API clamps requests to.
func (l *Live) KeepaliveMax() time.Duration {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return time.Duration(l.snap.KeepaliveMaxSeconds) * time.Second
}
