package contextbuilder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

// stageBundle copies the happy-path bundle into a temp dir so a test can add
// links to its snapshot without touching the committed fixture.
func stageBundle(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(bundleRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(bundleRoot, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return dst
}

// A link must arrive in the container AS A LINK. Dereferencing it would put
// identical content at two paths, so a diff naming one path would no longer
// reproduce the snapshot, and a reviewer would see duplicated files with no
// indication they are linked - a finding the format induced.
func TestInTreeSymlinkIsCopiedAsASymlink(t *testing.T) {
	bundle := stageBundle(t)
	repo := filepath.Join(bundle, "reviewer", "repository")
	if err := os.MkdirAll(filepath.Join(repo, "shared"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "shared", "frr.conf"), []byte("router bgp 65001\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink("shared/frr.conf", filepath.Join(repo, "linked.conf")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	work := t.TempDir()
	if _, err := New().Prepare(adapters.StageReasoner1, bundle, promptPath, work, HandoffPolicy{}, StageInputs{}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	placed := filepath.Join(work, "input", "reviewer", "repository", "linked.conf")
	info, err := os.Lstat(placed)
	if err != nil {
		t.Fatalf("the link was not placed in the stage input: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link was dereferenced into a regular file; it must stay a link")
	}
	target, err := os.Readlink(placed)
	if err != nil || target != "shared/frr.conf" {
		t.Errorf("target = %q (err %v), want it preserved verbatim", target, err)
	}
	// And it must actually resolve where the reviewer reads it.
	body, err := os.ReadFile(placed)
	if err != nil || !strings.Contains(string(body), "router bgp") {
		t.Errorf("link does not resolve inside the stage input: %v", err)
	}
}

// The context builder checks containment itself rather than trusting that the
// validator already ran. It gates what enters a container; the validator gates
// admissibility. Two independent checks of one property is the posture this
// package takes everywhere else.
func TestEscapingSymlinkIsRefusedEvenIfValidationWasSkipped(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "oracle.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for name, target := range map[string]string{
		"absolute": filepath.Join(outside, "oracle.json"),
		"dotdot":   "../../../../etc/passwd",
		"dangling": "./missing.conf",
	} {
		t.Run(name, func(t *testing.T) {
			bundle := stageBundle(t)
			link := filepath.Join(bundle, "reviewer", "repository", "leak.json")
			if err := os.Symlink(target, link); err != nil {
				t.Fatalf("setup: %v", err)
			}
			_, err := New().Prepare(adapters.StageReasoner1, bundle, promptPath, t.TempDir(), HandoffPolicy{}, StageInputs{})
			if err == nil {
				t.Fatalf("expected Prepare to refuse a symlink with target %q", target)
			}
		})
	}
}
