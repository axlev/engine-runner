// Package codex implements the AgentAdapter backed by OpenAI's Codex CLI
// (`codex`), invoked non-interactively inside an isolated container per
// docs/system-design.md section 9.
package codex

import (
	"fmt"
	"os"
	"path/filepath"
)

// CredentialKind distinguishes the two ways engine-runner can authenticate
// a codex invocation, mirroring the claude package's dual API-key/
// subscription-token support.
type CredentialKind string

const (
	CredentialAPIKey      CredentialKind = "api_key"
	CredentialAccessToken CredentialKind = "access_token"
	// CredentialChatGPTLogin is an interactive `codex login` already done
	// on the host: the CLI persists OAuth tokens to $CODEX_HOME/auth.json
	// and reads them from there, with no environment variable involved.
	// Verified live: a container given ONLY a read-only bind mount of that
	// file authenticated and completed a call.
	CredentialChatGPTLogin CredentialKind = "chatgpt_login"
)

const (
	envAPIKey      = "OPENAI_API_KEY"
	envAccessToken = "CODEX_ACCESS_TOKEN"
)

// Credentials is a detected credential's kind and value. Only Kind is ever
// safe to record in results or logs.
//
// Whether `codex exec` actually reads these two environment variables
// directly, without a prior interactive `codex login`, is not confirmed by
// a live invocation in this codebase - doing so would make a real,
// billable API call under whatever account is configured on the host
// running these tests, which is not something to do without being asked.
// It is inferred from `codex login --help`, which names exactly these two
// variables as what it persists to disk from
// (`printenv OPENAI_API_KEY | codex login --with-api-key` and the
// --with-access-token equivalent for CODEX_ACCESS_TOKEN) - strong but not
// conclusive evidence they also work standalone as ambient env vars.
type Credentials struct {
	Kind  CredentialKind
	Value string
}

// EnvVar is the variable this credential is injected as, or "" for
// chatgpt_login, which travels as a mounted file rather than an env var.
func (c Credentials) EnvVar() string {
	switch c.Kind {
	case CredentialAPIKey:
		return envAPIKey
	case CredentialAccessToken:
		return envAccessToken
	default:
		return ""
	}
}

// AuthFilePath is the host auth.json backing a chatgpt_login credential,
// and "" for the env-var kinds. Value is never the token itself for this
// kind - only where it lives - so nothing that logs Value can leak it.
func (c Credentials) AuthFilePath() string {
	if c.Kind == CredentialChatGPTLogin {
		return c.Value
	}
	return ""
}

// hostAuthFile is where `codex login` persists tokens, honouring CODEX_HOME
// the way the CLI does.
func hostAuthFile(lookup func(string) (string, bool)) string {
	if home, ok := lookup("CODEX_HOME"); ok && home != "" {
		return filepath.Join(home, "auth.json")
	}
	// HOME comes from lookup, not os.UserHomeDir: detection must depend
	// only on what the caller passes, or the result changes with whichever
	// machine the tests run on and a test asserting "no credentials"
	// passes or fails by accident.
	if home, ok := lookup("HOME"); ok && home != "" {
		return filepath.Join(home, ".codex", "auth.json")
	}
	return ""
}

// DetectCredentials finds exactly one usable credential via lookup (an
// injectable stand-in for os.LookupEnv). It prefers OPENAI_API_KEY over
// CODEX_ACCESS_TOKEN - engine-runner's own consistent precedence choice
// (matching the claude package's), not a verified statement about codex's
// internal precedence when both are set.
func DetectCredentials(lookup func(string) (string, bool)) (Credentials, error) {
	if v, ok := lookup(envAPIKey); ok && v != "" {
		return Credentials{Kind: CredentialAPIKey, Value: v}, nil
	}
	if v, ok := lookup(envAccessToken); ok && v != "" {
		return Credentials{Kind: CredentialAccessToken, Value: v}, nil
	}
	// Last, because an explicit environment variable is a deliberate
	// choice for this run while a host login is ambient state. Value
	// holds the PATH, never the token.
	if p := hostAuthFile(lookup); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
			return Credentials{Kind: CredentialChatGPTLogin, Value: p}, nil
		}
	}
	return Credentials{}, fmt.Errorf("codex: no credentials found; set %s or %s, or run `codex login`", envAPIKey, envAccessToken)
}
