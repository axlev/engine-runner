// Package codex implements the AgentAdapter backed by OpenAI's Codex CLI
// (`codex`), invoked non-interactively inside an isolated container per
// docs/system-design.md section 9.
package codex

import "fmt"

// CredentialKind distinguishes the two ways engine-runner can authenticate
// a codex invocation, mirroring the claude package's dual API-key/
// subscription-token support.
type CredentialKind string

const (
	CredentialAPIKey      CredentialKind = "api_key"
	CredentialAccessToken CredentialKind = "access_token"
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
	return Credentials{}, fmt.Errorf("codex: no credentials found; set %s or %s", envAPIKey, envAccessToken)
}
