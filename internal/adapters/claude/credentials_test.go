package claude

import "testing"

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func TestDetectCredentialsPrefersAPIKeyWhenBothSet(t *testing.T) {
	creds, err := DetectCredentials(lookupFrom(map[string]string{
		envAPIKey:     "sk-ant-test",
		envOAuthToken: "oauth-test",
	}))
	if err != nil {
		t.Fatalf("DetectCredentials: %v", err)
	}
	if creds.Kind != CredentialAPIKey {
		t.Errorf("Kind = %q, want %q", creds.Kind, CredentialAPIKey)
	}
	if creds.Value != "sk-ant-test" {
		t.Errorf("Value = %q, want the API key value", creds.Value)
	}
}

func TestDetectCredentialsFallsBackToOAuthToken(t *testing.T) {
	creds, err := DetectCredentials(lookupFrom(map[string]string{
		envOAuthToken: "oauth-test",
	}))
	if err != nil {
		t.Fatalf("DetectCredentials: %v", err)
	}
	if creds.Kind != CredentialOAuthToken {
		t.Errorf("Kind = %q, want %q", creds.Kind, CredentialOAuthToken)
	}
	if creds.Value != "oauth-test" {
		t.Errorf("Value = %q, want the OAuth token value", creds.Value)
	}
}

func TestDetectCredentialsErrorsWhenNeitherSet(t *testing.T) {
	if _, err := DetectCredentials(lookupFrom(map[string]string{})); err == nil {
		t.Fatalf("expected an error when neither credential is set")
	}
}

func TestDetectCredentialsTreatsEmptyValueAsUnset(t *testing.T) {
	creds, err := DetectCredentials(lookupFrom(map[string]string{
		envAPIKey:     "",
		envOAuthToken: "oauth-test",
	}))
	if err != nil {
		t.Fatalf("DetectCredentials: %v", err)
	}
	if creds.Kind != CredentialOAuthToken {
		t.Errorf("Kind = %q, want fallback to %q when API key is empty", creds.Kind, CredentialOAuthToken)
	}
}

func TestCredentialsEnvVar(t *testing.T) {
	if got := (Credentials{Kind: CredentialAPIKey}).EnvVar(); got != envAPIKey {
		t.Errorf("EnvVar() = %q, want %q", got, envAPIKey)
	}
	if got := (Credentials{Kind: CredentialOAuthToken}).EnvVar(); got != envOAuthToken {
		t.Errorf("EnvVar() = %q, want %q", got, envOAuthToken)
	}
}
