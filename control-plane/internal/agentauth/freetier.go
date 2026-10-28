package agentauth

import "os"

// defaultOpencodeFreeModel is the keyless Zen model an un-connected opencode
// task runs on. It must be one of Zen's free (no-auth) models — "big-pickle"
// is opencode's featured free model. Override with SANDBOXD_OPENCODE_FREE_MODEL.
const defaultOpencodeFreeModel = "big-pickle"

// OpencodeFreeModel is the model OpenCode runs with no connected account (its
// zero-setup free tier). Operator-overridable.
func OpencodeFreeModel() string {
	if m := os.Getenv("SANDBOXD_OPENCODE_FREE_MODEL"); m != "" {
		return m
	}
	return defaultOpencodeFreeModel
}
