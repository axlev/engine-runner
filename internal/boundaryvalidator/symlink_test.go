package boundaryvalidator

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// linkIn creates a symlink at <bundle>/reviewer/repository/<name> pointing at
// target, and returns the bundle root.
func linkIn(t *testing.T, bundle, name, target string) {
	t.Helper()
	full := filepath.Join(bundle, "reviewer", "repository", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(target, full); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func irregularViolations(r Report) []string {
	var out []string
	for _, v := range r.Violations {
		if v.Rule == RuleNoIrregularFiles {
			out = append(out, v.Path+": "+v.Detail)
		}
	}
	return out
}

// The case this change exists for: FRR's topotests link shared router configs
// between suites. Every FRR commit carries ~77 of these, so under the old
// blanket rule not one of 1,667 mined candidates could produce a valid
// bundle - failing on links no PR had touched.
func TestInTreeRelativeSymlinkIsAdmissible(t *testing.T) {
	bundle := copyBundle(t)
	shared := filepath.Join(bundle, "reviewer", "repository", "shared", "r1")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shared, "frr.conf"), []byte("router bgp 65001\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// A file link and a directory link, the two shapes FRR actually uses.
	linkIn(t, bundle, "suite/r1", "../shared/r1")
	linkIn(t, bundle, "suite/frr.conf", "../shared/r1/frr.conf")
	rewriteChecksums(t, bundle)

	r := validate(t, bundle)
	if v := irregularViolations(r); len(v) != 0 {
		t.Fatalf("in-tree relative symlinks must be admissible, got: %v", v)
	}
	if !r.Passed() {
		t.Fatalf("bundle should pass: %s", r.Summary())
	}
}

// Everything the relaxation must still refuse. Each is a way a link could
// reach content the reviewer was never given.
func TestSymlinkEscapesAreStillRejected(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "oracle-data.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cases := map[string]struct{ name, target, want string }{
		"absolute target":    {"abs.go", filepath.Join(outside, "oracle-data.json"), "absolute"},
		"escapes via dotdot": {"esc.go", "../../../../etc/passwd", "escapes"},
		"reaches control":    {"ctl.json", "../../control/manifest.json", "escapes"},
		"dangling target":    {"gone.go", "./nope.go", "does not exist"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			bundle := copyBundle(t)
			linkIn(t, bundle, tc.name, tc.target)
			rewriteChecksums(t, bundle)

			r := validate(t, bundle)
			got := irregularViolations(r)
			if len(got) == 0 {
				t.Fatalf("expected rejection, bundle passed: %s", r.Summary())
			}
			if !strings.Contains(strings.Join(got, " "), tc.want) {
				t.Errorf("violation should say %q, got %v", tc.want, got)
			}
		})
	}
}

// Chains are refused rather than followed to a depth limit: FRR has none, and
// a rule with no traversal loop cannot have a traversal bug.
func TestSymlinkChainIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	repo := filepath.Join(bundle, "reviewer", "repository")
	if err := os.WriteFile(filepath.Join(repo, "real.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	linkIn(t, bundle, "first.go", "real.go")
	linkIn(t, bundle, "second.go", "first.go")
	rewriteChecksums(t, bundle)

	got := irregularViolations(validate(t, bundle))
	if !strings.Contains(strings.Join(got, " "), "itself a symlink") {
		t.Errorf("expected the chain to be rejected, got %v", got)
	}
}

// A link outside the source snapshot has no legitimate purpose: only
// reviewer/repository/ is a source tree that can contain them.
func TestSymlinkOutsideTheSnapshotIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	link := filepath.Join(bundle, "reviewer", "notes.md")
	if err := os.Symlink("repository/lib/stream.h", link); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got := irregularViolations(validate(t, bundle))
	if !strings.Contains(strings.Join(got, " "), "outside reviewer/repository/") {
		t.Errorf("expected rejection for a link beside diff.patch, got %v", got)
	}
}

// rewriteChecksums regenerates control/checksums.sha256 over every regular
// file in reviewer/. Needed because these tests add files to a copied bundle,
// and an unrelated checksum failure would mask the rule under test.
func rewriteChecksums(t *testing.T, bundle string) {
	t.Helper()
	reviewer := filepath.Join(bundle, "reviewer")
	var lines []string
	err := filepath.Walk(reviewer, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(bundle, path)
		if relErr != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		lines = append(lines, hex.EncodeToString(sum[:])+"  "+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	sort.Strings(lines)
	out := filepath.Join(bundle, "control", "checksums.sha256")
	if err := os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
}
