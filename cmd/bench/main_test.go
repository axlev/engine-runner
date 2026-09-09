package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const happyPathBundle = "../../fixtures/cases/happy-path/prospective"

func TestRunHappyPathEndToEnd(t *testing.T) {
	resultsRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	var stdout bytes.Buffer

	err := run([]string{
		"-repo-root", "../..",
		"-protocol", "../../configs/protocols/pilot-v1.yaml",
		"-agents", "../../fixtures/agents/fixture.yaml",
		"-fixtures-dir", "../../fixtures",
		"-bundle", happyPathBundle,
		"-case-id", "happy-path",
		"-run-id", "test-run-cli-happy",
		"-workspace-root", workspaceRoot,
		"-results-root", resultsRoot,
	}, &stdout)
	if err != nil {
		t.Fatalf("run: %v (stdout: %s)", err, stdout.String())
	}

	if !strings.Contains(stdout.String(), "completed") {
		t.Errorf("stdout = %q, want it to mention \"completed\"", stdout.String())
	}

	runDir := filepath.Join(resultsRoot, "test-run-cli-happy")
	for _, rel := range []string{
		"run.json",
		"checksums.sha256",
		"events.jsonl",
		filepath.Join("stages", "reasoner-1", "review-a.json"),
		filepath.Join("stages", "reasoner-2", "review-b.json"),
		filepath.Join("stages", "reasoner-3", "review-c.json"),
		filepath.Join("evaluation", "scores.json"),
		filepath.Join("evaluation", "findings.json"),
	} {
		if _, err := os.Stat(filepath.Join(runDir, rel)); err != nil {
			t.Errorf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestRunFailedCaseStillWritesResultsAndReturnsError(t *testing.T) {
	resultsRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	var stdout bytes.Buffer

	err := run([]string{
		"-repo-root", "../..",
		"-protocol", "../../configs/protocols/pilot-v1.yaml",
		"-agents", "../../fixtures/agents/fixture.yaml",
		"-fixtures-dir", "../../fixtures",
		"-bundle", happyPathBundle,
		"-case-id", "schema-violation",
		"-run-id", "test-run-cli-fail",
		"-workspace-root", workspaceRoot,
		"-results-root", resultsRoot,
	}, &stdout)
	if err == nil {
		t.Fatalf("expected an error for the schema-violation case")
	}

	runDir := filepath.Join(resultsRoot, "test-run-cli-fail")
	if _, statErr := os.Stat(filepath.Join(runDir, "run.json")); statErr != nil {
		t.Errorf("expected run.json to still be written for a failed run: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(runDir, "evaluation")); !os.IsNotExist(statErr) {
		t.Errorf("expected no evaluation/ dir for a failed run, got err=%v", statErr)
	}
}

func TestRunRequiresBundleAndCaseID(t *testing.T) {
	var stdout bytes.Buffer

	if err := run([]string{"-case-id", "x"}, &stdout); err == nil {
		t.Errorf("expected an error when -bundle is missing")
	}
	if err := run([]string{"-bundle", "somewhere"}, &stdout); err == nil {
		t.Errorf("expected an error when -case-id is missing")
	}
}

func TestRunRejectsUnimplementedAdapter(t *testing.T) {
	armPath := writeAgentSet(t, "gemini", "some-model")

	var stdout bytes.Buffer
	err := run([]string{
		"-repo-root", "../..",
		"-protocol", "../../configs/protocols/pilot-v1.yaml",
		"-agents", armPath,
		"-bundle", happyPathBundle,
		"-case-id", "happy-path",
		"-results-root", t.TempDir(),
		"-workspace-root", t.TempDir(),
	}, &stdout)
	if err == nil {
		t.Fatalf("expected an error for an unimplemented adapter")
	}
	if !strings.Contains(err.Error(), "gemini") {
		t.Errorf("error = %q, want it to mention the unimplemented adapter name", err.Error())
	}
}

// TestRunAcceptsClaudeAndCodexAdapterNames proves the exit criterion in
// docs/system-design.md section 15's Milestone 2 ("swapping providers
// requires configuration changes only") at the config-parsing/wiring level:
// pointing -agents at a different directory selects a different
// AgentAdapter with no other code change, and the protocol is byte-identical
// across both arms. It does not exercise a real
// invocation - neither adapter has a container image configured here, so
// each fails fast with a clear "Image is not configured" error rather than
// attempting network access.
func TestRunAcceptsClaudeAndCodexAdapterNames(t *testing.T) {
	for _, vendor := range []string{"claude", "codex"} {
		t.Run(vendor, func(t *testing.T) {
			armPath := writeAgentSet(t, vendor, "some-model")

			t.Setenv("ANTHROPIC_API_KEY", "sk-test")
			t.Setenv("OPENAI_API_KEY", "sk-test")

			var stdout bytes.Buffer
			err := run([]string{
				"-repo-root", "../..",
				"-protocol", "../../configs/protocols/pilot-v1.yaml",
				"-agents", armPath,
				"-bundle", happyPathBundle,
				"-case-id", "happy-path",
				"-results-root", t.TempDir(),
				"-workspace-root", t.TempDir(),
			}, &stdout)
			if err == nil {
				t.Fatalf("expected an error, since no -adapter-image is configured")
			}
			if !strings.Contains(err.Error(), "Image") {
				t.Errorf("error = %q, want it to fail on the missing adapter image, not on adapter construction", err.Error())
			}
		})
	}
}

// writeAgentSet builds a throwaway one-file arm naming one vendor for all
// three stages. The vendor binding lives outside the protocol, so tests
// that exercise adapter selection vary this rather than the protocol file.
func writeAgentSet(t *testing.T, adapter, model string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "arm.yaml")
	content := "adapter: " + adapter + "\nmodel: " + model + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return path
}

// TestDefaultWorkspaceRootIsOutsideTheRepository pins a boundary, not a
// preference. internal/runner refuses to mount the repository into a stage
// container (system-design section 9), and the per-attempt workspace IS
// bind-mounted. A default inside the repo makes every live run fail on the
// mount guard - which is exactly what happened before this was changed.
func TestDefaultWorkspaceRootIsOutsideTheRepository(t *testing.T) {
	got := defaultWorkspaceRoot()

	if !filepath.IsAbs(got) {
		t.Fatalf("defaultWorkspaceRoot() = %q, want an absolute path: a relative one resolves against the repo and trips the same guard", got)
	}

	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	if got == repoRoot || strings.HasPrefix(got, repoRoot+string(filepath.Separator)) {
		t.Errorf("defaultWorkspaceRoot() = %q, which is inside the repository at %q; it is bind-mounted into stage containers and will be refused", got, repoRoot)
	}
}

// The fallback matters: a machine with no resolvable home must not silently
// land back inside the repository.
func TestDefaultWorkspaceRootFallbackIsStillOutsideTheRepository(t *testing.T) {
	// os.UserCacheDir consults HOME on unix and fails when it is unset.
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")

	got := defaultWorkspaceRoot()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}
	if !filepath.IsAbs(got) || strings.HasPrefix(got, repoRoot+string(filepath.Separator)) {
		t.Errorf("fallback defaultWorkspaceRoot() = %q, want an absolute path outside %q", got, repoRoot)
	}
}
