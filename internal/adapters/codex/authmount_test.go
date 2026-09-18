package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hostLogin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The copy must be readable by the container's uid (10001) and must not be
// the host file: the host's 0600 is the operator's credential and stays
// untouched.
func TestStageAuthCopiesAndWidensWithoutTouchingTheHost(t *testing.T) {
	host := hostLogin(t, `{"tokens":{"access":"x"}}`)
	m, err := stageAuth(host)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	if m.copyFile == host {
		t.Fatal("the host file itself was staged; it must never be mounted")
	}
	if !strings.HasPrefix(m.copyFile, os.TempDir()) {
		t.Errorf("copy is not in a temp dir: %s", m.copyFile)
	}
	st, err := os.Stat(m.copyFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Errorf("copy is %04o; uid 10001 cannot read 0600", st.Mode().Perm())
	}
	hostSt, _ := os.Stat(host)
	if hostSt.Mode().Perm() != 0o600 {
		t.Errorf("host login permissions changed to %04o", hostSt.Mode().Perm())
	}
	m.cleanup()
	if _, err := os.Stat(m.dir); !os.IsNotExist(err) {
		t.Error("the staged credential outlived the run")
	}
}

func TestSyncBackWritesARotatedToken(t *testing.T) {
	host := hostLogin(t, `{"tokens":{"access":"old"}}`)
	m, err := stageAuth(host)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	rotated := `{"tokens":{"access":"new"}}`
	if err := os.WriteFile(m.copyFile, []byte(rotated), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := m.syncBack()
	if err != nil {
		t.Fatalf("syncBack: %v", err)
	}
	if !changed {
		t.Error("a rotation was not reported")
	}
	got, _ := os.ReadFile(host)
	if string(got) != rotated {
		t.Errorf("host login not updated: %s", got)
	}
	st, _ := os.Stat(host)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("host login left at %04o, must be restored to 0600", st.Mode().Perm())
	}
}

func TestSyncBackLeavesTheHostAloneWhenNothingRotated(t *testing.T) {
	host := hostLogin(t, `{"tokens":{"access":"same"}}`)
	m, _ := stageAuth(host)
	defer m.cleanup()
	changed, err := m.syncBack()
	if err != nil || changed {
		t.Fatalf("unchanged credential reported as rotated: changed=%v err=%v", changed, err)
	}
}

// The one outcome that must never happen: a truncated or corrupt write
// replacing a working login and leaving the operator logged out.
func TestSyncBackRefusesToOverwriteWithGarbage(t *testing.T) {
	const good = `{"tokens":{"access":"good"}}`
	for name, bad := range map[string]string{"empty": "", "whitespace": "  \n", "truncated": `{"tokens":`} {
		host := hostLogin(t, good)
		m, _ := stageAuth(host)
		if err := os.WriteFile(m.copyFile, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		changed, err := m.syncBack()
		if err == nil {
			t.Errorf("%s: a corrupt credential was accepted", name)
		}
		if changed {
			t.Errorf("%s: reported as written", name)
		}
		got, _ := os.ReadFile(host)
		if string(got) != good {
			t.Errorf("%s: the working login was destroyed: %q", name, got)
		}
		m.cleanup()
	}
}

// Detection order: an explicit env var is a choice for this run; a host
// login is ambient state, so it comes last.
func TestDetectCredentialsPrefersEnvOverHostLogin(t *testing.T) {
	host := hostLogin(t, `{"tokens":{}}`)
	dir := filepath.Dir(host)
	withHome := func(extra map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) {
			if k == "CODEX_HOME" {
				return dir, true
			}
			v, ok := extra[k]
			return v, ok
		}
	}
	c, err := DetectCredentials(withHome(map[string]string{envAPIKey: "sk-test"}))
	if err != nil || c.Kind != CredentialAPIKey {
		t.Errorf("api key must win: %v %v", c.Kind, err)
	}
	c, err = DetectCredentials(withHome(nil))
	if err != nil || c.Kind != CredentialChatGPTLogin {
		t.Fatalf("host login not detected: %v %v", c.Kind, err)
	}
	if c.AuthFilePath() != host {
		t.Errorf("AuthFilePath = %q, want the host login path", c.AuthFilePath())
	}
	if json.Valid([]byte(c.Value)) {
		t.Error("Value looks like credential content; it must be a path only")
	}
}
