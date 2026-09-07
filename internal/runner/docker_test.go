package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDockerRunnerIntegration actually launches a container. It requires a
// reachable Docker daemon and a locally available "busybox" image, neither
// of which this repository's CI or a sandboxed dev environment necessarily
// has - it skips cleanly rather than failing when Available() is false.
// This is the live counterpart to the pure, always-runnable tests in
// container_test.go, which cover the security-critical argument
// construction without needing Docker at all.
func TestDockerRunnerIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d := &DockerRunner{}
	if !d.Available(ctx) {
		t.Skip("docker daemon not reachable in this environment; skipping live container test")
	}

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(inputDir, "prompt.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	spec := ContainerSpec{
		ContainerName: "engine-runner-test-" + t.Name(),
		Image:         "busybox:latest",
		Command:       []string{"sh", "-c", "cat /workspace/input/prompt.md > /workspace/output/echoed.txt"},
		WorkingDir:    "/workspace",
		NetworkPolicy: NetworkDisabled,
		Mounts: []Mount{
			{HostPath: inputDir, ContainerPath: "/workspace/input", ReadOnly: true},
			{HostPath: outputDir, ContainerPath: "/workspace/output", ReadOnly: false},
		},
	}

	result, err := d.Run(ctx, spec)
	if err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, result.Stderr)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0 (stderr: %s)", result.ExitCode, result.Stderr)
	}

	got, err := os.ReadFile(filepath.Join(outputDir, "echoed.txt"))
	if err != nil {
		t.Fatalf("reading container output: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("container output = %q, want %q", got, "hello")
	}
}

func TestDockerRunnerIntegrationRejectsWriteToReadOnlyMount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d := &DockerRunner{}
	if !d.Available(ctx) {
		t.Skip("docker daemon not reachable in this environment; skipping live container test")
	}

	inputDir := t.TempDir()

	spec := ContainerSpec{
		ContainerName: "engine-runner-test-" + t.Name(),
		Image:         "busybox:latest",
		Command:       []string{"sh", "-c", "echo should-not-be-writable > /workspace/input/escape.txt"},
		WorkingDir:    "/workspace",
		NetworkPolicy: NetworkDisabled,
		Mounts: []Mount{
			{HostPath: inputDir, ContainerPath: "/workspace/input", ReadOnly: true},
		},
	}

	result, err := d.Run(ctx, spec)
	if err == nil {
		t.Fatalf("expected a write to a read-only mount to fail, got exit code %d", result.ExitCode)
	}
	if _, statErr := os.Stat(filepath.Join(inputDir, "escape.txt")); !os.IsNotExist(statErr) {
		t.Errorf("read-only mount was written to on the host: %v", statErr)
	}
}
