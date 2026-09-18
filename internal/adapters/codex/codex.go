package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/runner"
)

const adapterName = "codex"

// Adapter is the AgentAdapter backed by OpenAI's Codex CLI. Run executes
// the CLI inside an isolated container per docs/system-design.md section 9;
// Version queries a local `codex` binary directly, which needs neither
// network nor credentials.
type Adapter struct {
	// CodexPath is the codex executable Version() queries; "codex" when
	// empty. Unrelated to what Run's container invokes, which always
	// calls "codex" by name inside Image.
	CodexPath string

	// Image is the container image Run executes in - building and
	// publishing one with the codex CLI baked in is a separate,
	// not-yet-built infrastructure task, matching the same gap in the
	// claude package.
	Image string

	Runner      *runner.DockerRunner
	Credentials Credentials
	Limits      runner.ResourceLimits
}

func New(image string, lookup func(string) (string, bool)) (*Adapter, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	creds, err := DetectCredentials(lookup)
	if err != nil {
		return nil, err
	}
	return &Adapter{
		Image:       image,
		Runner:      &runner.DockerRunner{},
		Credentials: creds,
	}, nil
}

func (a *Adapter) Name() string { return adapterName }

func (a *Adapter) codexPath() string {
	if a.CodexPath == "" {
		return "codex"
	}
	return a.CodexPath
}

// Version reports the local codex CLI's version. Like the claude package,
// this queries the host binary, not the one baked into Image - an interim
// gap pending a real pinned container image.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, a.codexPath(), "--version").Output()
	if err != nil {
		return "", fmt.Errorf("codex: getting version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *Adapter) Run(ctx context.Context, req adapters.RunRequest) (adapters.RunResult, error) {
	started := time.Now()

	if a.Image == "" {
		return adapters.RunResult{}, fmt.Errorf("codex: Image is not configured")
	}
	promptBytes, err := os.ReadFile(req.PromptPath)
	if err != nil {
		return adapters.RunResult{}, fmt.Errorf("codex: reading prompt %s: %w", req.PromptPath, err)
	}

	attempt := req.Environment[adapters.AttemptEnvKey]
	if attempt == "" {
		attempt = "1"
	}
	containerName := fmt.Sprintf("%s-%s-attempt-%s", req.RunID, req.Stage, attempt)

	// codex writes its final message directly to this path via -o; unlike
	// claude there is no envelope to unwrap afterward.
	outputFile := string(req.Stage) + ".json"
	containerOutputPath := "/workspace/output/" + outputFile
	// Before the container, never after: see adapters.PrepareWorkspace.
	if err := adapters.PrepareWorkspace(req.WorkspacePath); err != nil {
		return adapters.RunResult{}, fmt.Errorf("codex: %w", err)
	}
	hostOutputPath := filepath.Join(req.WorkspacePath, "output", outputFile)

	// A chatgpt_login credential travels as a mounted file, not an env
	// var: the CLI reads $CODEX_HOME/auth.json and nothing else.
	var auth *authMount
	if a.Credentials.Kind == CredentialChatGPTLogin {
		m, err := stageAuth(a.Credentials.AuthFilePath())
		if err != nil {
			return adapters.RunResult{}, err
		}
		auth = m
		defer auth.cleanup()
	}

	env := map[string]string{}
	if v := a.Credentials.EnvVar(); v != "" {
		env[v] = a.Credentials.Value
	}

	args := buildCodexArgs(req, containerOutputPath)
	spec := runner.ContainerSpec{
		ContainerName: containerName,
		Image:         a.Image,
		Command:       append([]string{"codex"}, args...),
		WorkingDir:    "/workspace",
		Mounts: []runner.Mount{
			{HostPath: filepath.Join(req.WorkspacePath, "input"), ContainerPath: "/workspace/input", ReadOnly: true},
			{HostPath: filepath.Join(req.WorkspacePath, "output"), ContainerPath: "/workspace/output", ReadOnly: false},
		},
		Env:   env,
		Stdin: promptBytes,
		// Same section 9 exception as the claude adapter: reaching the
		// OpenAI API requires egress. See claude.go's identical comment
		// for the deferred controlled-egress hardening this needs.
		NetworkPolicy: runner.NetworkEnabled,
		Limits:        a.Limits,
	}

	// Read-write so the CLI can rotate its own token; the copy is
	// discarded after the run and any rotation is written back to the
	// host explicitly, under the checks in syncBack.
	if auth != nil {
		spec.Mounts = append(spec.Mounts, runner.Mount{
			HostPath: auth.copyFile, ContainerPath: containerAuthPath, ReadOnly: false,
		})
	}

	result, runErr := a.Runner.Run(ctx, spec)
	finished := time.Now()

	// Write a rotated credential back before anything else can fail: the
	// CLI refreshes its own tokens, and silently discarding a rotation
	// leaves the operator's stored login stale and eventually broken.
	// Refusal to write is surfaced, never swallowed - a half-written
	// auth.json is the one outcome that must not happen quietly.
	var authRefreshed bool
	if auth != nil {
		refreshed, err := auth.syncBack()
		if err != nil {
			return adapters.RunResult{}, err
		}
		authRefreshed = refreshed
	}

	base := adapters.RunResult{
		StartedAt:  started,
		FinishedAt: finished,
		Adapter:    a.Name(),
		ExitCode:   result.ExitCode,
		// Kind only - Credentials.Value is never recorded anywhere.
		AuthMode:      string(a.Credentials.Kind),
		AuthRefreshed: authRefreshed,
		// Usage comes from the event stream's final turn.completed, whose
		// shape was read off a real containerised run (see parseUsage).
		// CostUSD stays zero: under a ChatGPT subscription the vendor
		// reports no billable figure, and inventing one would put a
		// number in a field readers treat as metered.
		Usage: func() adapters.Usage {
			u := parseUsage(result.Stdout)
			if !u.Found {
				return adapters.Usage{}
			}
			return adapters.Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
		}(),
	}

	if runErr != nil {
		return base, fmt.Errorf("codex: %w (stderr: %s)", runErr, result.Stderr)
	}

	content, err := os.ReadFile(hostOutputPath)
	if err != nil {
		return base, fmt.Errorf("codex: reading final message output %s: %w", hostOutputPath, err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return base, fmt.Errorf("codex: final message output %s is empty", hostOutputPath)
	}
	base.OutputPath = hostOutputPath

	return base, nil
}
