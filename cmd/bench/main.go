// Command bench runs one case through the full engine-runner pipeline: it
// loads a protocol, sequences the three reasoning stages, and seals the
// result tree - the single command Milestone 1's exit criterion refers to
// (docs/system-design.md section 15): "one command produces a reproducible,
// checksummed A/B/C/evaluation result from synthetic inputs without vendor
// credentials."
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/adapters/claude"
	"github.com/axlev/engine-runner/internal/adapters/codex"
	"github.com/axlev/engine-runner/internal/adapters/fixture"
	"github.com/axlev/engine-runner/internal/orchestrator"
	"github.com/axlev/engine-runner/internal/results"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}
}

type config struct {
	repoRoot      string
	protocolPath  string
	bundleRoot    string
	caseID        string
	runID         string
	fixturesDir   string
	workspaceRoot string
	resultsRoot   string
	adapterImage  string
	agentSetPath  string
}

func parseArgs(args []string) (config, error) {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	var cfg config
	fs.StringVar(&cfg.repoRoot, "repo-root", ".", "repository root, used to resolve the protocol's prompt/schema paths")
	fs.StringVar(&cfg.protocolPath, "protocol", "configs/protocols/pilot-v1.yaml", "path to the protocol YAML file")
	fs.StringVar(&cfg.bundleRoot, "bundle", "", "path to the case's prospective/ directory (required)")
	fs.StringVar(&cfg.caseID, "case-id", "", "case identifier; also selects the FixtureAdapter scenario when an agent config names \"fixture\" (required)")
	fs.StringVar(&cfg.runID, "run-id", "", "run identifier (default: generated from the current time and case-id)")
	fs.StringVar(&cfg.fixturesDir, "fixtures-dir", "fixtures", "fixtures root, used when an agent config names \"fixture\"")
	fs.StringVar(&cfg.workspaceRoot, "workspace-root", defaultWorkspaceRoot(), "root under which fresh per-attempt workspace directories are created; must be outside the repository, since it is bind-mounted into stage containers")
	fs.StringVar(&cfg.resultsRoot, "results-root", "build/results", "root under which the sealed run directory is written")
	fs.StringVar(&cfg.adapterImage, "adapter-image", "", "container image to run the vendor CLI in, used when an agent config names \"claude\" or \"codex\"")
	fs.StringVar(&cfg.agentSetPath, "agents", "fixtures/agents/fixture.yaml", "agent set file: the vendor binding (adapter, model, effort, per-stage budgets) for one arm. One file per arm - see configs/agents/")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.bundleRoot == "" {
		return config{}, fmt.Errorf("-bundle is required")
	}
	if cfg.caseID == "" {
		return config{}, fmt.Errorf("-case-id is required")
	}
	if cfg.runID == "" {
		cfg.runID = time.Now().UTC().Format("20060102T150405Z") + "-" + cfg.caseID
	}
	return cfg, nil
}

// defaultWorkspaceRoot picks a per-attempt workspace location OUTSIDE the
// repository, and that placement is a boundary requirement rather than a
// tidiness preference.
//
// internal/runner refuses to mount /home/alex/repos into a stage container
// (docs/system-design.md section 9: a reasoning stage must never see the
// engine's own source). The old default, build/cache/runs, sat inside the
// repository, so every live run was refused by that guard - correctly. The
// guard is not the thing to relax: a directory that gets bind-mounted into
// a reasoning container has no business living inside the tree the guard
// exists to protect, where one wrong "../" would mount the source.
//
// Results are unaffected and still default under build/: they are written
// by the engine and never mounted into a container.
//
// Honours XDG_CACHE_HOME. Falls back to the system temp dir rather than to
// a repository-relative path, so a machine with no resolvable home cannot
// silently reintroduce the mount that was just forbidden.
func defaultWorkspaceRoot() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "engine-runner", "runs")
	}
	return filepath.Join(os.TempDir(), "engine-runner", "runs")
}

