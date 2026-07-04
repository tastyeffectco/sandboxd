// Package agentprompt is the single source of truth for the platform system
// prompt injected into every coding-agent run. The prompt text lives in the
// committed prompt.md and is embedded into BOTH binaries that need it:
//   - cmd/runtimed injects it into the agent (claude --append-system-prompt /
//     opencode prompt preamble), rendered with the sandbox's real port so no
//     loopback address or port is hard-coded in the source.
//   - internal/api serves the rendered text (with defaults) in GET /v1/settings
//     so the console can display it read-only.
package agentprompt

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed prompt.md
var raw string

// Defaults used when a field is unset (also what the read-only Settings view
// shows). Kept here, not in prompt.md, so the template carries no literal
// loopback address or port.
const (
	defaultAppDir     = "/home/sandbox/workspace/app"
	defaultPort       = 3000
	defaultHealthPath = "/"
	loopbackHost      = "127.0.0.1" // the sandbox's own loopback — always correct in-container
)

// Vars are the per-sandbox values substituted into the template.
type Vars struct {
	AppDir     string
	Port       int
	HealthPath string
}

// Raw returns the unrendered template (with {{PLACEHOLDERS}}), for tests/debug.
func Raw() string { return raw }

// Render substitutes the sandbox's real values into the template. Any zero
// field falls back to the documented default.
func Render(v Vars) string {
	appDir := v.AppDir
	if appDir == "" {
		appDir = defaultAppDir
	}
	port := v.Port
	if port <= 0 {
		port = defaultPort
	}
	health := v.HealthPath
	if health == "" {
		health = defaultHealthPath
	}
	local := "http://" + loopbackHost + ":" + strconv.Itoa(port)
	return strings.NewReplacer(
		"{{APP_DIR}}", appDir,
		"{{PORT}}", strconv.Itoa(port),
		"{{HEALTH_PATH}}", health,
		"{{LOCAL_URL}}", local,
	).Replace(raw)
}
