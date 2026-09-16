// Package batch runs one H1 arm across a cohort: resumable, bounded
// concurrency, probe-gated, and honest about stalls.
//
// Three properties matter more than throughput here.
//
// Resumability is by SEALED RESULT, not by a progress file. A case counts
// as done when a run directory exists for it whose protocol_version and
// protocol_hash match the arm being run - so a crash, a killed terminal or
// an exhausted rate-limit window costs nothing, and re-running the command
// is always safe. A progress file would be a second source of truth that
// can disagree with the results tree.
//
// Probe gating is a refusal, not a filter. A case the A11 probe voided must
// not run at all: paying for a review of a case the model already knew the
// answer to produces a number that cannot be used. A case whose probe is
// UNRESOLVED - mechanically clean but not yet read by the evaluator - is
// also refused, because "clean scan" is not "clean" (see internal/probe).
//
// A stall is not a failure. Under a subscription token the vendor
// rate-limits rather than bills, so exhausting the window is an expected
// operational state that resolves by waiting. Counting it as a failure
// would silently turn "come back in an hour" into a missing case in the
// cohort, which is exactly the kind of gap that quietly biases a result.
// Stalls are recorded per case with the times, retried, and reported
// separately from failures in every summary.
package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Arm is one protocol to run across the cohort.
type Arm struct {
	Label        string // "T", "G"
	ProtocolPath string
	ProtocolHash string // resolved by the caller; "" disables hash matching
	Version      string // protocol_version, e.g. h1-t-v1
}

// CaseState is what happened to one case in one arm.
type CaseState struct {
	CaseID    string     `json:"case_id"`
	Arm       string     `json:"arm"`
	Status    string     `json:"status"` // done, skipped-probe, failed, stalled
	RunID     string     `json:"run_id,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	CostUSD   float64    `json:"cost_usd_estimate,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`

	// StalledAt/ResumedAt record rate-limit waits. A case can stall more
	// than once; each wait is appended, so the record shows how much of
	// the wall clock was the vendor's window rather than the work.
	StalledAt []time.Time `json:"stalled_at,omitempty"`
	ResumedAt []time.Time `json:"resumed_at,omitempty"`
}

// Statuses.
const (
	StatusDone         = "done"
	StatusAlreadyDone  = "already-done"
	StatusSkippedProbe = "skipped-probe"
	StatusFailed       = "failed"
	StatusStalled      = "stalled"
)

// rateLimitPatterns match a vendor rate-limit refusal in an adapter error
// or its stderr.
//
// CAVEAT, stated because it bounds what this code can promise: these
// patterns are written from the documented and commonly observed phrasings,
// NOT verified against a real subscription stall - no stall has occurred on
// this project yet. If the CLI words it differently, a stall will be
// recorded as a failure, which is the safe direction (visible and
// re-runnable) but still wrong. The first real stall should be used to
// confirm or correct this list; Classify records the matched text so that
// is possible after the fact.
var rateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)rate[ _-]?limit`),
	regexp.MustCompile(`(?i)\b429\b`),
	regexp.MustCompile(`(?i)too many requests`),
	regexp.MustCompile(`(?i)usage limit`),
	regexp.MustCompile(`(?i)quota (?:exceeded|exhausted)`),
	regexp.MustCompile(`(?i)try again (?:in|after)`),
	regexp.MustCompile(`(?i)capacity constraints`),
	regexp.MustCompile(`(?i)overloaded`),
}

// resetHintPatterns pull a wait hint out of the same text.
var resetHintPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)try again in (\d+)\s*(second|minute|hour)s?`),
	regexp.MustCompile(`(?i)retry[- ]after:?\s*(\d+)`),
	regexp.MustCompile(`(?i)resets? (?:at|in) (\d+)\s*(second|minute|hour)s?`),
}

// IsRateLimited reports whether an error reads as a vendor rate limit, and
// returns the matched text so a real stall can be checked against the
// patterns above.
func IsRateLimited(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	msg := err.Error()
	for _, p := range rateLimitPatterns {
		if m := p.FindString(msg); m != "" {
			return m, true
		}
	}
	return "", false
}

// ResetHint extracts a suggested wait from an error, if it names one.
func ResetHint(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	msg := err.Error()
	for _, p := range resetHintPatterns {
		m := p.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		var n int
		if _, e := fmt.Sscanf(m[1], "%d", &n); e != nil || n <= 0 {
			continue
		}
		unit := time.Second
		if len(m) > 2 {
			switch strings.ToLower(m[2]) {
			case "minute":
				unit = time.Minute
			case "hour":
				unit = time.Hour
			}
		}
		return time.Duration(n) * unit, true
	}
	return 0, false
}

