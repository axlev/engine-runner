package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// containerAuthPath is where the image's CODEX_HOME puts auth.json
// (docker/Dockerfile: ENV CODEX_HOME=/home/reasoner/.codex, user reasoner
// uid 10001). The CLI reads its login from there and nowhere else.
const containerAuthPath = "/home/reasoner/.codex/auth.json"

// authMount is a per-run copy of the host's codex login, staged for
// mounting and cleaned up afterwards.
type authMount struct {
	hostFile string // the operator's real auth.json; never mounted
	copyFile string // the short-lived copy that is
	dir      string
	before   []byte
}

// stageAuth copies the host login into a short-lived directory OUTSIDE the
// workspace.
//
// Outside deliberately, on both counts. The workspace's input/ is mounted
// into the container and is the reviewer's readable tree - a credential
// there is handed to the model being evaluated and can be quoted into a
// finding and sealed into results. And per-attempt workspaces are not
// reclaimed by default, so a copy left in one outlives the run indefinitely.
//
// The copy is 0644 because the container runs as uid 10001 and the host
// file is 0600 owned by the operator: uid 10001 cannot read it, and there
// is no uid to chown to without root. Widening is acceptable only because
// this copy is short-lived and outside both the workspace and the
// repository. The host file itself is never modified here.
func stageAuth(hostFile string) (*authMount, error) {
	raw, err := os.ReadFile(hostFile)
	if err != nil {
		return nil, fmt.Errorf("codex: reading host login %s: %w", hostFile, err)
	}
	dir, err := os.MkdirTemp("", "codex-auth-")
	if err != nil {
		return nil, fmt.Errorf("codex: staging login: %w", err)
	}
	copyFile := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(copyFile, raw, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("codex: staging login: %w", err)
	}
	return &authMount{hostFile: hostFile, copyFile: copyFile, dir: dir, before: raw}, nil
}

// syncBack writes a rotated credential back to the host and reports whether
// it did.
//
// The CLI refreshes its own tokens. Discarding a rotation would leave the
// operator's stored login stale and eventually broken, which is worse than
// writing the file - so it is written, but only under conditions that
// cannot leave them logged out:
//
//   - unchanged content writes nothing at all;
//   - a copy that is empty or does not parse as JSON is refused, because a
//     truncated write replacing a working credential is the one outcome
//     that must not happen;
//   - the write is atomic (temp file in the same directory, then rename)
//     and restores 0600, so an interrupted run cannot leave a half-written
//     auth.json where a whole one was.
func (m *authMount) syncBack() (bool, error) {
	if m == nil {
		return false, nil
	}
	after, err := os.ReadFile(m.copyFile)
	if err != nil {
		return false, fmt.Errorf("codex: reading refreshed login: %w", err)
	}
	if bytes.Equal(after, m.before) {
		return false, nil
	}
	if len(bytes.TrimSpace(after)) == 0 || !json.Valid(after) {
		return false, fmt.Errorf("codex: refreshed login is empty or not JSON; refusing to overwrite %s", m.hostFile)
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.hostFile), ".auth.json.")
	if err != nil {
		return false, fmt.Errorf("codex: writing refreshed login: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(after); err != nil {
		tmp.Close()
		return false, fmt.Errorf("codex: writing refreshed login: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, fmt.Errorf("codex: syncing refreshed login: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("codex: closing refreshed login: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return false, fmt.Errorf("codex: restoring login permissions: %w", err)
	}
	if err := os.Rename(tmp.Name(), m.hostFile); err != nil {
		return false, fmt.Errorf("codex: replacing %s: %w", m.hostFile, err)
	}
	return true, nil
}

func (m *authMount) cleanup() {
	if m != nil {
		_ = os.RemoveAll(m.dir)
	}
}
