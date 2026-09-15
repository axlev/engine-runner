package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/contextbuilder"
)

// A substantiation stage assesses findings; it does not discover them. Under
// H1 that is what keeps arm T "discovery + substantiation" rather than "two
// rounds of discovery" - if B could add findings of its own, the staging
// confound in brief section 2 would be uninterpretable. The prompts have
// always said "copy finding_id verbatim"; this makes the rule software.
//
// The check joins every assessments[].finding_id in a stage's output against
// findings[].id in the documents that stage was handed. Three things are
// fatal and trigger the retry policy: an assessment with no ancestor; a
// CONFIRMED assessment whose discovery_mechanism or mechanism is not the
// ancestor's mechanism as written; a NARROWED assessment whose mechanism is
// not actually different from what it narrows. A handed-over finding with no
// assessment is a warning: the scorer treats it as dropped, and dropping is
// a scoreable act, not a malformed document.
//
// A stage that received nothing, or whose output carries no assessments, is
// out of scope - pilot-v1's reasoner-3 and every discovery stage pass
// through untouched.

type ancestor struct {
	Mechanism string
	HasMech   bool
}

// validateAncestry reads the stage output at outputPath and the prior-stage
// outputs it was handed, and returns a fatal error plus warnings.
func validateAncestry(outputPath string, inputs contextbuilder.StageInputs) (error, []string) {
	if len(inputs.Outputs) == 0 {
		return nil, nil
	}

	var doc struct {
		Assessments []struct {
			FindingID          string  `json:"finding_id"`
			Disposition        string  `json:"disposition"`
			DiscoveryMechanism *string `json:"discovery_mechanism"`
			Mechanism          *string `json:"mechanism"`
		} `json:"assessments"`
	}
	if err := readJSON(outputPath, &doc); err != nil {
		return fmt.Errorf("ancestry: %w", err), nil
	}
	if doc.Assessments == nil {
		return nil, nil
	}

	ancestors := map[string]ancestor{}
	stages := make([]string, 0, len(inputs.Outputs))
	for s := range inputs.Outputs {
		stages = append(stages, string(s))
	}
	sort.Strings(stages)
	for _, s := range stages {
		var prior struct {
			Findings []struct {
				ID        string  `json:"id"`
				Mechanism *string `json:"mechanism"`
			} `json:"findings"`
		}
		if err := readJSON(inputs.Outputs[adapters.Stage(s)], &prior); err != nil {
			return fmt.Errorf("ancestry: reading handed-over %s output: %w", s, err), nil
		}
		for _, f := range prior.Findings {
			a := ancestor{}
			if f.Mechanism != nil {
				a.Mechanism, a.HasMech = *f.Mechanism, true
			}
			ancestors[f.ID] = a
		}
	}

	seen := map[string]bool{}
	for _, as := range doc.Assessments {
		anc, ok := ancestors[as.FindingID]
		if !ok {
			return fmt.Errorf("ancestry: assessment cites finding_id %q, which no handed-over stage produced - a substantiation stage may not introduce findings", as.FindingID), nil
		}
		seen[as.FindingID] = true
		if !anc.HasMech {
			continue // pre-H1 shape (pilot-v1 review-a has no mechanism field)
		}
		if as.DiscoveryMechanism != nil && *as.DiscoveryMechanism != anc.Mechanism {
			return fmt.Errorf("ancestry: assessment of %q restates discovery_mechanism; it must be the discovery stage's mechanism verbatim", as.FindingID), nil
		}
		switch as.Disposition {
		case "CONFIRMED":
			if as.Mechanism != nil && *as.Mechanism != anc.Mechanism {
				return fmt.Errorf("ancestry: assessment of %q is CONFIRMED but its mechanism differs from discovery's - CONFIRMED means the mechanism stands as written; a tighter claim is NARROWED", as.FindingID), nil
			}
		case "NARROWED":
			if as.Mechanism != nil && *as.Mechanism == anc.Mechanism {
				return fmt.Errorf("ancestry: assessment of %q is NARROWED but its mechanism is discovery's unchanged - NARROWED must state the tighter claim", as.FindingID), nil
			}
		}
	}

	var warnings []string
	ids := make([]string, 0, len(ancestors))
	for id := range ancestors {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if !seen[id] {
			warnings = append(warnings, fmt.Sprintf("ancestry: handed-over finding %q has no assessment; the scorer treats it as dropped", id))
		}
	}
	return nil, warnings
}

func readJSON(path string, into interface{}) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}
