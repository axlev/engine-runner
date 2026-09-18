package main

import (
	"testing"
	"time"
)

// The verbatim message from the first real subscription stall this project
// has seen. If either half stops working - classification or the reset
// time - a rate limit becomes a failure again and the run burns the rest
// of the cohort producing them.
const realStall = `codex: You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), ` +
	`visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 2:30 PM.: ` +
	`exit status 1 (stderr: Reading prompt from stdin...)`

func TestRealStallIsClassifiedAsRateLimited(t *testing.T) {
	matched, ok := isStall(realStall)
	if !ok {
		t.Fatal("a real usage limit was not recognised as a stall; it would be sealed as a failure")
	}
	if matched == "" {
		t.Error("no matched text recorded, so the pattern cannot be checked after the fact")
	}
	if _, ok := isStall("codex: exit status 1 (stderr: )"); ok {
		t.Error("an ordinary failure was treated as a stall and would be retried forever")
	}
	if _, ok := isStall(""); ok {
		t.Error("an empty error was treated as a stall")
	}
}

func TestStallWaitReadsTheClockReset(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 46, 0, 0, time.Local)
	got := stallWait(realStall, now, 20*time.Minute)
	// 12:46 -> 14:30 is 1h44m, plus the cushion.
	if got < 104*time.Minute || got > 105*time.Minute {
		t.Errorf("wait = %v, want about 1h44m", got)
	}
}

// The stated reset carries no timezone and the account's need not match
// this host's. Observed: "try again at 2:30 PM" was still returned at 14:45
// local, so the reset was in some other zone. The clock is therefore a HINT,
// used only when it lands plausibly ahead of now, and the fallback covers
// everything else.
func TestStallWaitTreatsThePastAsUnknownRatherThanImminent(t *testing.T) {
	// A reset already behind the clock says nothing except that the hint
	// is not in our timezone. Returning the one-minute floor for it
	// produced a retry every minute against a still-exhausted quota.
	now := time.Date(2026, 9, 18, 14, 45, 0, 0, time.Local)
	if d := stallWait("try again at 2:30 PM", now, 20*time.Minute); d != 20*time.Minute {
		t.Errorf("a passed reset gave %v; it must fall back, not busy-loop", d)
	}
	// Nor may it wrap to tomorrow: a wrong-timezone hint would then cost
	// most of a day.
	now2 := time.Date(2026, 9, 18, 23, 0, 0, 0, time.Local)
	if d := stallWait("try again at 12:15 AM", now2, 20*time.Minute); d != 20*time.Minute {
		t.Errorf("a wrapped reset gave %v; an untrusted hint must not cost a day", d)
	}
	// A plausible future time in this zone is still used.
	now3 := time.Date(2026, 9, 18, 12, 46, 0, 0, time.Local)
	if d := stallWait("try again at 2:30 PM", now3, 20*time.Minute); d < 104*time.Minute || d > 105*time.Minute {
		t.Errorf("a future reset = %v, want about 1h44m", d)
	}
}

func TestStallWaitFallsBackWhenNoResetIsGiven(t *testing.T) {
	if d := stallWait("429 too many requests", time.Now(), 20*time.Minute); d != 20*time.Minute {
		t.Errorf("fallback = %v, want 20m", d)
	}
	if d := stallWait("try again at 99:99", time.Now(), 20*time.Minute); d != 20*time.Minute {
		t.Errorf("an unparseable time must fall back, got %v", d)
	}
}
