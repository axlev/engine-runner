# Prospective bundle ingest contract

**Status:** normative for `engine-runner`. This is what the engine accepts.

This document exists because `engine-runner` and `miner` diverged: the
miner currently exports `miner/prospective-case/v1`, a flat root with a
patch-and-SHA source model, while the engine consumes a `reviewer/` +
`control/` tree with a materialized source snapshot. The decision was that
**the miner adapts to the engine**, so this is the target.

It is derived from what `internal/boundaryvalidator` and
`internal/contextbuilder` actually enforce, not from an aspiration. Where
`docs/system-design.md` §7 differs, this document is the accurate one.

## Layout

```text
<bundle-root>/                     # the directory passed to bench -bundle
├── reviewer/                      # everything a reasoner may ever see
│   ├── repository/                # sanitized source snapshot (required)
│   ├── diff.patch                 # the admissible diff (required)
│   └── metadata.json              # reviewer-visible metadata (optional)
└── control/                       # engine-only; never mounted to a reasoner
    ├── manifest.json              # routing and identity
    └── checksums.sha256           # integrity over every reviewer/ file
```

`reviewer/` and `control/` are separate for a reason the engine depends on:
the context builder allow-list-copies only the three named entries under
`reviewer/`, so `control/` cannot reach a reasoner even by accident. There
is no filter to bypass, because the control tree is never read on that
path at all.

## `reviewer/repository/`

A plain directory tree of the source as of the cutoff. Required.

**Not** a git repository, and not a patch to be applied. The engine mounts
this read-only into an isolated container; there is no checkout step, no
git dependency, and therefore no post-mount mutation that could
reintroduce contamination after validation ran.

Rejected outright anywhere in the tree:

- symlinks, sockets, device nodes, named pipes
- `.git`, `.gitmodules`, `packed-refs`, `HEAD`, `ORIG_HEAD`, `FETCH_HEAD`,
  `MERGE_HEAD`, `shallow`, `objects`, `refs`, `reflogs`, `worktrees`,
  `alternates`

A symlink is rejected rather than resolved: it can point anywhere,
including at the oracle bundle.

## `reviewer/diff.patch`

The admissible diff, as a unified patch. Required. Named `diff.patch` —
the miner's current `change.patch` must be renamed.

## `reviewer/metadata.json`

Optional. When present, a JSON object carrying **only** these fields:

| Field | Notes |
|---|---|
| `schema_version` | Required, must be exactly `reviewer-metadata/v1` |
| `repository` | `owner/name` |
| `title` | Only if provable as of cutoff |
| `description` | Only if provable as of cutoff |
| `cutoff_timestamp` | RFC3339 |
| `base_branch` | Only if provable unchanged since cutoff |
| `commit_messages` | Array; only if membership was frozen at or before cutoff |

Any other field is a violation, including ones that look innocuous —
`merged`, `state`, `merged_at`, `closed_at`, `fix_commit` all carry
outcome information.

`base_branch` and `commit_messages` are admissible and were added to the
engine's allow-list after reading the miner's own schema: a reviewer at
the cutoff could genuinely see both, and excluding them would make the
benchmark measure a harder task than the real one. The miner's existing
provenance rules for them ("present only when positively provable") are
exactly right and should be kept.

`schema_version` is the one place the engine asks the miner to add
something it currently omits. The miner's instinct — that version
information belongs in the manifest, and that reviewer-visible metadata
should stay purely natural-language context — is defensible. The engine
requires it anyway because it pins the metadata contract independently of
the manifest, so a metadata file that gets separated from its bundle is
still self-identifying. It is one string.

Field *values* are also scanned: a value reading as retrospective outcome
information (`reverted`, `regression introduced`, `root cause was`) is a
violation even in an allowed field.

## `control/manifest.json`

Engine-only routing and identity. Must carry
`"schema_version": "engine-manifest/v1"`. Other fields (case id, base and
cutoff commits, snapshot format) are recorded but not currently
constrained by the validator.

The engine never passes this file to a reasoner.

## `control/checksums.sha256`

One line per file, `sha256sum` format — digest, two spaces, path relative
to the **bundle root**:

```text
8a5b3aa6...dce  reviewer/diff.patch
947a8e7d...083  reviewer/metadata.json
cfc79970...61f  reviewer/repository/internal/paging/paginate.go
```

It must describe **exactly** the set of regular files under `reviewer/` —
no more, no fewer. Both directions are violations:

- a file present in the bundle but not listed (this is how a stray oracle
  file gets caught even if its name looks innocent)
- a file listed but absent

Every digest is recomputed and compared. This replaces the miner's
`manifest.json.artifacts` hashes.

## Names that trip the oracle-shaped check

Any path or metadata value containing (case-insensitive): `oracle`,
`ground_truth`, `groundtruth`, `expected_finding`, `expected-finding`,
`answer_key`, `answerkey`, `solution`, `postmortem`, `post_mortem`,
`regression_report`, `fix_commit`, `final_state`, `review_comments`,
`ci_result`, `verdict`.

These are heuristics, and they are deliberately blunt. `solution` and
`verdict` in particular can false-positive on legitimate source paths in a
real repository. That is the intended trade — a false rejection costs one
investigation, a false acceptance silently corrupts a result that will
look perfectly credible. Report false positives rather than working
around them; the list is meant to be tuned against real exports.

## Migration delta from `miner/prospective-case/v1`

| Current miner output | Required |
|---|---|
| flat root | `reviewer/` + `control/` split |
| `change.patch` | `reviewer/diff.patch` |
| `comparison_base_sha` + patch, no tree | `reviewer/repository/` materialized snapshot |
| `manifest.json` at engine-visible root | `control/manifest.json`, engine-only |
| `manifest.json.artifacts` hashes | `control/checksums.sha256` |
| `metadata.json` with no `schema_version` | add `schema_version: reviewer-metadata/v1` |
| `metadata.json` at root | `reviewer/metadata.json` |

The evaluator-only root stays exactly as it is — separately supplied,
never under the bundle root. The engine never traverses it.

## Verifying against the engine before handing a bundle over

```bash
go run ./cmd/bench -bundle <bundle-root> -case-id <case-id>
```

A contaminated bundle exits non-zero with `status: invalidated`, runs no
stages, spends nothing, and writes `boundary-validation.json` naming every
rule that tripped and the path that tripped it. That report is the
intended feedback loop for miner development — it is designed to be read,
not just to gate.
