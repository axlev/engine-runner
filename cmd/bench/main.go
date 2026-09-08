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
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
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
}

func parseArgs(args []string) (config, error) {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	var cfg config
	fs.StringVar(&cfg.repoRoot, "repo-root", ".", "repository root, used to resolve the protocol's prompt/schema paths")
	fs.StringVar(&cfg.protocolPath, "protocol", "configs/protocols/pilot-v1.yaml", "path to the protocol YAML file")
	fs.StringVar(&cfg.bundleRoot, "bundle", "", "path to the case's prospective/ directory (required)")
	fs.StringVar(&cfg.caseID, "case-id", "", "case identifier; also selects the FixtureAdapter scenario when the protocol's adapter is \"fixture\" (required)")
	fs.StringVar(&cfg.runID, "run-id", "", "run identifier (default: generated from the current time and case-id)")
	fs.StringVar(&cfg.fixturesDir, "fixtures-dir", "fixtures", "fixtures root, used when the protocol's adapter is \"fixture\"")
	fs.StringVar(&cfg.workspaceRoot, "workspace-root", "build/cache/runs", "root under which fresh per-attempt workspace directories are created")
	fs.StringVar(&cfg.resultsRoot, "results-root", "build/results", "root under which the sealed run directory is written")
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

func buildAdapter(protocol *orchestrator.Protocol, cfg config) (adapters.AgentAdapter, error) {
	switch protocol.Adapter {
	case "fixture":
		return fixture.New(cfg.fixturesDir)
	default:
		return nil, fmt.Errorf("adapter %q is not implemented yet (only \"fixture\" is available in this milestone)", protocol.Adapter)
	}
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

	adapter, err := buildAdapter(protocol, cfg)
	if err != nil {
		return fmt.Errorf("constructing adapter: %w", err)
	}

	o, err := orchestrator.New(cfg.repoRoot, cfg.protocolPath, protocol, adapter, cfg.workspaceRoot)
	if err != nil {
		return fmt.Errorf("constructing orchestrator: %w", err)
	}

	outcome, runErr := o.Run(context.Background(), cfg.runID, cfg.caseID, cfg.bundleRoot)

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
