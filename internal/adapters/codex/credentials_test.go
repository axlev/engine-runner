package codex

import "testing"

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

func TestDetectCredentialsPrefersAPIKeyWhenBothSet(t *testing.T) {
	creds, err := DetectCredentials(lookupFrom(map[string]string{
		envAPIKey:      "sk-test",
		envAccessToken: "token-test",
	}))
	if err != nil {
		t.Fatalf("DetectCredentials: %v", err)
	}
	if creds.Kind != CredentialAPIKey {
		t.Errorf("Kind = %q, want %q", creds.Kind, CredentialAPIKey)
	}
}

func TestDetectCredentialsFallsBackToAccessToken(t *testing.T) {
	creds, err := DetectCredentials(lookupFrom(map[string]string{
		envAccessToken: "token-test",
	}))
	if err != nil {
		t.Fatalf("DetectCredentials: %v", err)
	}
	if creds.Kind != CredentialAccessToken {
		t.Errorf("Kind = %q, want %q", creds.Kind, CredentialAccessToken)
	}
}

func TestDetectCredentialsErrorsWhenNeitherSet(t *testing.T) {
	if _, err := DetectCredentials(lookupFrom(map[string]string{})); err == nil {
		t.Fatalf("expected an error when neither credential is set")
	}
}

func TestCredentialsEnvVar(t *testing.T) {
	if got := (Credentials{Kind: CredentialAPIKey}).EnvVar(); got != envAPIKey {
		t.Errorf("EnvVar() = %q, want %q", got, envAPIKey)
	}
	if got := (Credentials{Kind: CredentialAccessToken}).EnvVar(); got != envAccessToken {
		t.Errorf("EnvVar() = %q, want %q", got, envAccessToken)
	}
}
