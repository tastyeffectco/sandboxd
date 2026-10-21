package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// A prior task directory is not proof that this agent has a conversation.
// In particular, switching agent/model/connection or retrying a login failure
// must start fresh instead of asking a CLI to resume a nonexistent session.
func sessionMarker(root string, t *task) string {
	provider := ""
	if t.provider != nil {
		provider = t.provider.ID
	}
	sum := sha256.Sum256([]byte(t.agentName + "\x00" + provider + "\x00" + t.model))
	return filepath.Join(root, "agent-session-state", hex.EncodeToString(sum[:])+".ready")
}

func canContinue(root string, t *task) bool {
	if !t.cont {
		return false
	}
	_, err := os.Stat(sessionMarker(root, t))
	return err == nil
}

func rememberSession(root string, t *task) error {
	marker := sessionMarker(root, t)
	if err := os.MkdirAll(filepath.Dir(marker), 0700); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte("ready\n"), 0600)
}
