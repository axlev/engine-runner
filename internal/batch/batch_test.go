package batch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func armT() Arm {
	return Arm{Label: "T", ProtocolPath: "p", Version: "h1-t-v1", ProtocolHash: "hash-t"}
}

func opts(run RunCase, cases ...string) Options {
	return Options{
		Arms:        []Arm{armT()},
		Cases:       cases,
		BundlePath:  func(id string) string { return "/bundles/" + id },
		Concurrency: 2,
		Run:         run,
		Sleep:       func(time.Duration) {},
	}
}

func TestRunsEveryCaseAndSums(t *testing.T) {
	var n int32
	r, err := Run(context.Background(), opts(func(_ context.Context, _ Arm, id, _, runID string) (string, float64, error) {
		atomic.AddInt32(&n, 1)
		return runID, 1.25, nil
	}, "c1", "c2", "c3"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || len(r.Cases) != 3 {
		t.Fatalf("ran %d cases, recorded %d", n, len(r.Cases))
	}
	if r.CostUSD != 3.75 {
		t.Errorf("cost %v", r.CostUSD)
	}
	for _, c := range r.Cases {
		if c.Status != StatusDone || c.RunID == "" || c.StartedAt == nil || c.EndedAt == nil {
			t.Errorf("%+v", c)
		}
	}
	if len(r.Summaries) != 1 || r.Summaries[0].Done != 3 || r.Summaries[0].Remaining != 0 {
		t.Errorf("%+v", r.Summaries)
	}
}

func TestConcurrencyIsBounded(t *testing.T) {
	var mu sync.Mutex
	var cur, max int
	o := opts(func(_ context.Context, _ Arm, id, _, runID string) (string, float64, error) {
		mu.Lock()
		cur++
		if cur > max {
			max = cur
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		cur--
		mu.Unlock()
		return runID, 0, nil
	}, "c1", "c2", "c3", "c4", "c5", "c6")
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if max > 2 {
		t.Errorf("concurrency reached %d, want <= 2", max)
	}
}

// A stall is a stall: waited out, retried, never a failure, and the times
// recorded so the vendor's window is separable from the work.
func TestRateLimitStallsAndResumes(t *testing.T) {
	var calls int32
	r, err := Run(context.Background(), opts(func(_ context.Context, _ Arm, id, _, runID string) (string, float64, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return "", 0, errors.New("claude: API error: 429 rate_limit_error; try again in 30 minutes")
		}
		return runID, 2, nil
	}, "c1"))
	if err != nil {
		t.Fatal(err)
	}
	c := r.Cases[0]
	if c.Status != StatusDone {
		t.Fatalf("a waited-out stall must end done, got %+v", c)
	}
	if len(c.StalledAt) != 1 || len(c.ResumedAt) != 1 {
		t.Errorf("stall not recorded: %+v", c)
	}
	if r.Summaries[0].Failed != 0 || r.Summaries[0].Done != 1 {
		t.Errorf("a stall must not count as a failure: %+v", r.Summaries[0])
	}
}

func TestPersistentStallIsRecordedNotFailed(t *testing.T) {
	o := opts(func(_ context.Context, _ Arm, id, _, runID string) (string, float64, error) {
		return "", 0, errors.New("usage limit reached for your plan")
	}, "c1")
	o.MaxStallRetries = 2
	r, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	c := r.Cases[0]
	if c.Status != StatusStalled {
		t.Fatalf("want stalled, got %+v", c)
	}
	if !strings.Contains(c.Reason, "nothing was sealed") {
		t.Errorf("the reason must say it is resumable: %q", c.Reason)
	}
	if r.Summaries[0].Stalled != 1 || r.Summaries[0].Failed != 0 {
		t.Errorf("stalls and failures must be separate: %+v", r.Summaries[0])
	}
}

func TestRealFailureIsAFailure(t *testing.T) {
	r, err := Run(context.Background(), opts(func(_ context.Context, _ Arm, id, _, runID string) (string, float64, error) {
		return "", 0.5, errors.New("stage reasoner-1: exceeded max_cost_usd: used 6.2000, limit 6.0000")
	}, "c1"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Cases[0].Status != StatusFailed {
		t.Errorf("a cap kill is a failure, not a stall: %+v", r.Cases[0])
	}
	if r.Cases[0].CostUSD != 0.5 {
		t.Errorf("a failed attempt still cost something: %+v", r.Cases[0])
	}
}

func TestRateLimitDetection(t *testing.T) {
	limited := []string{
		"API error: 429 Too Many Requests",
		"rate_limit_error",
		"rate limit exceeded",
		"usage limit reached",
		"quota exceeded for this window",
		"Please try again in 45 seconds",
		"upstream capacity constraints",
		"server overloaded",
	}
	for _, m := range limited {
		if _, ok := IsRateLimited(errors.New(m)); !ok {
			t.Errorf("should read as rate limited: %q", m)
		}
	}
	for _, m := range []string{
		"exceeded max_cost_usd: used 6.2000, limit 6.0000",
		"evidence: cites x.c:30-31, outside a 21-line file",
		"context deadline exceeded",
	} {
		if _, ok := IsRateLimited(errors.New(m)); ok {
			t.Errorf("must NOT read as rate limited: %q", m)
		}
	}
	if IsRateLimitedNil() {
		t.Error("nil error is not a rate limit")
	}
}

func IsRateLimitedNil() bool { _, ok := IsRateLimited(nil); return ok }

func TestResetHint(t *testing.T) {
	cases := map[string]time.Duration{
		"try again in 30 minutes": 30 * time.Minute,
		"try again in 45 seconds": 45 * time.Second,
		"try again in 2 hours":    2 * time.Hour,
		"retry-after: 90":         90 * time.Second,
	}
	for msg, want := range cases {
		got, ok := ResetHint(errors.New(msg))
		if !ok || got != want {
			t.Errorf("%q: got %v (%v), want %v", msg, got, ok, want)
		}
	}
	if _, ok := ResetHint(errors.New("rate limited")); ok {
		t.Error("no hint should be reported when none is named")
	}
}

// Resumability is by sealed result: a case already reviewed under this
// exact protocol is not re-run and not re-paid for.
func TestResumeSkipsSealedRunsForThisArmOnly(t *testing.T) {
	root := t.TempDir()
	seal := func(runID, caseID, version, hash, status string) {
		d := filepath.Join(root, runID)
		_ = os.MkdirAll(d, 0o755)
		body := fmt.Sprintf(`{"run_id":%q,"case_id":%q,"protocol_version":%q,"status":%q,"fingerprints":{"protocol_hash":%q}}`,
			runID, caseID, version, status, hash)
		_ = os.WriteFile(filepath.Join(d, "run.json"), []byte(body), 0o644)
	}
	seal("t-c1", "c1", "h1-t-v1", "hash-t", "completed")   // this arm: skip
	seal("g-c2", "c2", "h1-g-v1", "hash-g", "completed")   // other arm: run
	seal("t-c3", "c3", "h1-t-v1", "old-hash", "completed") // stale prompt: run
	seal("t-c4", "c4", "h1-t-v1", "hash-t", "failed")      // not completed: run

	var ran []string
	var mu sync.Mutex
	o := opts(func(_ context.Context, _ Arm, id, _, runID string) (string, float64, error) {
		mu.Lock()
		ran = append(ran, id)
		mu.Unlock()
		return runID, 0, nil
	}, "c1", "c2", "c3", "c4")
	o.ResultsRoot = root
	o.Concurrency = 1
	r, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, ",") != "c2,c3,c4" {
		t.Errorf("ran %v, want c2,c3,c4 (c1 already sealed under this arm)", ran)
	}
	var already int
	for _, c := range r.Cases {
		if c.Status == StatusAlreadyDone {
			already++
		}
	}
	if already != 1 {
		t.Errorf("want 1 already-done, got %d", already)
	}
}

// The probe gate refuses; it does not quietly filter.
func TestProbeGateRefusesVoidedAndUnresolved(t *testing.T) {
	var ran []string
	var mu sync.Mutex
	o := opts(func(_ context.Context, _ Arm, id, _, runID string) (string, float64, error) {
		mu.Lock()
		ran = append(ran, id)
		mu.Unlock()
		return runID, 0, nil
	}, "clean", "voided", "unresolved")
	o.Gate = ProbeGate{
		Voided:     map[string]bool{"voided": true},
		Unresolved: map[string]bool{"unresolved": true},
	}
	r, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ran, ",") != "clean" {
		t.Errorf("only the clean case may run, ran %v", ran)
	}
	reasons := map[string]string{}
	for _, c := range r.Cases {
		if c.Status == StatusSkippedProbe {
			reasons[c.CaseID] = c.Reason
		}
	}
	if len(reasons) != 2 {
		t.Fatalf("want 2 skipped, got %+v", reasons)
	}
	if !strings.Contains(reasons["voided"], "already knew") {
		t.Errorf("voided reason: %q", reasons["voided"])
	}
	if !strings.Contains(reasons["unresolved"], "not an acquittal") {
		t.Errorf("unresolved reason: %q", reasons["unresolved"])
	}
}

// Arms run in sequence: a dry window leaves one complete arm, not two
// halves.
func TestArmsRunInOrder(t *testing.T) {
	var order []string
	var mu sync.Mutex
	o := opts(func(_ context.Context, a Arm, id, _, runID string) (string, float64, error) {
		mu.Lock()
		order = append(order, a.Label+":"+id)
		mu.Unlock()
		return runID, 0, nil
	}, "c1", "c2")
	o.Concurrency = 1
	o.Arms = []Arm{
		{Label: "T", Version: "h1-t-v1"},
		{Label: "G", Version: "h1-g-v1"},
	}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(order, ",")
	if !strings.HasPrefix(joined, "T:") || !strings.Contains(joined, "G:") {
		t.Fatalf("order %v", order)
	}
	firstG := strings.Index(joined, "G:")
	if strings.LastIndex(joined, "T:") > firstG {
		t.Errorf("a T case ran after a G case started: %v", order)
	}
}
