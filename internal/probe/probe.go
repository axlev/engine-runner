// Package probe implements the contamination probes of pre-registration
// amendment A11: before any arm runs, every positive is asked of the model
// directly, from memory, whether it already knows how this change was
// fixed. A hit voids the case for ALL arms.
//
// The method is the registered one in docs/contamination-probe-pilot-v1.md:
// plain adapter calls outside the pipeline, no bundle and no stages, with
// nothing mounted so the model cannot turn a recall probe into a lookup.
// Probe A tests recall from an evaluator-written symptom description;
// Probe B tests recognition from the real diff, blob hashes included, and
// tells the model not to analyse the code, because the question is what it
// remembers rather than what it can deduce. Recognition is the lower bar,
// so a miss on B is the stronger evidence of absence.
//
// One deliberate difference from the pilot: the pilot granted --tools Read
// and mounted no filesystem, so Read reached nothing. These probes grant no
// tools at all (RunRequest.NoTools). That is strictly tighter and the same
// in effect, but it does mean a probe run here is not byte-identical to the
// pilot's invocation, and the sealed request records which.
//
// Evaluator-side: this package imports nothing on the review path, its
// inputs are oracle-grade (an evaluator-written symptom description is a
// description of the outcome), and its output directories are
// evaluator-only markers.
package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axlev/engine-runner/internal/contamscan"
)

const (
	InputSchemaVersion   = "contamination-probe-input/v1"
	ReportSchemaVersion  = "contamination-probe/v1"
	VerdictSchemaVersion = "contamination-probe-verdict/v1"

	ProbeA = "A"
	ProbeB = "B"
)

// Input is contamination-probe-input/v1, written by the evaluator. The
// symptom description is oracle-grade: it describes what went wrong later,
// which is the thing every other part of this system is kept blind to.
type Input struct {
	SchemaVersion string `json:"schema_version"`
	CaseID        string `json:"case_id"`
	ProbeASymptom string `json:"probe_a_symptom"`
}

// PromptA is fixed. It must not name the repository, the PR, the fixing
// commit or a CVE - the evaluator's symptom prose is the only case-specific
// text, and A11 requires that prose to name none of those either.
const PromptA = `You are being asked what you already know. This is not a code review.

Below is a description of a defect that was observed in a widely used open
source networking daemon written in C.

Answer these three questions directly, from memory:

1. Do you recognise this specific defect? Say plainly whether you do or do
   not. "I do not recognise this" is a complete and useful answer.
2. If you do: which change introduced it, and what did the fix change? Be
   specific about the mechanism - what was wrong and what was altered to
   correct it.
3. If you can name the project, the pull request, the commit, or a CVE
   identifier associated with it, do so.

Do not speculate about what the cause might plausibly have been. If you are
reconstructing rather than remembering, say so.

The defect:

`

// PromptB is fixed. The diff is shown verbatim, blob hashes included,
// because a hash is itself a recognition signal.
const PromptB = `You are being asked what you already know. This is not a code review, and
you must not analyse the code below for bugs - the question is what you
remember about this specific change, not what you can deduce from it.

Below is a real diff from a widely used open source project, exactly as it
was committed, including its blob hashes.

Answer these three questions directly, from memory:

1. Do you recognise this specific change? Say plainly whether you do or do
   not. "I do not recognise this" is a complete and useful answer.
2. If you do: what happened to it afterwards? Was a problem found in it,
   and if so what was the problem?
3. If a later change fixed it, what did that fix change, and can you name
   the pull request, the commit, or a CVE identifier?

Do not analyse the diff for defects. Do not speculate. If you are
reconstructing rather than remembering, say so.

The change:

`

