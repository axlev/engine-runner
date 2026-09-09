package orchestrator

import (
	"fmt"

	"github.com/axlev/engine-runner/internal/adapters"
)

// Budget enforcement comes in two kinds, and the difference matters when
// reading a failed run.
//
// PREVENTION stops a stage before it overruns. Only two bounds can be
// prevented, and only one of those by a vendor:
//
//   - max_wall_clock_seconds - enforced by the orchestrator as a context
//     deadline around each attempt. Works for every adapter, because
//     honouring ctx is part of the AgentAdapter contract. Neither vendor CLI
//     has a timeout flag (verified against `claude --help` and
//     `codex exec --help`), so this has to be ours.
//   - max_cost_usd - passed to claude as --max-budget-usd. `codex exec` has
//     no budget flag at all, so a codex stage cannot be cost-bounded before
//     the fact.
//
// DETECTION catches an overrun after it happened. max_input_tokens,
// max_output_tokens and max_tool_calls have no flag on either CLI, so the
// only honest thing left is to check the usage a stage reports and fail it
// if it busted its bound. That does not save the spend - the money is gone -
// but it does keep the protocol's declared limits meaningful: a run that
// exceeded its budget is not silently reported as a clean success.
//
// Where an adapter reports no usage at all, a detection check cannot run.
// That case is reported explicitly rather than passing quietly, because a
// bound that is enforced on one vendor and silently skipped on another is
// worse than one that is honestly labelled unenforced.

// UsageUnavailable describes bounds that could not be checked because the
// adapter reported no usage for them.
type UsageUnavailable struct {
	Adapter string
	Bounds  []string
}

// checkUsageAgainstBudget reports an error when reported usage exceeded a
// declared bound, and separately reports which bounds could not be checked.
//
// A zero in a Usage field is ambiguous - it can mean "used none" or "not
// reported" - so a bound is treated as uncheckable only when the adapter
// reported nothing at all for any field, which is how a non-instrumented
// adapter presents. A partially-instrumented adapter is taken at its word.
func checkUsageAgainstBudget(adapterName string, budget BudgetConfig, usage adapters.Usage) (error, *UsageUnavailable) {
	declared := map[string]int{
		"max_input_tokens":  budget.MaxInputTokens,
		"max_output_tokens": budget.MaxOutputTokens,
		"max_tool_calls":    budget.MaxToolCalls,
	}
	reported := map[string]int{
		"max_input_tokens":  usage.InputTokens,
		"max_output_tokens": usage.OutputTokens,
		"max_tool_calls":    usage.ToolCalls,
	}
	order := []string{"max_input_tokens", "max_output_tokens", "max_tool_calls"}

	// Unavailability is judged PER BOUND, not for the usage block as a
	// whole. The original all-or-nothing test only fired when an adapter
	// reported nothing at all, which missed the commoner and more dangerous
	// case: an adapter that reports some fields and not others. claude
	// reports tokens and cost but has no tool-call count in its envelope,
	// so max_tool_calls was being compared against a phantom 0 and passing
	// vacuously - a declared bound that silently enforced nothing, which is
	// exactly what this function exists to prevent.
	//
	// A zero here means "not reported" rather than "genuinely zero". That
	// conflation is safe in this direction: a stage that truly consumed
	// zero of something cannot have exceeded a positive bound, so the worst
	// case is an unnecessary warning, never a missed overrun.
	var unavailable []string
	for _, name := range order {
		limit := declared[name]
		if limit <= 0 {
			continue // no bound declared, nothing to check
		}
		if reported[name] == 0 {
			unavailable = append(unavailable, name)
			continue
		}
		if reported[name] > limit {
			return fmt.Errorf("exceeded %s: used %d, limit %d", name, reported[name], limit), nil
		}
	}

	if budget.MaxCostUSD > 0 {
		switch {
		case usage.CostUSD == 0:
			unavailable = append(unavailable, "max_cost_usd")
		case usage.CostUSD > budget.MaxCostUSD:
			return fmt.Errorf("exceeded max_cost_usd: used %.4f, limit %.4f", usage.CostUSD, budget.MaxCostUSD), nil
		}
	}

	if len(unavailable) > 0 {
		return nil, &UsageUnavailable{Adapter: adapterName, Bounds: unavailable}
	}
	return nil, nil
}
