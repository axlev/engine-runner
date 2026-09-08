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
	dir := t.TempDir()
	protocolPath := filepath.Join(dir, "protocol.yaml")
	content := `
protocol_version: v1
adapter: claude
retry_policy:
  max_attempts: 1
stages:
  reasoner-1: {prompt: p.md, model: m, output_schema: s.json}
  reasoner-2: {prompt: p.md, model: m, output_schema: s.json}
  reasoner-3: {prompt: p.md, model: m, output_schema: s.json}
`
	if err := os.WriteFile(protocolPath, []byte(content), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var stdout bytes.Buffer
	err := run([]string{
		"-protocol", protocolPath,
		"-bundle", happyPathBundle,
		"-case-id", "happy-path",
		"-results-root", t.TempDir(),
		"-workspace-root", t.TempDir(),
	}, &stdout)
	if err == nil {
		t.Fatalf("expected an error for an unimplemented adapter")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error = %q, want it to mention the unimplemented adapter name", err.Error())
	}
}
