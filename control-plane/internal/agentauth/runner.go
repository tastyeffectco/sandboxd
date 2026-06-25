package agentauth

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// authProc is a started login process with bridged stdin/stdout.
type authProc struct {
	stdin  io.WriteCloser
	stdout io.Reader // combined stdout+stderr
	wait   func() error
	kill   func()
}

// authRunner launches a provider's login flow with HOME=staging. Pluggable so
// tests can run a fake CLI without Docker.
type authRunner interface {
	start(flow loginFlow, stagingDir string) (*authProc, error)
}

// dockerAuthRunner runs the login in an EPHEMERAL container from the base image
// (used only because it carries the CLI). No workspace is mounted, HOME points
// at the staging dir, and `script` provides the PTY the CLI's interactive flow
// needs. This is NOT an app sandbox.
type dockerAuthRunner struct {
	image     string
	dockerBin string
	userns    string
}

func (d dockerAuthRunner) start(flow loginFlow, stagingDir string) (*authProc, error) {
	name := "agent-auth-" + filepath.Base(stagingDir)
	args := []string{"run", "--rm", "-i", "--name", name}
	if d.userns != "" {
		args = append(args, "--userns", d.userns)
	}
	args = append(args,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--tmpfs", "/tmp:size=64m",
		"-e", "HOME=/auth",
		"-v", stagingDir+":/auth", // the ONLY mount — no workspace
		d.image,
		"script", "-qec", flow.innerCmd, "/dev/null",
	)
	bin := d.dockerBin
	if bin == "" {
		bin = "docker"
	}
	cmd := exec.Command(bin, args...)
	return startProc(cmd, func() { _ = exec.Command(bin, "rm", "-f", name).Run() })
}

// startProc wires a combined stdout+stderr pipe and a stdin pipe, starts the
// command, and returns the handle.
func startProc(cmd *exec.Cmd, killExtra func()) (*authProc, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return nil, err
	}
	_ = pw.Close() // child holds its own dup; close ours so the reader sees EOF on exit
	kill := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		if killExtra != nil {
			killExtra()
		}
	}
	return &authProc{stdin: stdin, stdout: pr, wait: cmd.Wait, kill: kill}, nil
}