// buildAdapters constructs one adapter per distinct vendor the agent set
// names. This is the entire surface a provider swap touches: pointing
// -agents at a different directory selects different cases here with no
// other code change, which is Milestone 2's exit criterion ("swapping
// providers requires configuration changes only") made concrete.
//
// An agent set may name different vendors for different stages, so this
// returns a map rather than a single adapter.
func buildAdapters(agentSet orchestrator.AgentSet, cfg config) (map[string]adapters.AgentAdapter, error) {
	built := map[string]adapters.AgentAdapter{}
	for _, name := range agentSet.Adapters() {
		var (
			a   adapters.AgentAdapter
			err error
		)
		switch name {
		case "fixture":
			a, err = fixture.New(cfg.fixturesDir)
		case "claude":
			a, err = claude.New(cfg.adapterImage, nil)
		case "codex":
			a, err = codex.New(cfg.adapterImage, nil)
		default:
			return nil, fmt.Errorf("adapter %q is not implemented", name)
		}
		if err != nil {
			return nil, fmt.Errorf("constructing adapter %q: %w", name, err)
		}
		built[name] = a
	}
	return built, nil
}

// run executes one case end to end and always attempts to seal whatever
// result it produced, even a failed one - a failed run is still a run
// worth recording, per docs/system-design.md section 12. It returns a
// non-nil error whenever the case's run itself did not complete, even
// though results were successfully written; main() uses that to set the
// process exit code.
func run(args []string, stdout io.Writer) error {
	cfg, err := parseArgs(args)
	if err != nil {
		return err
	}

	protocol, err := orchestrator.LoadProtocol(cfg.protocolPath)
	if err != nil {
		return fmt.Errorf("loading protocol: %w", err)
	}

	agentSet, err := orchestrator.LoadAgentSet(cfg.agentSetPath)
	if err != nil {
		return fmt.Errorf("loading agent set: %w", err)
	}

	built, err := buildAdapters(agentSet, cfg)
	if err != nil {
		return err
	}

	o, err := orchestrator.New(orchestrator.Options{
		RepoRoot:      cfg.repoRoot,
		ProtocolPath:  cfg.protocolPath,
		Protocol:      protocol,
		AgentSetPath:  cfg.agentSetPath,
		AgentSet:      agentSet,
		Adapters:      built,
		WorkspaceRoot: cfg.workspaceRoot,
		// Budget bounds that could not be checked (an adapter reporting no
		// usage) surface here rather than passing silently - a limit
		// enforced on one vendor and skipped on another must be visible.
		Warn: func(msg string) { fmt.Fprintln(os.Stderr, "bench: warning:", msg) },
	})
	if err != nil {
		return fmt.Errorf("constructing orchestrator: %w", err)
	}

	// Ctrl+C must cancel the run, not kill the process. Without this, an
	// interrupt terminated bench while the vendor container kept running
	// and kept billing - it had to be killed by hand - and every attempt
	// already completed died with the process instead of being sealed.
	//
	// Cancellation propagates: the orchestrator stops retrying, the runner
	// docker-kills the live container, and the seal below still writes a
	// result recording exactly how far the run got. A second interrupt
	// bypasses all of that via the default handler, so a wedged run can
	// still be killed outright.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	outcome, runErr := o.Run(ctx, cfg.runID, cfg.caseID, cfg.bundleRoot)
	if ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "bench: interrupted; sealing what completed so far")
	}

	w, err := results.NewWriter(cfg.resultsRoot)
	if err != nil {
		return fmt.Errorf("constructing results writer: %w", err)
	}
	runDir, writeErr := w.WriteRun(outcome)
	if writeErr != nil {
		return fmt.Errorf("writing results: %w", writeErr)
	}

	fmt.Fprintf(stdout, "run %s: %s\nresults: %s\n", outcome.RunID, outcome.Status, runDir)

	if runErr != nil {
		return fmt.Errorf("run did not complete: %w", runErr)
	}
	return nil
}