// SealedRuns maps case id -> run id for runs already sealed under this arm.
// A run counts only if its protocol_version matches and, when the caller
// supplied a hash, its protocol_hash matches too: a case reviewed under an
// older version of the prompt is not this arm's case.
func SealedRuns(resultsRoot string, arm Arm) (map[string]string, error) {
	out := map[string]string{}
	manifests, _ := filepath.Glob(filepath.Join(resultsRoot, "*", "run.json"))
	sort.Strings(manifests)
	for _, m := range manifests {
		raw, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var run struct {
			RunID           string `json:"run_id"`
			CaseID          string `json:"case_id"`
			ProtocolVersion string `json:"protocol_version"`
			Status          string `json:"status"`
			Fingerprints    struct {
				ProtocolHash string `json:"protocol_hash"`
			} `json:"fingerprints"`
		}
		if json.Unmarshal(raw, &run) != nil {
			continue
		}
		if run.Status != "completed" || run.ProtocolVersion != arm.Version {
			continue
		}
		if arm.ProtocolHash != "" && run.Fingerprints.ProtocolHash != arm.ProtocolHash {
			continue
		}
		out[run.CaseID] = run.RunID
	}
	return out, nil
}

// ProbeGate decides whether a case may run. Returns a reason when it may
// not.
type ProbeGate struct {
	Voided     map[string]bool
	Unresolved map[string]bool
}

// Refuse reports why a case must not run, or "" if it may.
func (g ProbeGate) Refuse(caseID string) string {
	switch {
	case g.Voided[caseID]:
		return "probe voided this case (the model already knew the fix); it is excluded from every arm"
	case g.Unresolved[caseID]:
		return "probe is UNRESOLVED: mechanically clean but no evaluator verdict yet, and a clean scan is not an acquittal"
	}
	return ""
}

// RunCase is the work function: it runs one case and returns its run id and
// cost estimate. Injected so the batch logic is testable without a vendor.
type RunCase func(ctx context.Context, arm Arm, caseID, bundlePath, runID string) (runIDOut string, cost float64, err error)

// Options configure a batch.
type Options struct {
	Arms        []Arm
	Cases       []string            // case ids, in order
	BundlePath  func(string) string // case id -> bundle dir
	ResultsRoot string
	Gate        ProbeGate
	Concurrency int
	Run         RunCase

	// MaxStallRetries bounds how many times one case waits out a rate
	// limit before being recorded as stalled and left for the next run.
	// The default is small on purpose: a case that has stalled repeatedly
	// is telling you the window is exhausted, and the right response is to
	// stop and come back, not to keep a machine spinning overnight.
	MaxStallRetries int
	// StallWait is used when the vendor names no reset time.
	StallWait time.Duration
	// Sleep is injectable so tests do not wait.
	Sleep func(time.Duration)
	// Progress is called after each case completes.
	Progress func(Summary)
}

// Summary is the one-line progress state.
type Summary struct {
	Arm       string  `json:"arm"`
	Done      int     `json:"done"`
	Running   int     `json:"running"`
	Stalled   int     `json:"stalled"`
	Skipped   int     `json:"skipped"`
	Failed    int     `json:"failed"`
	Remaining int     `json:"remaining"`
	CostUSD   float64 `json:"cost_usd_estimate"`
}

func (s Summary) String() string {
	return fmt.Sprintf("arm %s: done %d, running %d, stalled %d, skipped %d, failed %d, remaining %d, cost estimate $%.2f",
		s.Arm, s.Done, s.Running, s.Stalled, s.Skipped, s.Failed, s.Remaining, s.CostUSD)
}

// Result is the whole batch's record.
type Result struct {
	SchemaVersion string      `json:"schema_version"`
	StartedAt     time.Time   `json:"started_at"`
	EndedAt       time.Time   `json:"ended_at"`
	Cases         []CaseState `json:"cases"`
	Summaries     []Summary   `json:"summaries"`
	CostUSD       float64     `json:"cost_usd_estimate_total"`
	Note          string      `json:"note"`
}

