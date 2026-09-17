package adapters

import (
	"fmt"
	"os"
	"path/filepath"
)

// PrepareWorkspace creates the input/ and output/ bind-mount sources an
// adapter is about to hand to docker.
//
// It must run BEFORE the container starts. If either directory is missing at
// that point docker creates the bind-mount source itself as root:root 0755,
// and two writes then fail for the price of one paid model call: the stage
// inside the container runs as a non-root user (uid 10001 in our images) and
// cannot write its result, and the adapter's own host-side write of that
// result is refused by a root-owned directory. Both failures land AFTER the
// call has completed and been billed, which is the most expensive possible
// place to discover a missing directory.
//
// 0777 on output/ is deliberate and set with an explicit Chmod, because
// MkdirAll masks its mode with the process umask (typically 022, yielding
// 0755 and reproducing the bug). The container's uid is a property of the
// image, not of the host, so there is no uid to chown to without root. The
// exposure is bounded: this is per-attempt scratch under the operator's own
// cache root, it holds no credentials, and the result is copied out and
// checksummed before anything is read back.
//
// contextbuilder does the same thing for pipeline stages, which is why the
// pipeline never hit this. Callers that build a workspace by hand - cmd/probe
// is one - have no such helper, so adapters do it themselves and no caller
// can get it wrong.
func PrepareWorkspace(workspacePath string) error {
	if workspacePath == "" {
		return fmt.Errorf("adapters: workspace path is empty")
	}
	if err := os.MkdirAll(filepath.Join(workspacePath, "input"), 0o755); err != nil {
		return fmt.Errorf("adapters: creating input dir: %w", err)
	}
	outputDir := filepath.Join(workspacePath, "output")
	if err := os.MkdirAll(outputDir, 0o777); err != nil {
		return fmt.Errorf("adapters: creating output dir: %w", err)
	}
	if err := os.Chmod(outputDir, 0o777); err != nil {
		return fmt.Errorf("adapters: making output dir container-writable: %w", err)
	}
	return nil
}
