package claude

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

const adapterName = "claude"

// Adapter is the AgentAdapter backed by the Claude Code CLI. Run executes
// the CLI inside an isolated container per docs/system-design.md section 9;
// Version queries a local `claude` binary directly since a version check is
// metadata, not reasoning execution, and does not need the same isolation.
type Adapter struct {
	// ClaudePath is the claude executable Version() queries; "claude"
	// when empty. It is unrelated to what actually runs inside Run's
	// container, which always invokes "claude" by name inside Image.
	ClaudePath string

	// Image is the container image Run executes in. Building and
	// publishing an image with the claude CLI baked in is a separate,
	// not-yet-built infrastructure task (docs/system-design.md build/
	// images/); Run fails clearly if Image is empty rather than silently
	// falling back to some assumed default.
	Image string

	Runner      *runner.DockerRunner
	Credentials Credentials

	// Limits bounds every container Run launches. Wall-clock is enforced
	// by the caller's context, not a container flag - the CLI itself has
	// no --timeout flag (verified: it does not exist in `claude --help`).
	Limits runner.ResourceLimits
}

// New constructs an Adapter, detecting credentials via lookup (nil means
// os.LookupEnv). image is required for Run but not for Version.
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

func (a *Adapter) claudePath() string {
	if a.ClaudePath == "" {
		return "claude"
	}
	return a.ClaudePath
}

// Version reports the local claude CLI's version string. This queries
// whatever `claude` binary is on the host, not the one baked into Image -
// an acceptable interim gap while no pinned container image exists yet;
// once one does, Version should be changed to query the image instead so
// the reported version matches what Run actually executed.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, a.claudePath(), "--version").Output()
	if err != nil {
		return "", fmt.Errorf("claude: getting version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *Adapter) Run(ctx context.Context, req adapters.RunRequest) (adapters.RunResult, error) {
	started := time.Now()

	if a.Image == "" {
		return adapters.RunResult{}, fmt.Errorf("claude: Image is not configured")
	}
	promptBytes, err := os.ReadFile(req.PromptPath)
	if err != nil {
		return adapters.RunResult{}, fmt.Errorf("claude: reading prompt %s: %w", req.PromptPath, err)
	}

	attempt := req.Environment[adapters.AttemptEnvKey]
	if attempt == "" {
		attempt = "1"
	}
	containerName := fmt.Sprintf("%s-%s-attempt-%s", req.RunID, req.Stage, attempt)

	args := buildClaudeArgs(req, a.Credentials, string(promptBytes))
	spec := runner.ContainerSpec{
		ContainerName: containerName,
		Image:         a.Image,
		Command:       append([]string{"claude"}, args...),
		WorkingDir:    "/workspace",
		Mounts: []runner.Mount{
			{HostPath: filepath.Join(req.WorkspacePath, "input"), ContainerPath: "/workspace/input", ReadOnly: true},
			{HostPath: filepath.Join(req.WorkspacePath, "output"), ContainerPath: "/workspace/output", ReadOnly: false},
		},
		Env: map[string]string{a.Credentials.EnvVar(): a.Credentials.Value},
		// Section 9's escape hatch: "network disabled unless a vendor
		// invocation specifically requires controlled egress" - reaching
		// the Anthropic API is exactly that case. Unrestricted egress
		// here is a known gap: docs/system-design.md section 16 leaves
		// "whether controlled egress is provided by a host-side adapter
		// proxy or a tightly allowlisted container network" as an
		// explicitly deferred decision, not resolved by this adapter.
		NetworkPolicy: runner.NetworkEnabled,
		Limits:        a.Limits,
	}

	result, runErr := a.Runner.Run(ctx, spec)
	finished := time.Now()

	base := adapters.RunResult{
		StartedAt:  started,
		FinishedAt: finished,
		Adapter:    a.Name(),
		ExitCode:   result.ExitCode,
		// Kind only - Credentials.Value is never recorded anywhere.
		AuthMode: string(a.Credentials.Kind),
	}

	if runErr != nil && len(result.Stdout) == 0 {
		// The container never produced a response envelope at all (e.g.
		// context cancellation, or docker itself failed to start it) -
		// nothing to parse. stderr is the only diagnostic that exists in
		// that case, so it must travel with the error: a docker-level
		// failure such as a denied socket is otherwise indistinguishable
		// from a CLI crash, both surfacing as "exited with code 1".
		return base, withStderr(runErr, result.Stderr)
	}

	resp, parseErr := parseResponse(result.Stdout)
	if parseErr != nil {
		return base, withStderr(fmt.Errorf("claude: %w", parseErr), result.Stderr)
	}
	base.VendorDurationMS = resp.DurationMS
	base.VendorAPIDurationMS = resp.DurationAPIMS
	base.Usage = adapters.Usage{
		InputTokens:  resp.TotalInputTokens(),
		OutputTokens: resp.Usage.OutputTokens,
		CostUSD:      resp.TotalCostUSD,
		// Deliberately NOT populated: claude's envelope carries no
		// tool-call count. num_turns is the nearest field but counts
		// assistant turns, not tool calls, and mapping one to the other
		// would be a fabricated number in a budget check. Left zero, and
		// checkUsageAgainstBudget now reports max_tool_calls as
		// uncheckable rather than passing it against a phantom 0.
	}
	if resp.IsError {
		return base, fmt.Errorf("claude: %s", resp.Result)
	}

	outDir := filepath.Join(req.WorkspacePath, "output")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return base, fmt.Errorf("claude: creating output dir: %w", err)
	}
	outPath := filepath.Join(outDir, string(req.Stage)+".json")
	if err := os.WriteFile(outPath, []byte(resp.Result), 0o644); err != nil {
		return base, fmt.Errorf("claude: writing output: %w", err)
	}
	base.OutputPath = outPath

	return base, nil
}

// maxStderrInError bounds how much of a failed container's stderr is folded
// into the returned error. Enough to identify the failure, not so much that
// a CLI dumping megabytes makes an error unreadable or bloats telemetry,
// which stores these strings verbatim.
const maxStderrInError = 2000

// withStderr attaches a container's stderr to an error. Empty stderr is
// omitted rather than rendered as "(stderr: )", which reads like the
// container said nothing when in fact nothing was captured.
func withStderr(err error, stderrBytes []byte) error {
	trimmed := strings.TrimSpace(string(stderrBytes))
	if trimmed == "" {
		return err
	}
	if len(trimmed) > maxStderrInError {
		trimmed = trimmed[:maxStderrInError] + "... (truncated)"
	}
	return fmt.Errorf("%w (stderr: %s)", err, trimmed)
}
