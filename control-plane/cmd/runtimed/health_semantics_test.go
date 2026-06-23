package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sandboxd/control-plane/internal/runtime"
)

func webStatus(p runtime.PreviewStatus) runtime.Status {
	return runtime.Status{Preview: runtime.PreviewState{Status: p}}
}

// postTaskHealth: web app health follows build + preview; preview_ok is set.
func TestPostTaskHealthWeb(t *testing.T) {
	// passed build + ready preview -> healthy, preview_ok=true
	h, po := postTaskHealth(false, runtime.BuildPassed, webStatus(runtime.PreviewReady))
	if !h || po == nil || !*po {
		t.Errorf("passed+ready: healthy=%v previewOK=%v", h, po)
	}
	// skipped build (e.g. Next.js) + ready preview -> still healthy
	if h, _ := postTaskHealth(false, runtime.BuildSkipped, webStatus(runtime.PreviewReady)); !h {
		t.Error("skipped build + ready preview should be healthy")
	}
	// preview down -> unhealthy, preview_ok=false
	h, po = postTaskHealth(false, runtime.BuildPassed, webStatus(runtime.PreviewDown))
	if h || po == nil || *po {
		t.Errorf("passed+down: healthy=%v previewOK=%v", h, po)
	}
	// failed build -> unhealthy even if preview ready
	if h, _ := postTaskHealth(false, runtime.BuildFailed, webStatus(runtime.PreviewReady)); h {
		t.Error("failed build must be unhealthy")
	}
}

// postTaskHealth: worker-only has no preview_ok; health follows a running worker.
func TestPostTaskHealthWorkerOnly(t *testing.T) {
	running := runtime.Status{Processes: []runtime.ProcessState{{Name: "w", Kind: "worker", Running: true}}}
	h, po := postTaskHealth(true, runtime.BuildSkipped, running)
	if !h {
		t.Error("worker-only with a running worker should be healthy")
	}
	if po != nil {
		t.Errorf("worker-only must omit preview_ok, got %v", po)
	}
	// no worker running -> unhealthy
	stopped := runtime.Status{Processes: []runtime.ProcessState{{Name: "w", Kind: "worker", Running: false}}}
	if h, _ := postTaskHealth(true, runtime.BuildSkipped, stopped); h {
		t.Error("worker-only with no running worker should be unhealthy")
	}
}

// preview_ok is omitted from JSON when nil (worker-only); present when set.
func TestTaskResultPreviewOKOmitEmpty(t *testing.T) {
	worker := runtime.TaskResult{BuildStatus: runtime.BuildSkipped}
	b, _ := json.Marshal(worker)
	if strings.Contains(string(b), "preview_ok") {
		t.Errorf("worker-only result must omit preview_ok: %s", b)
	}
	po := true
	web := runtime.TaskResult{BuildStatus: runtime.BuildPassed, PreviewOK: &po}
	if b, _ := json.Marshal(web); !strings.Contains(string(b), `"preview_ok":true`) {
		t.Errorf("web result must include preview_ok: %s", b)
	}
}
