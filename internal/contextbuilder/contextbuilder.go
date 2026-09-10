// Package contextbuilder constructs the exact, isolated input materials for
// one reasoning-stage execution. See docs/system-design.md sections 6.3, 7,
// and 9.
//
// The builder works by allow-listing, never by filtering out what is
// forbidden: it copies only the named reviewer-visible entries under a
// case's prospective/reviewer directory, and only the handoff files a
// caller-supplied HandoffPolicy explicitly authorizes for the requested
// stage. The prospective bundle's control directory (engine-only manifest
// and checksums) and any oracle data are never read by this package at all,
// so there is no filter to bypass and no field to accidentally forward.
//
// This package has no notion of protocol YAML, models, or budgets - it only
// answers "which files may this stage see." The orchestrator (Phase 4)
// combines its output with protocol-level settings to build a full
// adapters.RunRequest.
package contextbuilder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axlev/engine-runner/internal/adapters"
)

// HandoffPolicy controls which upstream review outputs a stage may see. The
// orchestrator derives it from the frozen protocol; the context builder
// treats it as the sole source of truth - even if a prior review output is
// available on disk, it is not copied unless the policy authorizes it.
type HandoffPolicy struct {
	// EnableAToB is Pilot v1's "A-to-B handoff": whether reasoner-2 (and,
	// transitively, reasoner-3) may see review-a.json. See section 6.4.
	EnableAToB bool
}

// StageInputs carries whichever prior review outputs exist so far in the
// run. Only the fields required by the requested stage and policy need be
// set.
type StageInputs struct {
	ReviewAPath string
	ReviewBPath string
}

// StageContext is the exact, isolated material prepared for one stage.
type StageContext struct {
	// WorkspacePath is the stage's fresh workspace root, matching
	// adapters.RunRequest.WorkspacePath. Its "input" subdirectory holds
	// everything this package placed; "output" is left for the adapter
	// to write into and is never touched here.
	WorkspacePath string

	// PromptPath is the frozen stage prompt, copied as its own read-only
	// file alongside (not inside) the input tree, matching section 9's
	// treatment of the prompt as a separate mount from reviewer input.
	PromptPath string

	// InputFiles lists every file this package placed under WorkspacePath,
	// as paths relative to WorkspacePath. It exists for isolation tests
	// to assert an exact allow-list rather than merely "nothing forbidden
	// was found."
	InputFiles []string
}

// reviewerEntries are the only entries ever read from
// <bundleRoot>/reviewer. control/manifest.json and control/checksums.sha256
// are never in this list and are therefore never reachable through Prepare,
// regardless of what else exists in the bundle.
var reviewerEntries = []struct {
	name     string
	required bool
}{
	{name: "repository", required: true},
	{name: "diff.patch", required: true},
	{name: "metadata.json", required: false}, // may be legitimately omitted, section 7
}

// Builder constructs stage contexts. It is stateless and safe for concurrent
// use.
type Builder struct{}

func New() *Builder { return &Builder{} }

// Prepare populates workDir (which must already exist, fresh and empty -
// creating an isolated environment is the runner's job, not this package's)
// with exactly the materials adapters.Stage is allowed to see, and returns
// where they ended up.
//
// bundleRoot is a case's prospective/ directory, i.e. the parent of
// reviewer/ and control/.
func (b *Builder) Prepare(stage adapters.Stage, bundleRoot, promptPath, workDir string, policy HandoffPolicy, inputs StageInputs) (StageContext, error) {
	inputDir := filepath.Join(workDir, "input")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return StageContext{}, fmt.Errorf("contextbuilder: creating input dir: %w", err)
	}

	// output/ must be created HERE, even though nothing in this package
	// writes to it. If it does not exist when the container starts, docker
	// creates the bind-mount source itself as root:root 0755 - and the
	// stage runs as a non-root user (uid 10001 in our images), so writing
	// the result fails with "permission denied" AFTER the model call has
	// completed and been paid for. That is the most expensive possible
	// place to discover a missing directory.
	//
	// 0777 is deliberate and set with an explicit Chmod, because MkdirAll
	// masks its mode with the process umask (typically 022, yielding 0755
	// and reproducing the bug). The container's uid is a property of the
	// image, not of the host, so there is no uid to chown to without root.
	// The exposure is bounded: this is per-attempt scratch under the
	// operator's own cache root, it holds no credentials, and the run
	// result is copied out and checksummed before anything is read back.
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(outputDir, 0o777); err != nil {
		return StageContext{}, fmt.Errorf("contextbuilder: creating output dir: %w", err)
	}
	if err := os.Chmod(outputDir, 0o777); err != nil {
		return StageContext{}, fmt.Errorf("contextbuilder: making output dir container-writable: %w", err)
	}

	var placed []string

	reviewerSrcRoot := filepath.Join(bundleRoot, "reviewer")
	reviewerDstRoot := filepath.Join(inputDir, "reviewer")
	for _, entry := range reviewerEntries {
		src := filepath.Join(reviewerSrcRoot, entry.name)
		info, err := os.Lstat(src)
		if err != nil {
			if os.IsNotExist(err) && !entry.required {
				continue
			}
			return StageContext{}, fmt.Errorf("contextbuilder: stat %s: %w", src, err)
		}
		dst := filepath.Join(reviewerDstRoot, entry.name)
		copied, err := copyTree(src, dst, info, workDir)
		if err != nil {
			return StageContext{}, err
		}
		placed = append(placed, copied...)
	}

	handoffDir := filepath.Join(inputDir, "handoff")
	switch stage {
	case adapters.StageReasoner1:
		// Discovery reviewer sees the prospective bundle only. No prior
		// review exists yet, so there is nothing to authorize.

	case adapters.StageReasoner2:
		if policy.EnableAToB {
			copied, err := copyHandoff(inputs.ReviewAPath, filepath.Join(handoffDir, "review-a.json"), workDir, "review-a")
			if err != nil {
				return StageContext{}, err
			}
			placed = append(placed, copied)
		}

	case adapters.StageReasoner3:
		// Reasoner 3 always sees exactly what reasoner 2 saw, plus
		// review-b.json - never review-a.json on its own authority.
		if policy.EnableAToB {
			copied, err := copyHandoff(inputs.ReviewAPath, filepath.Join(handoffDir, "review-a.json"), workDir, "review-a")
			if err != nil {
				return StageContext{}, err
			}
			placed = append(placed, copied)
		}
		copied, err := copyHandoff(inputs.ReviewBPath, filepath.Join(handoffDir, "review-b.json"), workDir, "review-b")
		if err != nil {
			return StageContext{}, err
		}
		placed = append(placed, copied)

	default:
		return StageContext{}, fmt.Errorf("contextbuilder: unknown stage %q", stage)
	}

	promptDst := filepath.Join(workDir, "prompt"+filepath.Ext(promptPath))
	if err := copyFile(promptPath, promptDst); err != nil {
		return StageContext{}, err
	}

	return StageContext{
		WorkspacePath: workDir,
		PromptPath:    promptDst,
		InputFiles:    placed,
	}, nil
}

