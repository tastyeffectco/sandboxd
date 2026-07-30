package agentprompt

import (
	"strings"
	"testing"
)

// The platform briefing must carry the Project-brain convention: agents are
// told to read workspace/BRAIN.md before starting and append durable learnings
// before finishing. Every agent adapter delivers this briefing, so this single
// section is what standardizes the brain across OpenCode/Claude Code/Codex.
func TestRenderIncludesProjectBrain(t *testing.T) {
	out := Render(Vars{AppDir: "/home/sandbox/workspace/app"})

	for _, want := range []string{
		"## Project brain",
		"/home/sandbox/workspace/app/BRAIN.md", // path rendered with the real app dir
		"verification command",
		"never write secrets",
		"If nothing durable was learned, write nothing.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered briefing missing %q", want)
		}
	}

	if strings.Contains(out, "{{APP_DIR}}") {
		t.Error("unsubstituted {{APP_DIR}} placeholder left in rendered briefing")
	}
}
