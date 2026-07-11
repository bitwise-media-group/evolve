# Reviewing reports

Reports are the repository-level view over a sweep. `evolve report` reads every committed
[`results.<ext>`](../evaluations/results.md), rolls the entries up across plugins, skills, and models, and renders two
artifacts: a human-readable `EVALUATION.md` and a machine-readable rollup. Nothing is measured here — the report is a
projection of what the stored results already hold, so it re-renders identically until the results change.

## Generating reports

`evolve report` is the last step of `evolve run all`, but it touches no agents, so you can re-run it any time the stored
results change — after a partial sweep, a `--migrate`, or a `models` restriction.

```sh
evolve report                 # regenerate EVALUATION.md + the machine rollup
evolve report --migrate       # upgrade stored results to the latest schema first
evolve report --check         # also gate on configured thresholds (non-zero exit on breach)
```

| Flag                         | Description                                                                                                                                                                                                                     |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--check`                    | Fail (exit `1`) when a pass rate breaches its threshold (defaults: triggers `0.5`, evals `0.66`).                                                                                                                               |
| `--migrate`                  | Upgrade stored results files to the latest schema before rendering.                                                                                                                                                             |
| `--min-triggers-pass-rate`   | Minimum trigger pass rate `0..1` for `--check` (overrides the config threshold).                                                                                                                                                |
| `--min-evals-pass-rate`      | Minimum eval pass rate `0..1` for `--check` (overrides the config threshold).                                                                                                                                                   |
| `--maturity`                 | Comma-separated maturity levels (`stable`, `unstable`, `prerelease`) whose Tier 1/Tier 2 evidence issues fail `--check`; other levels only warn. Default `stable,unstable,prerelease` (overrides `report.thresholds.maturity`). |
| `--stale-results keep\|drop` | What to do with stored results for models outside the active `models` set (default: prompt on a terminal, else keep).                                                                                                           |

The output format follows `--results-format`; switching formats removes the stale rollup from the previous choice. What
gets written depends on the [repository layout](../config/index.md):

| Layout              | `EVALUATION.md` (repo root)            | `EVALUATION.<format>` (repo root) | Per-plugin `EVALUATION.md`             |
| ------------------- | -------------------------------------- | --------------------------------- | -------------------------------------- |
| single              | rollup **and** per-skill detail tables | yes                               | —                                      |
| multi / marketplace | rollup only                            | yes                               | one per plugin, with the detail tables |

## The Markdown report

`EVALUATION.md` opens with a generated-by marker, an `# Skill evaluations` heading, and a methodology paragraph
explaining where the figures come from and how to read empty cells. If a `models` restriction is active, an **##
Excluded models** table follows, listing the catalog models left out (so a partial matrix never reads as the whole
picture).

Then one **## &lt;plugin&gt;** section per plugin, each with up to two rollup tables.