func copyHandoff(src, dst, workDir, label string) (string, error) {
	if src == "" {
		return "", fmt.Errorf("contextbuilder: %s handoff is authorized but no %s output was supplied", label, label)
	}
	if err := copyFile(src, dst); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(workDir, dst)
	if err != nil {
		return "", fmt.Errorf("contextbuilder: computing relative path for %s: %w", dst, err)
	}
	return rel, nil
}

// copyTree copies src (a file, directory, or in-tree symlink) to dst and
// returns the workDir-relative paths of every file copied.
//
// A symlink is recreated as a symlink rather than dereferenced, and only
// after the same containment check the boundary validator applies: relative
// target, resolving inside the copied tree, to something that exists and is
// not itself a link. Dereferencing instead would put identical content at
// two paths, so a diff naming one path would no longer reproduce the
// snapshot - and a reviewer would see duplicated files with no indication
// they are linked.
//
// This check is deliberately not delegated to the validator having already
// run. The validator gates admissibility; this gates what is placed in a
// container. Two independent checks of the same property is the posture the
// rest of this package takes (allow-list, not filter).
func copyTree(src, dst string, info os.FileInfo, workDir string) ([]string, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		return copySymlink(src, dst, workDir)
	}
	if !info.IsDir() {
		if err := copyFile(src, dst); err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(workDir, dst)
		if err != nil {
			return nil, fmt.Errorf("contextbuilder: computing relative path for %s: %w", dst, err)
		}
		return []string{rel}, nil
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, fmt.Errorf("contextbuilder: creating %s: %w", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, fmt.Errorf("contextbuilder: reading %s: %w", src, err)
	}
	var placed []string
	for _, e := range entries {
		childInfo, err := e.Info()
		if err != nil {
			return nil, fmt.Errorf("contextbuilder: statting %s: %w", filepath.Join(src, e.Name()), err)
		}
		copied, err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), childInfo, workDir)
		if err != nil {
			return nil, err
		}
		placed = append(placed, copied...)
	}
	return placed, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("contextbuilder: reading %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("contextbuilder: creating %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("contextbuilder: writing %s: %w", dst, err)
	}
	return nil
}

// symlinkRoot is the directory a copied symlink's target must stay inside:
// the source snapshot the link came from.
func symlinkRoot(src string) string {
	// src is <bundle>/reviewer/repository/<...>; walk up to repository/.
	dir := src
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		if filepath.Base(dir) == "repository" {
			return dir
		}
		dir = parent
	}
}

// copySymlink recreates one in-tree symlink at dst, or refuses it.
//
// Returns no copied paths: the link is not a file whose bytes are hashed,
// and its target is already accounted for under the target's own path.
func copySymlink(src, dst, workDir string) ([]string, error) {
	root := symlinkRoot(src)
	if root == "" {
		return nil, fmt.Errorf("contextbuilder: refusing symlink %s outside a repository snapshot", src)
	}

	target, err := os.Readlink(src)
	if err != nil {
		return nil, fmt.Errorf("contextbuilder: reading symlink %s: %w", src, err)
	}
	if filepath.IsAbs(target) {
		return nil, fmt.Errorf("contextbuilder: refusing symlink %s with absolute target %q", src, target)
	}

	resolved := filepath.Clean(filepath.Join(filepath.Dir(src), target))
	rootAbs, err1 := filepath.Abs(root)
	resAbs, err2 := filepath.Abs(resolved)
	if err1 != nil || err2 != nil {
		return nil, fmt.Errorf("contextbuilder: cannot resolve symlink %s for containment", src)
	}
	if resAbs != rootAbs && !strings.HasPrefix(resAbs, rootAbs+string(filepath.Separator)) {
		return nil, fmt.Errorf("contextbuilder: refusing symlink %s: target %q escapes the snapshot", src, target)
	}

	info, err := os.Lstat(resolved)
	if err != nil {
		return nil, fmt.Errorf("contextbuilder: refusing symlink %s: target %q does not exist", src, target)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("contextbuilder: refusing symlink %s: target %q is itself a symlink", src, target)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, fmt.Errorf("contextbuilder: creating parent for %s: %w", dst, err)
	}
	if err := os.Symlink(target, dst); err != nil {
		return nil, fmt.Errorf("contextbuilder: recreating symlink %s: %w", dst, err)
	}
	return nil, nil
}
