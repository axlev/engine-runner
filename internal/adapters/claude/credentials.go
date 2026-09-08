// Package claude implements the AgentAdapter backed by the Claude Code CLI,
// invoked non-interactively inside an isolated container per
// docs/system-design.md section 9.
package claude

import "fmt"

// CredentialKind distinguishes the two ways engine-runner can authenticate
// a claude invocation - either is fully supported, matching Claude Code's
// own dual support for API-key and Pro/Max subscription auth.
type CredentialKind string

const (
	CredentialAPIKey     CredentialKind = "api_key"
	CredentialOAuthToken CredentialKind = "oauth_token"
)

const (
	envAPIKey     = "ANTHROPIC_API_KEY"
	envOAuthToken = "CLAUDE_CODE_OAUTH_TOKEN"
)

// Credentials is a detected credential's kind and value. The value is never
// logged, fingerprinted, or written into any result artifact - only Kind is
// safe to record, and only Kind is what ClaudeAdapter reports for
// provenance.
type Credentials struct {
	Kind  CredentialKind
	Value string
}

// EnvVar returns the environment variable name this credential must be
// injected into a container as.
func (c Credentials) EnvVar() string {
	switch c.Kind {
	case CredentialAPIKey:
		return envAPIKey
	case CredentialOAuthToken:
		return envOAuthToken
	default:
		return ""
	}
}

// DetectCredentials finds exactly one usable credential via lookup - an
// injectable stand-in for os.LookupEnv, so detection is testable without
// mutating the process environment. It prefers ANTHROPIC_API_KEY over
// CLAUDE_CODE_OAUTH_TOKEN, matching Claude Code's own authentication
// precedence: when both are set (e.g. a developer's personal Pro/Max token
// left over from interactive use, plus a project API key), engine-runner's
// choice matches what `claude` itself would choose, so provenance recorded
// here can't silently diverge from what actually authenticated the call.
func DetectCredentials(lookup func(string) (string, bool)) (Credentials, error) {
	if v, ok := lookup(envAPIKey); ok && v != "" {
		return Credentials{Kind: CredentialAPIKey, Value: v}, nil
	}
	if v, ok := lookup(envOAuthToken); ok && v != "" {
		return Credentials{Kind: CredentialOAuthToken, Value: v}, nil
	}
	return Credentials{}, fmt.Errorf("claude: no credentials found; set %s or %s", envAPIKey, envOAuthToken)
}
