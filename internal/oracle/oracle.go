// Package oracle ingests the miner's evaluator-only correlated record.
//
// THIS PACKAGE MUST NEVER BE IMPORTED BY THE REVIEW PATH. The miner's
// export contract marks the directory this reads as "evaluator-only; must
// never be exposed to an engine", and docs/system-design.md section 6.5
// requires the judge to be isolated from reasoners 1-3. Until now that
// isolation held only because no code read the oracle at all; this package
// is the first thing that does, so the guarantee needs teeth. isolation_test.go
// walks the import graph and fails if any package under internal/adapters,
// internal/orchestrator, internal/runner or cmd/bench reaches this one.
//
// WHAT THIS FILE IS NOT. It deliberately implements no matching rule. The
// record supplies graded evidence that a later commit corrected the PR, and
// deciding which reviewer findings that evidence corroborates is backlog 2.3
// - alex's call, because it determines what the benchmark claims. What this
// package provides is the primitive every candidate rule needs: a normalised,
// path-keyed view of the evidence. Choosing how to use it is someone else's
// decision, made explicitly.
package oracle

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Strength orders the three signal tiers the miner emits. The tiers are not
// interchangeable and averaging them would be wrong: a FIXES_SHA reference
// is a maintainer stating that a later commit fixes this one, while
// SAME_FILE_MODIFICATION only says somebody touched the same file later,
// which on a small-file repository is close to no information at all.
type Strength int

const (
	StrengthNone Strength = iota
	StrengthWeak
	StrengthMedium
	StrengthStrong
)

func (s Strength) String() string {
	switch s {
	case StrengthStrong:
		return "strong"
	case StrengthMedium:
		return "medium"
	case StrengthWeak:
		return "weak"
	default:
		return "none"
	}
}

// Record is the subset of the miner's PRCandidateRecord this package reads.
// Fields the evaluator has no use for are left unmodelled rather than
// carried, so that adding a use is a visible change.
//
// Shape confirmed against internal/model/candidate.go and the exporter at
// internal/prospectiveexport/export.go:691 in the miner, and independently
// by a keys-only inspection of all ten sealed reports.
type Record struct {
	SchemaVersion string `json:"schema_version"`

	Original struct {
		Number     int    `json:"number"`
		Repository string `json:"repository"`
	} `json:"original"`

	// Stateful is the MINER'S OWN HEURISTIC, not evidence. It is read only
	// so that a disagreement with the retrospective evidence can be
	// surfaced rather than silently folded in - see Evidence.StatefulConflict.
	Stateful struct {
		Score          float64  `json:"score"`
		Category       string   `json:"category"`
		MatchedSymbols []string `json:"matched_symbols"`
	} `json:"stateful"`

	Retrospective struct {
		SummaryRankScore float64 `json:"summary_rank_score"`

		StrongSignals []struct {
			SignalType   string   `json:"signal_type"`
			SourceRef    string   `json:"source_ref"`
			Confidence   float64  `json:"confidence"`
			ChangedPaths []string `json:"changed_paths"`
		} `json:"strong_signals"`

		MediumSignals []struct {
			SignalType     string   `json:"signal_type"`
			SourceRef      string   `json:"source_ref"`
			FunctionOrPath string   `json:"function_or_path"`
			ChangedPaths   []string `json:"changed_paths"`
		} `json:"medium_signals"`

		WeakSignals []struct {
			SignalType string `json:"signal_type"`
			SourceRef  string `json:"source_ref"`
			FilePath   string `json:"file_path"`
		} `json:"weak_signals"`

		Reversal struct {
			ReversalType string `json:"reversal_type"`
			PathOverlaps []struct {
				Path                                string `json:"path"`
				LineageSupportedRemovedOverlapCount int    `json:"lineage_supported_removed_overlap_count"`
			} `json:"path_overlaps"`
		} `json:"reversal"`
	} `json:"retrospective"`
}

// PathEvidence is everything the record says about one file path. Path is
// the finest granularity available: every line-level field in the record is
// an int count, so there are no line numbers and no line content to key on.
type PathEvidence struct {
	Path string

	// Strength is the strongest signal tier naming this path.
	Strength Strength

	// SignalTypes are the distinct signal types naming it, sorted. Kept
	// because the type carries the meaning - REGRESSION_TEST_ADDED and
	// SAME_FILE_MODIFICATION are both "evidence" and mean very different
	// things.
	SignalTypes []string

	// Symbols are function-or-path values from medium signals that resolve
	// to this path. Usually empty: only medium signals carry a symbol at
	// all, which is why a file+symbol rule is only partially supported.
	Symbols []string

	// ReversalOverlap is how many removed lines the later fix undid on this
	// path, lineage-supported. It is a count, not a location, and it is the
	// only quantity available for ranking paths by how much of the original
	// change the fix actually reverted.
	ReversalOverlap int
}

// Evidence is the normalised, path-keyed view of one case's oracle record.
type Evidence struct {
	CaseID     string
	Repository string
	PRNumber   int

	// MaxStrength is the strongest tier anywhere in the record. StrengthNone
	// means the record carries no corrective signal at all, which is a
	// meaningful state and not an error.
	MaxStrength Strength

	Paths []PathEvidence

	// HeuristicScore and HeuristicCategory are the miner's own guess.
	HeuristicScore    float64
	HeuristicCategory string

	// StatefulConflict is set when the miner's heuristic and its own
	// retrospective evidence point opposite ways: a high heuristic score
	// with no corrective signal, or a strong corrective signal with a LOW
	// score. The evaluator must not quietly average these - one of them is
	// a prediction and the other is an observation, and a case where they
	// disagree is exactly the case a judge should treat as unresolved
	// rather than as a middling number.
	StatefulConflict bool
}

