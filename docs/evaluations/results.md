# Results

A sweep produces two kinds of artifact, and it helps to keep them straight:

- **Stored results** — one committed `results.<ext>` per skill, written as the evals run. This is the source of truth:
  the raw per-model, per-case outcomes, kept deterministic so it commits and diffs cleanly. This page covers them.
- **Reports** — `EVALUATION.md` and a machine-readable rollup, _rendered_ from the stored results by `evolve report`.
  Nothing is measured there; the report is a view over what the results files already hold. See
  [Reviewing reports](../reports/index.md).

| Artifact        | Path                          | Scope      | Written by      |
| --------------- | ----------------------------- | ---------- | --------------- |
| Stored results  | `evals/<skill>/results.<ext>` | one skill  | the eval engine |
| Markdown report | `EVALUATION.md`               | whole repo | `evolve report` |
| Machine rollup  | `EVALUATION.<format>`         | whole repo | `evolve report` |

Both are generated — regenerate them, don't edit them by hand.

## Where they live

Each skill's outcomes land in a single `results.<ext>` beside its authored definitions:

```text
evals/<skill>/
├── triggers.<ext>     # Tier 1 definitions
├── evals.<ext>        # Tier 2 definitions
└── results.<ext>      # committed outcomes — both tiers
```

Supported formats are `json`, `jsonc`, `yaml` and `yml`; the active one follows `--results-format`. A sweep rewrites
only the model entries it actually ran and leaves the rest untouched, so diffs stay scoped to what changed. Writes are
atomic and deterministic — sorted keys, fixed field order, rounded floats, trailing newline — so re-running an unchanged
suite produces a byte-identical file. The mechanics of the write are in
[How evaluations run](execution.md#writing-results).

## What's inside

A results file is nested **model-major**: a top-level header, then a `models` map keyed by `provider/model-id`, and
under each model up to two entries — `triggers` and `evals` — either of which is absent when that tier hasn't run for
the model. The key is provider-qualified (`anthropic/claude-opus-4-8`) because a harness like Cursor can drive another
vendor's model, and the prefix keeps those ids from colliding.

| Field    | Meaning                                                                          |
| -------- | -------------------------------------------------------------------------------- |
| `schema` | Results schema version. `evolve report --migrate` upgrades older files in place. |
| `plugin` | Plugin the skill belongs to.                                                     |
| `skill`  | Skill name (matches the eval directory).                                         |
| `models` | Map of `provider/model-id` → per-model `triggers` / `evals` entries.             |

Each tier entry carries a shared header and then its per-case results:

| Field                          | Meaning                                                                                                                                                                           |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `provider`, `model`, `display` | Provider id, model id, and human-readable name.                                                                                                                                   |
| `harness`                      | Agent CLI that executed the run (`claude`, `codex`, …).                                                                                                                           |
| `tool_version`                 | evolve version that wrote the entry.                                                                                                                                              |
| `ran_at`                       | RFC3339 UTC timestamp of the run.                                                                                                                                                 |
| `content_hash`                 | Fingerprint of the skill content the entry was graded against — triggers hash the `SKILL.md` frontmatter, evals the whole skill directory. Drives `--modified`/`--new` staleness. |
| `timeout_seconds`              | Per-case timeout in force for the run.                                                                                                                                            |
| `pricing`                      | The input/output USD-per-MTok rates snapshotted at run time, or an explicit `null` when the model is unpriced.                                                                    |
| `results`                      | The per-case array — one entry per trigger query or eval case.                                                                                                                    |
| `summary`                      | Aggregates over `results`: `passed`/`failed`/`total`, `pass_rate`, `avg_run_seconds`, and any usage rollup.                                                                       |

Per-case detail differs by tier. A **trigger** result records the `query`, whether it `should_trigger`, the `hits` over
`runs`, the `passed` verdict (hit-rate `≥ 0.5`), and `avg_run_seconds`. An **eval** result records the case `id`/`name`,
a tri-state `passed` (null when skipped or errored), a `runtime_error` string when the agent run failed outright, the
graded `expectations` (each with its `text`, `passed`, and `evidence`), and `execution_metrics`/`timing` for the run.

!!! note "Usage and pricing are grouped, not nulled"

    Token figures live in two optional sub-objects: `estimate` (input tokens from the provider's counting API over
    `SKILL.md` + the query/prompt, priced at the input rate) and `measured` (the harness-reported consumption — fresh
    input, cache reads/writes, output, and total cost). A provider that can't count or report usage simply omits the
    sub-object, so an absent figure stays distinguishable from a measured zero.

Alongside the current run, an entry keeps compact **prior snapshots** so the report can show movement without a re-run:

- `previous` (both tiers) — the run this one replaced. The one-back comparison, i.e. your iteration signal.
- `baseline` (evals only) — the same cases run with the skill _absent_. The gap is the skill's measured **lift**.

Snapshots store only the summary and per-case scalars, never the full expectation/timing detail, so the file stays
readable. The deltas themselves are **derived at report time**, not stored.

!!! tip "The schema is the contract"

    The field-by-field contract — including every optional sub-object — lives in the JSON Schemas under
    [`schemas/`](https://github.com/bitwise-media-group/evolve/tree/main/schemas) (`results.schema.json` and the shared
    `common.schema.json`). Point your editor at them with a `"$schema"` key for validation and completion.

Once the results are written, `evolve report` renders them into the repository's reports — see
[Reviewing reports](../reports/index.md).
