package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/axlev/engine-runner/internal/batch"
)

// clockReset matches the wall-clock form a codex subscription stall uses:
// "You've hit your usage limit. ... try again at 2:30 PM."
var clockReset = regexp.MustCompile(`(?i)try again at (\d{1,2}):(\d{2})\s*(AM|PM)?`)

// stallWait is how long to sleep before retrying a rate-limited call.
//
// A stall is not a failure: under a subscription the vendor rations rather
// than bills, so the case is still runnable, just not yet. Recording it as
// a failure loses the case AND keeps hammering the limit - six consecutive
// "failures" from one usage limit is what that looks like.
//
// The reset arrives as a clock time in the account's own timezone, which
// this host's local time is assumed to match. A time that has already
// passed today is read as tomorrow only if it is more than an hour behind,
// so a slightly stale clock waits minutes rather than a day.
func stallWait(errText string, now time.Time, fallback time.Duration) time.Duration {
	m := clockReset.FindStringSubmatch(errText)
	if m == nil {
		return fallback
	}
	h, err1 := strconv.Atoi(m[1])
	min, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || h > 23 || min > 59 {
		return fallback
	}
	switch {
	case m[3] == "PM" || m[3] == "pm":
		if h != 12 {
			h += 12
		}
	case m[3] == "AM" || m[3] == "am":
		if h == 12 {
			h = 0
		}
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), h, min, 0, 0, now.Location())
	d := target.Sub(now)
	if d < -time.Hour {
		d += 24 * time.Hour
	}
	if d < time.Minute {
		return time.Minute
	}
	return d + 30*time.Second // a small cushion past the stated reset
}

// isStall reports whether this error is the vendor rationing rather than
// something wrong with the run.
func isStall(errText string) (string, bool) {
	if errText == "" {
		return "", false
	}
	return batch.IsRateLimited(errors.New(errText))
}

func fmtWait(d time.Duration) string {
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
