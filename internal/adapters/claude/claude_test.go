package claude

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

func TestNewFailsWithoutCredentials(t *testing.T) {
	if _, err := New("some-image", lookupFrom(map[string]string{})); err == nil {
		t.Fatalf("expected an error when no credentials are configured")
	}
}

func TestNewDetectsCredentials(t *testing.T) {
	a, err := New("some-image", lookupFrom(map[string]string{envAPIKey: "sk-test"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Credentials.Kind != CredentialAPIKey {
		t.Errorf("Credentials.Kind = %q, want %q", a.Credentials.Kind, CredentialAPIKey)
	}
	if a.Name() != "claude" {
		t.Errorf("Name() = %q, want \"claude\"", a.Name())
	}
}

// TestVersionAgainstRealLocalBinary is the one part of this package
// verifiable without Docker or API credentials: `claude --version` runs
// with neither. It skips cleanly when no claude binary is on PATH, the way
// internal/runner's Docker tests skip when no daemon is reachable.
func TestVersionAgainstRealLocalBinary(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude binary not on PATH in this environment; skipping")
	}
	a := &Adapter{}
	version, err := a.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version == "" {
		t.Errorf("Version() returned an empty string")
	}
	if !strings.Contains(version, "Claude Code") {
		t.Errorf("Version() = %q, want it to mention \"Claude Code\"", version)
	}
}

func TestRunRequiresImage(t *testing.T) {
	a, err := New("", lookupFrom(map[string]string{envAPIKey: "sk-test"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = a.Run(context.Background(), adapters.RunRequest{WorkspacePath: t.TempDir(), PromptPath: "/nonexistent"})
	if err == nil {
		t.Fatalf("expected an error when Image is not configured")
	}
	if !strings.Contains(err.Error(), "Image") {
		t.Errorf("error = %q, want it to mention the missing Image", err.Error())
	}
}

// TestWithStderrAttachesTheOnlyDiagnosticThereIs pins the fix for a real
// debugging cycle: a denied docker socket made `docker run` exit 1 in 12ms
// with the reason on stderr, and the adapter returned only "exited with
// code 1". The permission error was in hand and thrown away.
func TestWithStderrAttachesTheOnlyDiagnosticThereIs(t *testing.T) {
	base := errors.New("container: stage-1 exited with code 1")

	got := withStderr(base, []byte("permission denied while trying to connect to the docker API\n"))
	if !strings.Contains(got.Error(), "permission denied") {
		t.Errorf("stderr must survive into the error, got: %s", got)
	}
	if !strings.Contains(got.Error(), "exited with code 1") {
		t.Errorf("original error must be preserved, got: %s", got)
	}
	if !errors.Is(got, base) {
		t.Errorf("wrapping must keep errors.Is working, got: %s", got)
	}
}

// Empty stderr must not render as "(stderr: )", which reads like the
// container said nothing rather than nothing having been captured.
func TestWithStderrOmitsEmptyStderr(t *testing.T) {
	base := errors.New("context deadline exceeded")
	for _, empty := range [][]byte{nil, {}, []byte("   \n\t ")} {
		got := withStderr(base, empty)
		if got != base {
			t.Errorf("withStderr(%q) = %q, want the error returned unchanged", empty, got)
		}
	}
}

// A CLI dumping megabytes must not make the error unreadable or bloat
// telemetry, which stores these strings verbatim.
func TestWithStderrTruncatesRunawayOutput(t *testing.T) {
	got := withStderr(errors.New("boom"), []byte(strings.Repeat("x", maxStderrInError*3)))
	if len(got.Error()) > maxStderrInError+200 {
		t.Errorf("error length %d, want it bounded near maxStderrInError=%d", len(got.Error()), maxStderrInError)
	}
	if !strings.Contains(got.Error(), "truncated") {
		t.Errorf("truncation must be visible, got a %d-char error", len(got.Error()))
	}
}
