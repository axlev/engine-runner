package codex

import (
	"context"
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
	if a.Name() != "codex" {
		t.Errorf("Name() = %q, want \"codex\"", a.Name())
	}
}

// TestVersionAgainstRealLocalBinary needs neither Docker nor credentials:
// `codex --version` runs with neither, so this is genuinely verified rather
// than mocked, and skips cleanly where no codex binary is on PATH.
func TestVersionAgainstRealLocalBinary(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex binary not on PATH in this environment; skipping")
	}
	a := &Adapter{}
	version, err := a.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version == "" {
		t.Errorf("Version() returned an empty string")
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
