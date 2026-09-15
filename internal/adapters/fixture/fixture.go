// Package fixture implements FixtureAdapter, the deterministic test double
// behind AgentAdapter used to build and test engine-runner without invoking
// a real LLM. See docs/system-design.md section 8.1.
//
// FixtureAdapter never performs network I/O or reads credentials: every
// response it can return is a byte-for-byte copy of a file committed under
// fixtures/, selected entirely by RunRequest.CaseID (the scenario name),
// RunRequest.Stage, and an optional attempt number. It does not parse or
// validate response content - a fixture can and does deliberately return
// malformed JSON or schema-violating output, because that content-level
// validation belongs to the engine, not the adapter.
package fixture

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
)

const adapterName = "fixture"
const adapterVersion = "fixture/v1"

// FixtureAdapter is the deterministic AgentAdapter implementation described
// in section 8.1. It is safe for concurrent use.
type FixtureAdapter struct {
	dir       string
	scenarios map[string]*scenarioFile
	// override, when set, selects this scenario for every request instead
	// of the request's CaseID. It exists for dry runs over REAL bundles:
	// the case id then belongs to a mined case with no canned responses,
	// and what is being exercised is everything around the model call -
	// boundary validation, checksums, metadata, handoffs, sealing.
	override string

	mu    sync.Mutex
	calls []adapters.RunRequest
}

// New loads every scenario under dir/scenarios/*.json. dir is the fixtures
// root (e.g. "fixtures" at the repository root), not a scenario-specific
// path.
func New(dir string) (*FixtureAdapter, error) {
	scenarios, err := loadScenarios(dir)
	if err != nil {
		return nil, err
	}
	return &FixtureAdapter{dir: dir, scenarios: scenarios}, nil
}

// WithScenario returns the adapter with every request routed to the named
// scenario regardless of case id. Fails closed on an unknown name so a
// typo cannot become "every case silently ran the wrong canned answer".
func (f *FixtureAdapter) WithScenario(name string) (*FixtureAdapter, error) {
	if _, ok := f.scenarios[name]; !ok {
		return nil, fmt.Errorf("fixture: no scenario named %q to use as override", name)
	}
	f.override = name
	return f, nil
}

func (f *FixtureAdapter) Name() string { return adapterName }

func (f *FixtureAdapter) Version(ctx context.Context) (string, error) {
	return adapterVersion, nil
}

// Calls returns every RunRequest this adapter has received, in order. It
// exists purely for test introspection - e.g. asserting that a stage's
// WorkspacePath contained only its allowed mounts - and has no bearing on
// FixtureAdapter's own behavior, which depends only on the current request.
func (f *FixtureAdapter) Calls() []adapters.RunRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]adapters.RunRequest, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *FixtureAdapter) Run(ctx context.Context, req adapters.RunRequest) (adapters.RunResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()

	key := req.CaseID
	if f.override != "" {
		key = f.override
	}
	scenario, ok := f.scenarios[key]
	if !ok {
		return adapters.RunResult{}, fmt.Errorf("fixture: no scenario registered for case %q", req.CaseID)
	}
	stage, ok := scenario.Stages[string(req.Stage)]
	if !ok {
		return adapters.RunResult{}, fmt.Errorf("fixture: scenario %q defines no behavior for stage %q", scenario.Name, req.Stage)
	}

	attempt := req.Environment[adapters.AttemptEnvKey]
	if attempt == "" {
		attempt = "1"
	}
	behavior, usage := stage.behaviorForAttempt(attempt)

	return f.execute(ctx, req, behavior, usage, time.Now())
}

func (f *FixtureAdapter) execute(ctx context.Context, req adapters.RunRequest, behavior behaviorSpec, usage adapters.Usage, started time.Time) (adapters.RunResult, error) {
	switch behavior.Kind {
	case behaviorSuccess:
		return f.runSuccess(req, behavior, usage, started)

	case behaviorExit:
		return adapters.RunResult{
			ExitCode:   behavior.ExitCode,
			StartedAt:  started,
			FinishedAt: time.Now(),
			Usage:      usage,
			Adapter:    adapterName,
			Version:    adapterVersion,
		}, fmt.Errorf("fixture: simulated process exit code %d", behavior.ExitCode)

	case behaviorHang:
		if behavior.Then == nil {
			return adapters.RunResult{}, fmt.Errorf("fixture: hang behavior requires \"then\"")
		}
		delay := time.Duration(behavior.DelayMS) * time.Millisecond
		select {
		case <-ctx.Done():
			return adapters.RunResult{
				StartedAt:  started,
				FinishedAt: time.Now(),
				Usage:      usage,
				Adapter:    adapterName,
				Version:    adapterVersion,
			}, ctx.Err()
		case <-time.After(delay):
			return f.execute(ctx, req, *behavior.Then, usage, started)
		}

	default:
		return adapters.RunResult{}, fmt.Errorf("fixture: unknown behavior kind %q", behavior.Kind)
	}
}

func (f *FixtureAdapter) runSuccess(req adapters.RunRequest, behavior behaviorSpec, usage adapters.Usage, started time.Time) (adapters.RunResult, error) {
	if behavior.ResponsePath == "" {
		return adapters.RunResult{}, fmt.Errorf("fixture: success behavior missing response_path")
	}
	src := filepath.Join(f.dir, behavior.ResponsePath)
	content, err := os.ReadFile(src)
	if err != nil {
		return adapters.RunResult{}, fmt.Errorf("fixture: reading response fixture %q: %w", src, err)
	}

	if req.WorkspacePath == "" {
		return adapters.RunResult{}, fmt.Errorf("fixture: request missing workspace_path")
	}
	outDir := filepath.Join(req.WorkspacePath, "output")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return adapters.RunResult{}, fmt.Errorf("fixture: creating output dir: %w", err)
	}
	outPath := filepath.Join(outDir, string(req.Stage)+".json")
	if err := os.WriteFile(outPath, content, 0o644); err != nil {
		return adapters.RunResult{}, fmt.Errorf("fixture: writing output: %w", err)
	}

	return adapters.RunResult{
		OutputPath: outPath,
		ExitCode:   0,
		StartedAt:  started,
		FinishedAt: time.Now(),
		Usage:      usage,
		Adapter:    adapterName,
		Version:    adapterVersion,
	}, nil
}
