package main

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const (
	// A run shorter than this counts as a fast failure.
	fastFailWindow = 10 * time.Second
	// After this many consecutive fast failures the supervisor stops
	// restarting (a hopelessly broken process — reported down, not
	// crash-looped).
	maxFastFails = 5
	maxBackoff   = 30 * time.Second
)

// process supervises one long-running child — the web dev server OR a
// background worker. One child is kept running, restarted with exponential
// backoff on unexpected exit, and abandoned after repeated fast failures.
// (Generalized from the original single dev-server supervisor so an app's
// manifest can declare a web process and/or workers.)
type process struct {
	name    string // "web" or the worker's name
	kind    string // "web" | "worker"
	appDir  string
	command string // shell command, run via `bash -lc`
	logPath string
	log     *slog.Logger

	restartAfterTask bool // bounce this process after each task (manifest restart_after_task)

	mu       sync.Mutex
	proc     *os.Process
	running  bool
	restarts int
}

func newProcess(name, kind, appDir, command, logPath string, log *slog.Logger) *process {
	return &process{name: name, kind: kind, appDir: appDir, command: command, logPath: logPath,
		log: log.With("process", name, "kind", kind)}
}

// supervise is the process's whole lifecycle; it runs until ctx is cancelled
// (runtimed shutdown).
func (p *process) supervise(ctx context.Context) {
	fastFails := 0
	for {
		if ctx.Err() != nil {
			return
		}
		start := time.Now()
		p.runOnce()
		if ctx.Err() != nil {
			return // intentional shutdown — do not restart
		}
		p.mu.Lock()
		p.restarts++
		restarts := p.restarts
		p.mu.Unlock()
		if time.Since(start) < fastFailWindow {
			fastFails++
		} else {
			fastFails = 0
		}
		if fastFails >= maxFastFails {
			p.log.Error("process failing repeatedly — giving up until next start", "restarts", restarts)
			return
		}
		delay := backoff(fastFails)
		p.log.Warn("process exited; restarting after backoff", "delay", delay.String(), "restarts", restarts)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}
	}
}

// runOnce starts the child, records it live, and blocks until it exits.
func (p *process) runOnce() {
	// `bash -lc` so the login PATH (pnpm, node, …) is in scope.
	cmd := exec.Command("bash", "-lc", p.command)
	cmd.Dir = p.appDir
	// Own process group so the whole `bash → … ` tree can be signalled as a unit.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if f, err := os.Create(p.logPath); err == nil {
		cmd.Stdout, cmd.Stderr = f, f
		defer f.Close()
	} else {
		p.log.Warn("process log file", "path", p.logPath, "err", err.Error())
	}
	if err := cmd.Start(); err != nil {
		p.log.Error("process start failed", "err", err.Error())
		return
	}
	p.mu.Lock()
	p.proc = cmd.Process
	p.running = true
	p.mu.Unlock()
	p.log.Info("process started", "pid", cmd.Process.Pid)

	_ = cmd.Wait()

	p.mu.Lock()
	p.proc = nil
	p.running = false
	p.mu.Unlock()
	p.log.Info("process exited")
}

// stop terminates the child's process group: SIGTERM, then SIGKILL if it has
// not exited within the grace window.
func (p *process) stop() {
	p.mu.Lock()
	proc := p.proc
	p.mu.Unlock()
	if proc == nil {
		return
	}
	pgid := proc.Pid // == process group id (Setpgid made it the leader)
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	for i := 0; i < 50; i++ { // up to ~5s
		time.Sleep(100 * time.Millisecond)
		p.mu.Lock()
		running := p.running
		p.mu.Unlock()
		if !running {
			return
		}
	}
	p.log.Warn("process did not exit on SIGTERM; sending SIGKILL")
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// snapshot returns the current process state for GET /status.
func (p *process) snapshot() (pid, restarts int, running bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.proc != nil {
		pid = p.proc.Pid
	}
	return pid, p.restarts, p.running
}

// backoff is exponential in the consecutive-fast-failure count, capped.
func backoff(fastFails int) time.Duration {
	if fastFails < 1 {
		fastFails = 1
	}
	d := time.Second << (fastFails - 1)
	if d <= 0 || d > maxBackoff {
		return maxBackoff
	}
	return d
}
