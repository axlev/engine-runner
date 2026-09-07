package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// DockerRunner executes a ContainerSpec via the docker CLI.
type DockerRunner struct {
	// DockerPath is the docker executable to invoke; "docker" when empty.
	DockerPath string
}

func (d *DockerRunner) dockerPath() string {
	if d.DockerPath == "" {
		return "docker"
	}
	return d.DockerPath
}

// Available reports whether this DockerRunner can actually reach a Docker
// daemon right now. Use it to skip live container execution - in tests or
// otherwise - rather than failing confusingly or silently no-op'ing in an
// environment without Docker access.
func (d *DockerRunner) Available(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, d.dockerPath(), "version")
	return cmd.Run() == nil
}

// Result is the outcome of one container execution.
type Result struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

// Run executes spec to completion. If ctx is cancelled or its deadline
// expires before the container exits on its own, Run force-kills the named
// container via `docker kill` rather than relying on killing the `docker
// run` foreground process itself - that process dying does not reliably
// stop the detached container it started.
func (d *DockerRunner) Run(ctx context.Context, spec ContainerSpec) (Result, error) {
	args, err := BuildDockerArgs(spec)
	if err != nil {
		return Result{}, err
	}

	cmd := exec.Command(d.dockerPath(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("container: starting docker run: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-ctx.Done():
		kill := exec.Command(d.dockerPath(), "kill", spec.ContainerName)
		_ = kill.Run() // best effort; the container may already have exited
		<-done         // reap the process regardless
		return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, ctx.Err()

	case waitErr := <-done:
		result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
		if waitErr == nil {
			return result, nil
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, fmt.Errorf("container: %s exited with code %d: %w", spec.ContainerName, result.ExitCode, waitErr)
		}
		return result, fmt.Errorf("container: running docker: %w", waitErr)
	}
}
