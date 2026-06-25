# Reference

## Commands

Top-level:

| Command          | Description                                                                |
| ---------------- | -------------------------------------------------------------------------- |
| `evolve doctor`  | Check provider CLIs, credentials and counting APIs.                        |
| `evolve models`  | Show the effective provider/model matrix and pricing metadata.             |
| `evolve report`  | Regenerate evaluation rollups from stored results.                         |
| `evolve run`     | Run static checks, trigger checks, behavioral evals, or the full pipeline. |
| `evolve version` | Print build metadata.                                                      |

Run tiers:

```text
evolve run checks     Tier 0 — static validation (no agents run)
evolve run triggers   Tier 1 — does the expected skill activate?
evolve run evals      Tier 2 — behavioral cases in throwaway workspaces
evolve run all        check → triggers → evals → report
```

The generated command reference lives in `docs/cli/evolve.md`.

## Global flags

| Flag                                        | Description                           |
| ------------------------------------------- | ------------------------------------- |
| `--root PATH`                               | Repository root to operate on.        |
| `--layout auto\|single\|multi\|marketplace` | Repository layout.                    |
| `--results-format json\|jsonc\|yaml`        | Results and rollup format.            |
| `--json`                                    | Emit machine-readable JSONL progress. |
| `-v, --verbose`                             | Debug logging.                        |

## Run flags

| Flag                                          | Description                                                          |
| --------------------------------------------- | -------------------------------------------------------------------- |
| `--plugin a,b` (alias `--plugins`)            | Restrict the run to one or more plugins.                             |
| `--skill x,y` (alias `--skills`)              | Restrict the run to one or more skills.                              |
| `--model anthropic,openai` (alias `--models`) | Pick providers / model ids, or `all`.                                |
| `--eval case-id`                              | Restrict `run evals` to one behavioral case.                         |
| `--runs N`                                    | Repeat each trigger prompt N times.                                  |
| `--jobs N`                                    | Concurrency for behavioral evals.                                    |
| `--max-turns N`                               | Per-case turn cap.                                                   |
| `--timeout SECONDS`                           | Per-case timeout.                                                    |
| `--new`                                       | Run only work with missing or stale stored results.                  |
| `--modified`                                  | Rerun only cases whose authored content changed since their results. |
| `--keep-workspaces`                           | Leave temporary workspaces behind for debugging.                     |
| `--count-only`                                | Compute token usage without running agents.                          |
| `--stale-results keep\|drop`                  | What to do with results outside the `models` set.                    |
| `--strict`                                    | Turn check / eval failures into a non-zero exit.                     |
| `--no-tui`                                    | Force plain line output (also `EVOLVE_NO_TUI=1`).                    |

## Exit codes

| Code | Meaning                                                                   |
| ---- | ------------------------------------------------------------------------- |
| `0`  | The run completed. By default, failed checks / evals only warn.           |
| `1`  | With `--strict` (or `report --check`): check / eval / threshold failures. |
| `2`  | Usage, configuration, or runtime errors.                                  |

## Providers

| Provider       | Harness CLI            | Credential                                              | Triggers | Evals | Token counting |
| -------------- | ---------------------- | ------------------------------------------------------- | -------- | ----- | -------------- |
| Anthropic      | `claude`               | `ANTHROPIC_API_KEY` (or OAuth token vars)               | yes      | yes   | yes            |
| OpenAI         | `codex`                | `OPENAI_API_KEY`                                        | yes      | yes   | yes            |
| Google         | `gemini`               | `GEMINI_API_KEY` / `GOOGLE_API_KEY`                     | yes      | no    | yes            |
| Cursor         | `agent` (cursor-agent) | `CURSOR_API_KEY`                                        | yes      | yes   | no             |
| GitHub Copilot | `copilot`              | `COPILOT_GITHUB_TOKEN` (or `GH_TOKEN` / `GITHUB_TOKEN`) | yes      | yes   | no             |
| Antigravity    | `agy`                  | OAuth login via `agy`                                   | yes      | yes   | no             |

Each provider needs its harness CLI on `PATH` and the credentials that CLI requires. Cursor, Copilot and Antigravity
expose no token-counting API, so their figures render as `n/a` — structurally absent, not zero. Run `evolve doctor` to
check the local environment.

## Reports

`evolve report` rebuilds repository-level rollups from stored per-skill results, writing `EVALUATION.md` plus a
machine-readable rollup in the configured format (per-plugin detail pages in marketplace / multi repos). Gate on
thresholds:

```sh
evolve report --check --min-triggers-pass-rate 0.95 --min-evals-pass-rate 0.90
```

[Reviewing reports](reports/index.md) walks through the report layout, every table and its columns, and the threshold
gate.
