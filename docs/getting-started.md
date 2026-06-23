# Getting started

**Evolve** is a Go CLI for evaluating coding-agent plugins and plugin repositories. It validates plugin structure,
checks whether skills trigger for the right prompts, runs behavioral eval suites in throwaway workspaces, and writes
committed Markdown/JSON rollups for review and CI.

## The three tiers

| Tier                  | Command               | What it does                                                                                 |
| --------------------- | --------------------- | -------------------------------------------------------------------------------------------- |
| **Tier 0 — checks**   | `evolve run checks`   | Static validation of manifests, schemas, skill metadata and repository shape. No agents run. |
| **Tier 1 — triggers** | `evolve run triggers` | Prompt-level checks that verify the expected skill activates.                                |
| **Tier 2 — evals**    | `evolve run evals`    | Behavioral cases that run real agent CLIs in throwaway workspaces and grade the result.      |

Run the whole pipeline — `check → triggers → evals → report` — with:

```sh
evolve run all
```

## Quick start

From the root of a plugin repository:

```sh
evolve doctor          # check provider CLIs, credentials and counting APIs
evolve run checks      # Tier 0 — static validation
evolve run triggers    # Tier 1 — trigger accuracy
evolve run evals       # Tier 2 — behavioral evals
evolve report          # rebuild EVALUATION.md + the machine-readable rollup
```

Or run the full pipeline in one shot:

```sh
evolve run all
```

## Make it gate CI

By default, `run` commands warn about failed checks or evals but exit `0` when the run itself completes. `--strict`
turns those failures into exit `1`:

```sh
evolve run all --strict
evolve report --check
```

!!! tip "Interactive by default" On a TTY, `run triggers`, `run evals` and `run all` open a full-screen TUI — a
selection form to scope the run, then a live dashboard. Pass `--no-tui` (or set `EVOLVE_NO_TUI=1`) for plain line output
in CI. Both drive the same engine, so the run is identical either way. See [TUI](tui.md).

## Next steps

- [Installation](installation.md)
- [Configuration](config/index.md)
- [Authoring evaluations](authoring-evaluations.md)
- [Reference](reference.md)
- [TUI](tui.md)