!!! note "Two kinds of empty cell"

    The report distinguishes them deliberately, and the difference is decidable from the stored data:

    - **`—`** — not measured _yet_. A rerun could fill it (e.g. a tier that hasn't run for that model).
    - **`n/a`** — the provider can _never_ produce it: no counting API, no usage reporting, or no published pricing. It's
      structurally absent, not zero.

### Rollup tables

The per-plugin **Triggers** rollup, one row per model:

| Column            | What it shows                                                                             |
| ----------------- | ----------------------------------------------------------------------------------------- |
| `Provider`        | Vendor display name.                                                                      |
| `Model`           | Display name with the provider-local model id in backticks.                               |
| `Passed`          | Passing queries over total (`1/2`); `(N errored)` is appended when runs failed outright.  |
| `Pass rate`       | Share of queries that passed, as a percentage.                                            |
| `Δ rate`          | Change in pass rate versus the previous run; `—` when there's nothing to compare against. |
| `Avg run`         | Mean wall-clock per run, weighted across queries.                                         |
| `Input tokens`    | Estimated input tokens (`SKILL.md` + query) from the counting API.                        |
| `Est. input cost` | That estimate priced at the model's input rate.                                           |

The per-plugin **Evals** rollup adds the measured-usage columns:

| Column            | What it shows                                                                                                |
| ----------------- | ------------------------------------------------------------------------------------------------------------ |
| `Provider`        | Vendor display name.                                                                                         |
| `Model`           | Display name with the provider-local model id in backticks.                                                  |
| `Passed`          | Passing evals over total; `(N errored)` for runs that failed outright (excluded from the ratio).             |
| `Δ rate`          | Pass-rate change versus the previous run; `(vs base)` when only a baseline exists to compare to.             |
| `Lift vs base`    | Pass-rate gain over the no-skill **baseline** run — the skill's measured contribution; `—` with no baseline. |
| `Avg run`         | Mean executor duration (the agent run only, excluding grading).                                              |
| `Input tokens`    | Estimated input tokens from the counting API.                                                                |
| `Est. input cost` | The estimate priced at the input rate.                                                                       |
| `Measured in/out` | Harness-reported input/output tokens, as `in/out`.                                                           |
| `Cache rd/wr`     | Cache read / creation tokens, as `read/write`.                                                               |
| `Measured cost`   | Harness-reported total cost for the run.                                                                     |

A rendered slice of a single-layout report — the `solo` plugin section, with its `Triggers` and `Evals` rollups:

#### Triggers

| Provider  | Model                                 | Passed | Pass rate | Δ rate | Avg run | Input tokens | Est. input cost |
| --------- | ------------------------------------- | ------ | --------- | ------ | ------- | ------------ | --------------- |
| Anthropic | Claude Fable 5 (`claude-fable-5`)     | 1/2    | 50%       | -50%   | 7.1s    | 2,770        | $0.0277         |
| Cursor    | Cursor Composer 2.5 (`composer-2.5`)  | 2/2    | 100%      | —      | 12.7s   | n/a          | n/a             |
| Google    | Gemini 3.5 Flash (`gemini-3.5-flash`) | —      | —         | —      | —       | 2,580        | $0.0039         |

#### Evals

| Provider  | Model                             | Passed | Δ rate | Lift vs base | Avg run | Input tokens | Est. input cost | Measured in/out | Cache rd/wr   | Measured cost |
| --------- | --------------------------------- | ------ | ------ | ------------ | ------- | ------------ | --------------- | --------------- | ------------- | ------------- |
| Anthropic | Claude Fable 5 (`claude-fable-5`) | 0/1    | -100%  | +0%          | 84.2s   | 1,827        | $0.0183         | 8,200/3,142     | 220,000/5,480 | $0.7824       |

### Detail tables

The detail tables turn the view **case-major** — one heading per trigger query or per eval, with the models as rows, so
comparing models on a single case is the default. In a single-layout repo they follow the rollups in the root
`EVALUATION.md`; in multi/marketplace repos they move to each plugin's own `EVALUATION.md`.

Trigger detail, under a `#### <query> (expected: yes|no)` heading:

| Column              | What it shows                                            |
| ------------------- | -------------------------------------------------------- |
| `Provider`, `Model` | Vendor and model, as in the rollup.                      |
| `Result`            | Per-query verdict: `PASS`, `FAIL`, or `—` (ungraded).    |
| `Rate`              | Hits over runs (`3/3`).                                  |
| `Δ rate`            | Change versus the previous run; `—` when not comparable. |
| `Avg run`           | Mean run duration.                                       |
| `Input tokens`      | Estimated input tokens.                                  |
| `Est. cost`         | The estimate priced at the input rate.                   |

Eval detail, under a `#### <id> — <name>` heading, mirrors the eval rollup's columns but swaps the passed-count for a
per-case `Result`: `PASS`, `FAIL`, `ERROR` (a runtime error — the agent run failed), or `—`. Below the table, each
failed run is itemised — runtime errors as `- <model> runtime error: <message>`, and each failed expectation as
`- <model> failed <assertion>: <evidence>` — so a failure points straight at the judge's reasoning.

## The machine-readable rollup

`EVALUATION.<format>` is the same data as a structured object, for CI gates, dashboards, and diffing. Its top level:

| Field          | Meaning                                             |
| -------------- | --------------------------------------------------- |
| `schema`       | Rollup schema version.                              |
| `tool_version` | evolve version that generated the rollup.           |
| `latest_run`   | The most recent `ran_at` across every entry.        |
| `plugins`      | Map of plugin name → `{ triggers, evals, skills }`. |

Under each plugin, `triggers` and `evals` are maps of `provider/model-id` → a model rollup, and `skills` nests the same
shape per skill for drill-down. Each model rollup carries the aggregates the Markdown tables render — `passed`,
`failed`, `errored`, `total`, `pass_rate`, `avg_run_seconds`, the `estimate` and `measured` usage objects — plus the
machine-only `baseline` summary and the `previous_delta` / `baseline_delta` objects (`rate`, `avg_run_seconds`,
`input_tokens`, `output_tokens`, `cost_usd`) that the Markdown renders as `Δ rate` and `Lift vs base`.

## Gating on thresholds

`evolve report --check` turns the rollup into a CI gate: it compares the trigger and eval pass rates against the
thresholds in your [`.evolve` config](../config/index.md) (or the `--min-*-pass-rate` overrides) and exits non-zero on a
breach, printing each as `FAIL: …`.

```sh
evolve report --check --min-triggers-pass-rate 0.95 --min-evals-pass-rate 0.90
```

The thresholds have built-in defaults — `0.5` for triggers and `0.66` for evals — so with nothing configured `--check`
gates at those; config and the `--min-*-pass-rate` flags override them. The run dashboard classifies its rollup
indicators against the same thresholds, so a group that would breach the gate reads as a red ✗ there. Exit codes follow
the [reference](../reference.md#exit-codes): `0` clean, `1` on a breach, `2` on a usage or runtime error.

## Maturity-aware evidence gating

`--check` classifies each plugin by its manifest version and decides **per plugin** whether a Tier 1 / Tier 2 _evidence
issue_ fails the gate or only warns. The gated set is `report.thresholds.maturity` (default `stable,unstable,prerelease`
— every user-selectable level), or the `--maturity` flag: a plugin whose maturity is in the set **fails** on an evidence
issue, and every other plugin **warns** without failing. Evidence still renders in the report either way — maturity
changes the severity of an issue, not the visibility of results.

A plugin's maturity comes from its manifest version:

| Maturity     | Manifest version                                                 |
| ------------ | ---------------------------------------------------------------- |
| `stable`     | `>= 1.0.0` with no prerelease tag                                |
| `unstable`   | `< 1.0.0` (a `0.x.y` release)                                    |
| `prerelease` | any version carrying a SemVer prerelease tag (e.g. `2.0.0-rc.1`) |

A missing or unparseable version never fails the gate — it can only warn — so a manifest/version problem surfaces
through `evolve run checks`, never as a silently stricter report gate.

By default every classified maturity level is gated (`stable,unstable,prerelease`), so `--check` fails on an evidence
issue regardless of a plugin's release stability; only `unknown`-maturity plugins (missing or unparseable version) warn.
Narrow `--maturity` to relax the gate for less mature plugins:

```sh
evolve report --check                              # stable, unstable, and prerelease plugins all fail on evidence issues
evolve report --check --maturity stable             # only stable (>= 1.0.0) plugins fail; unstable/prerelease warn
```

An _evidence issue_, for a tier enabled by a non-zero threshold, is any of:

- **below threshold** — the tier's pass rate is under its `report.thresholds.*` minimum. Checked on any
  `report --check`.
- **missing / incomplete** — a model in the pinned matrix (`--strict`, or an explicit `report.thresholds.models`) has no
  current result.
- **stale** — stored results no longer match the authored `SKILL.md` / eval content. Checked only under `--strict`.

So plain `report --check` is a pass-rate gate; `--strict` adds the missing/incomplete and staleness requirements.
Maturity only decides fail-vs-warn for whichever issues arise — it is not itself tied to `--strict`.

Because gating is per plugin, a plugin evaluated as a standalone repository and the same plugin evaluated inside a
marketplace reach the same verdict given equivalent evidence and config.

### Marketplace CI modes

The gate reads whatever current evidence is on disk when it runs; it does not care whether that evidence was generated
in the same workflow, produced by an earlier step, or published with the plugin. Two modes are supported, and in both
Tier 0 (`evolve run checks --strict`) stays mandatory and is never maturity-exempt:

1. **Generate, then gate** — run the Tier 1 / Tier 2 sweeps in CI before the gate:

    ```sh
    evolve run checks --strict            # Tier 0, always required
    evolve run triggers && evolve run evals
    evolve report --check --strict
    ```

2. **Consume published evidence** — skip the expensive agent runs and gate on the `results.*` files published with the
   plugin (for example by a dedicated plugin repository that already ran the sweeps):

    ```sh
    evolve run checks --strict            # Tier 0, always required
    evolve report --check --strict        # gates the committed evidence as-is
    ```

If a PR changes a skill or eval suite and no matching current evidence exists when the gate runs, that evidence reads as
missing or stale: by default every classified plugin (stable, unstable, or prerelease) fails. A plugin whose version is
missing or unparseable classifies as `unknown` and only warns — it never fails the gate, since `unknown` is not a
selectable `--maturity` level and so can never enter the gated set. Give a plugin a valid version to bring it under the
gate.