// ByPath returns the evidence for one path, and whether any exists.
//
// This is the coarsest correspondence primitive, and it is NOT a matching
// rule. A "same-file" rule would be this lookup and nothing more - which is
// why same-file is expected to be near-vacuous on a repository whose
// changes touch few files. Finer rules would add a symbol test (supported
// only for medium signals) or a mechanism test (not supported by this record
// at all). Building any of those is backlog 2.3.
func (e Evidence) ByPath(path string) (PathEvidence, bool) {
	for _, p := range e.Paths {
		if p.Path == path {
			return p, true
		}
	}
	return PathEvidence{}, false
}

// Load reads and normalises one correlated-report.json.
//
// It does not validate against a schema, because the miner publishes none
// for this record - the shape is defined by its Go types. An unparseable
// file is an error; a parseable one with no signals is not.
func Load(path, caseID string) (Evidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Evidence{}, fmt.Errorf("oracle: reading %s: %w", path, err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Evidence{}, fmt.Errorf("oracle: parsing %s: %w", path, err)
	}
	if rec.SchemaVersion == "" {
		return Evidence{}, fmt.Errorf("oracle: %s has no schema_version; it is probably not a correlated record", path)
	}
	return normalise(rec, caseID), nil
}

func normalise(rec Record, caseID string) Evidence {
	ev := Evidence{
		CaseID:            caseID,
		Repository:        rec.Original.Repository,
		PRNumber:          rec.Original.Number,
		HeuristicScore:    rec.Stateful.Score,
		HeuristicCategory: rec.Stateful.Category,
	}

	acc := map[string]*PathEvidence{}
	touch := func(path string, s Strength, sigType, symbol string) {
		if path == "" {
			return
		}
		p, ok := acc[path]
		if !ok {
			p = &PathEvidence{Path: path}
			acc[path] = p
		}
		if s > p.Strength {
			p.Strength = s
		}
		if sigType != "" && !contains(p.SignalTypes, sigType) {
			p.SignalTypes = append(p.SignalTypes, sigType)
		}
		if symbol != "" && !contains(p.Symbols, symbol) {
			p.Symbols = append(p.Symbols, symbol)
		}
		if s > ev.MaxStrength {
			ev.MaxStrength = s
		}
	}

	for _, s := range rec.Retrospective.StrongSignals {
		// A strong signal with no changed_paths still raises MaxStrength:
		// the correction is attested even though nothing says where it
		// landed. Dropping it because it has no path would lose the
		// strongest evidence in the record.
		if s.SignalType != "" && len(s.ChangedPaths) == 0 && StrengthStrong > ev.MaxStrength {
			ev.MaxStrength = StrengthStrong
		}
		for _, p := range s.ChangedPaths {
			touch(p, StrengthStrong, s.SignalType, "")
		}
	}
	for _, s := range rec.Retrospective.MediumSignals {
		if s.SignalType != "" && len(s.ChangedPaths) == 0 && s.FunctionOrPath == "" && StrengthMedium > ev.MaxStrength {
			ev.MaxStrength = StrengthMedium
		}
		for _, p := range s.ChangedPaths {
			touch(p, StrengthMedium, s.SignalType, s.FunctionOrPath)
		}
		// function_or_path is exactly that - sometimes a symbol, sometimes
		// a path - so it is recorded as a symbol against every path the
		// signal touched, and additionally as its own key when the signal
		// named no paths. Guessing which of the two it is would be a
		// matching decision, and those are not made here.
		if len(s.ChangedPaths) == 0 {
			touch(s.FunctionOrPath, StrengthMedium, s.SignalType, s.FunctionOrPath)
		}
	}
	for _, s := range rec.Retrospective.WeakSignals {
		touch(s.FilePath, StrengthWeak, s.SignalType, "")
	}

	for _, o := range rec.Retrospective.Reversal.PathOverlaps {
		if o.Path == "" {
			continue
		}
		p, ok := acc[o.Path]
		if !ok {
			// A reversal overlap on a path no signal named is still
			// evidence about that path, so it is kept rather than
			// discarded - at StrengthNone, because no signal tier attested
			// it.
			p = &PathEvidence{Path: o.Path}
			acc[o.Path] = p
		}
		p.ReversalOverlap = o.LineageSupportedRemovedOverlapCount
	}

	for _, p := range acc {
		sort.Strings(p.SignalTypes)
		sort.Strings(p.Symbols)
		ev.Paths = append(ev.Paths, *p)
	}
	sort.Slice(ev.Paths, func(i, j int) bool { return ev.Paths[i].Path < ev.Paths[j].Path })

	// "HIGH heuristic but nothing corrective happened" and "a maintainer
	// said this was fixed but the heuristic rated it LOW" are both
	// disagreements between a prediction and an observation.
	switch {
	case rec.Stateful.Category == "HIGH" && ev.MaxStrength == StrengthNone:
		ev.StatefulConflict = true
	case ev.MaxStrength == StrengthStrong && rec.Stateful.Category == "LOW":
		ev.StatefulConflict = true
	}

	return ev
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