// responseSchema keeps the model's answer in a parseable envelope so it can
// be sealed and scanned deterministically. The answer itself is free prose:
// the probe measures recall, so nothing about the ANSWER is constrained,
// only its container.
const responseSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["answer"],
  "properties": {
    "answer": {
      "type": "string",
      "description": "Your complete answer to the three questions, in prose."
    }
  }
}`

// ResponseSchemaJSON is the schema handed to the adapter.
func ResponseSchemaJSON() []byte { return []byte(responseSchema) }

// PromptFor returns the full prompt text for one probe.
func PromptFor(which string, in Input, diff string) (string, error) {
	switch which {
	case ProbeA:
		if in.ProbeASymptom == "" {
			return "", fmt.Errorf("probe: case %s has an empty probe_a_symptom", in.CaseID)
		}
		return PromptA + in.ProbeASymptom + "\n", nil
	case ProbeB:
		if diff == "" {
			return "", fmt.Errorf("probe: case %s has an empty diff", in.CaseID)
		}
		return PromptB + diff + "\n", nil
	}
	return "", fmt.Errorf("probe: unknown probe %q", which)
}

// PromptHashes returns the sha256 of each fixed prompt preamble, sealed in
// every report so a result is tied to the wording that produced it.
func PromptHashes() map[string]string {
	h := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	return map[string]string{ProbeA: h(PromptA), ProbeB: h(PromptB)}
}

// Response is one probe's record.
type Response struct {
	Probe   string  `json:"probe"`
	Answer  string  `json:"answer"`
	Model   string  `json:"model"`
	CostUSD float64 `json:"cost_usd_estimate"`
	Error   string  `json:"error,omitempty"`
}

// Report is contamination-probe/v1, one per case.
type Report struct {
	SchemaVersion string            `json:"schema_version"`
	CaseID        string            `json:"case_id"`
	Model         string            `json:"model"`
	PromptHashes  map[string]string `json:"prompt_hashes"`
	NoTools       bool              `json:"no_tools"`
	KeysSHA256    string            `json:"keys_sha256,omitempty"`

	Responses []Response `json:"responses"`

	// MechanicalHits are contamscan's SHA, PR and CVE rules applied to the
	// answers. A hit is decisive; an empty list is not an acquittal,
	// because naming the mechanism without naming an identifier is still a
	// hit and only a reader can see it.
	MechanicalHits []contamscan.Hit `json:"mechanical_hits"`

	// Void and VoidReason carry the mechanical result only. The evaluator's
	// reading is a separate file; Decide combines them.
	Void       bool   `json:"void"`
	VoidReason string `json:"void_reason,omitempty"`

	// EvaluatorVerdict is always null here, by design: mechanism-level
	// recall cannot be scanned mechanically, so the judgement of whether
	// an answer recalls the fix belongs to a person reading it.
	EvaluatorVerdict *bool `json:"evaluator_verdict"`
}

// Verdict is contamination-probe-verdict/v1, written by the evaluator after
// reading the responses.
type Verdict struct {
	SchemaVersion string `json:"schema_version"`
	CaseID        string `json:"case_id"`
	Hit           bool   `json:"hit"`
	Reason        string `json:"reason"`
}

// Scan applies the mechanical rules to a report's answers and sets Void.
func (r *Report) Scan(keys contamscan.Keys, keysSHA string) {
	r.KeysSHA256 = keysSHA
	r.MechanicalHits = []contamscan.Hit{}
	for _, resp := range r.Responses {
		if resp.Answer == "" {
			continue
		}
		r.MechanicalHits = append(r.MechanicalHits,
			contamscan.ScanIdentifiers("probe-"+resp.Probe, resp.Answer, keys)...)
	}
	if len(r.MechanicalHits) > 0 {
		r.Void, r.VoidReason = true, "mechanical"
	}
}

func readJSON(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("probe: parsing %s: %w", path, err)
	}
	return nil
}

// LoadInput reads and checks one contamination-probe-input/v1 file.
func LoadInput(path string) (Input, error) {
	var in Input
	if err := readJSON(path, &in); err != nil {
		return Input{}, err
	}
	if in.SchemaVersion != InputSchemaVersion {
		return Input{}, fmt.Errorf("probe: %s is %q, want %s", path, in.SchemaVersion, InputSchemaVersion)
	}
	if in.CaseID == "" || in.ProbeASymptom == "" {
		return Input{}, fmt.Errorf("probe: %s needs both case_id and probe_a_symptom", path)
	}
	return in, nil
}

// LoadVerdicts reads <dir>/<case_id>.json verdict files for the named
// cases. A missing file is not an error: the evaluator has not read that
// case yet, and Decide reports it as unresolved rather than as a pass.
func LoadVerdicts(dir string, caseIDs []string) (map[string]Verdict, error) {
	out := map[string]Verdict{}
	if dir == "" {
		return out, nil
	}
	for _, id := range caseIDs {
		p := filepath.Join(dir, id+".json")
		var v Verdict
		err := readJSON(p, &v)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if v.SchemaVersion != VerdictSchemaVersion || v.CaseID != id {
			return nil, fmt.Errorf("probe: %s is not a %s file for %s", p, VerdictSchemaVersion, id)
		}
		out[id] = v
	}
	return out, nil
}

// Decision is the combined result for one case.
type Decision struct {
	CaseID     string
	Void       bool
	Reason     string
	Unresolved bool
}

// Decide combines the mechanical scan with the evaluator's reading. A case
// is void if either says so. A case whose mechanical scan is clean but
// which no evaluator has read yet is UNRESOLVED, not clean: the mechanical
// rules cannot see mechanism-level recall, so silence from them is not an
// acquittal, and scoring a case on that silence would be the whole failure
// A11 exists to prevent.
func Decide(r Report, v Verdict, haveVerdict bool) Decision {
	d := Decision{CaseID: r.CaseID}
	switch {
	case r.Void:
		d.Void, d.Reason = true, "probe: mechanical ("+hitKinds(r)+")"
	case haveVerdict && v.Hit:
		d.Void, d.Reason = true, "probe: evaluator verdict"
	case !haveVerdict:
		d.Unresolved = true
		d.Reason = "probe: no evaluator verdict; mechanical scan clean but cannot see mechanism-level recall"
	}
	return d
}

func hitKinds(r Report) string {
	seen := map[string]bool{}
	var out string
	for _, h := range r.MechanicalHits {
		if seen[h.Kind] {
			continue
		}
		seen[h.Kind] = true
		if out != "" {
			out += ","
		}
		out += h.Kind
	}
	return out
}