// Run executes every arm in order, and every case within an arm with
// bounded concurrency. Arms run strictly in sequence: if the window runs
// dry part-way, a complete first arm is worth far more than two half arms,
// because a half arm cannot be compared with anything.
func Run(ctx context.Context, o Options) (Result, error) {
	if o.Concurrency < 1 {
		o.Concurrency = 1
	}
	if o.MaxStallRetries < 1 {
		o.MaxStallRetries = 3
	}
	if o.StallWait <= 0 {
		o.StallWait = 5 * time.Minute
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}

	res := Result{
		SchemaVersion: "h1-batch/v1",
		StartedAt:     time.Now().UTC(),
		Note:          "cost figures are the CLI's estimates under a subscription token, not amounts billed",
	}

	for _, arm := range o.Arms {
		sealed, err := SealedRuns(o.ResultsRoot, arm)
		if err != nil {
			return res, err
		}

		var mu sync.Mutex
		sum := Summary{Arm: arm.Label}
		var todo []string
		for _, id := range o.Cases {
			if runID, ok := sealed[id]; ok {
				sum.Done++
				res.Cases = append(res.Cases, CaseState{CaseID: id, Arm: arm.Label, Status: StatusAlreadyDone, RunID: runID})
				continue
			}
			if reason := o.Gate.Refuse(id); reason != "" {
				sum.Skipped++
				res.Cases = append(res.Cases, CaseState{CaseID: id, Arm: arm.Label, Status: StatusSkippedProbe, Reason: reason})
				continue
			}
			todo = append(todo, id)
		}
		sum.Remaining = len(todo)
		if o.Progress != nil {
			o.Progress(sum)
		}

		sem := make(chan struct{}, o.Concurrency)
		var wg sync.WaitGroup
		for _, id := range todo {
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			default:
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(caseID string) {
				defer wg.Done()
				defer func() { <-sem }()

				mu.Lock()
				sum.Running++
				mu.Unlock()

				st := runOne(ctx, o, arm, caseID)

				mu.Lock()
				sum.Running--
				sum.Remaining--
				switch st.Status {
				case StatusDone:
					sum.Done++
				case StatusStalled:
					sum.Stalled++
				default:
					sum.Failed++
				}
				sum.CostUSD += st.CostUSD
				res.Cases = append(res.Cases, st)
				res.CostUSD += st.CostUSD
				snapshot := sum
				mu.Unlock()

				if o.Progress != nil {
					o.Progress(snapshot)
				}
			}(id)
		}
		wg.Wait()

		mu.Lock()
		final := sum
		mu.Unlock()
		res.Summaries = append(res.Summaries, final)
		if o.Progress != nil {
			o.Progress(final)
		}
	}

	res.EndedAt = time.Now().UTC()
	sort.Slice(res.Cases, func(i, j int) bool {
		if res.Cases[i].Arm != res.Cases[j].Arm {
			return res.Cases[i].Arm < res.Cases[j].Arm
		}
		return res.Cases[i].CaseID < res.Cases[j].CaseID
	})
	return res, nil
}

// runOne runs a single case, waiting out rate limits.
func runOne(ctx context.Context, o Options, arm Arm, caseID string) CaseState {
	st := CaseState{CaseID: caseID, Arm: arm.Label}
	started := time.Now().UTC()
	st.StartedAt = &started

	for attempt := 0; ; attempt++ {
		runID := fmt.Sprintf("%s-%s", strings.ToLower(arm.Label), caseID)
		if attempt > 0 {
			runID = fmt.Sprintf("%s-retry%d", runID, attempt)
		}
		outID, cost, err := o.Run(ctx, arm, caseID, o.BundlePath(caseID), runID)
		st.CostUSD += cost
		if err == nil {
			ended := time.Now().UTC()
			st.Status, st.RunID, st.EndedAt = StatusDone, outID, &ended
			return st
		}

		matched, limited := IsRateLimited(err)
		if !limited {
			ended := time.Now().UTC()
			st.Status, st.Reason, st.EndedAt = StatusFailed, err.Error(), &ended
			return st
		}

		// A stall. Record it, wait, and try again - never counted as a
		// failure or a cap kill.
		st.StalledAt = append(st.StalledAt, time.Now().UTC())
		if attempt+1 >= o.MaxStallRetries {
			ended := time.Now().UTC()
			st.Status = StatusStalled
			st.Reason = fmt.Sprintf("rate limited (%q) after %d attempts; left for the next run, which will pick it up because nothing was sealed", matched, attempt+1)
			st.EndedAt = &ended
			return st
		}
		wait := o.StallWait
		if hinted, ok := ResetHint(err); ok {
			wait = hinted
		}
		o.Sleep(wait)
		st.ResumedAt = append(st.ResumedAt, time.Now().UTC())
	}
}
