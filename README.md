# evolve

`evolve` is a Go CLI for evaluating coding-agent plugins and plugin repositories. It validates plugin structure, checks
whether skills trigger for the right prompts, runs behavioral eval suites in throwaway workspaces, and writes committed
Markdown/JSON rollups for review and CI.

The pipeline is split into three tiers:

- Tier 0 `checks`: static validation of manifests, schemas, skill metadata, and repository shape.
- Tier 1 `triggers`: prompt-level checks that verify the expected skill activates.
- Tier 2 `evals`: behavioral cases that run real agent CLIs and grade the result.

<!-- prettier-ignore -->
> [!TIP]
> **New to evolve?** Read the full docs at **[oss.bitwisemedia.uk/evolve](https://oss.bitwisemedia.uk/evolve/)** —
> getting started, authoring evaluations (triggers, behavioral evals, fixtures, and how they run), the configuration
> reference, and the TUI guide.

## Supported repositories

`evolve` auto-detects these layouts, or you can force one with `--layout`:

| Layout        | Marker                                    | Skill paths                        | Eval paths                        |
| ------------- | ----------------------------------------- | ---------------------------------- | --------------------------------- |
| `single`      | `.claude-plugin/plugin.json`              | `skills/<skill>/`                  | `evals/<skill>/`                  |
| `multi`       | `plugins/*/.claude-plugin/plugin.json`    | `plugins/<plugin>/skills/<skill>/` | `plugins/<plugin>/evals/<skill>/` |
| `marketplace` | `.claude-plugin/marketplace.json` at root | `plugins/<plugin>/skills/<skill>/` | `plugins/<plugin>/evals/<skill>/` |

Each eval directory may contain:

- `triggers.<ext>` for trigger-accuracy prompts.
- `evals.<ext>` for behavioral eval cases.
- `results.<ext>` for stored model results.

Supported data formats are `json`, `jsonc`, `yaml`, and `yml`; for a given basename, only one matching file may exist.

## Harnesses and providers

`evolve` distinguishes the **harness** — the agent CLI it drives — from the **provider** that owns and prices the model
the harness runs. Models are provider-qualified, and each is bound to the harness that can run it; evals execute once
per model, through that harness.

Built-in harnesses, each needing its runner CLI on `PATH` and whatever credentials that CLI requires:

| Harness        | Runner CLI     |
| -------------- | -------------- |
| Claude Code    | `claude`       |
| OpenAI Codex   | `codex`        |
| Gemini         | `gemini`       |
| Cursor         | `cursor-agent` |
| GitHub Copilot | `copilot`      |
| Antigravity    | `agy`          |
| Grok           | `grok`         |

Built-in providers (the model vendors) are **Anthropic**, **OpenAI**, **Google**, **Cursor**, and **xAI** — Cursor is
both a provider (it owns Composer) and a harness; xAI models are driven by the Grok harness. Run `evolve doctor` from a
plugin repository to check the environment, credentials, and runner CLIs, and `evolve models` to see the effective
provider / model / harness matrix.

## Install

Install with Homebrew on macOS and Linux:

```sh
brew install --cask bitwise-media-group/tap/evolve
```

Build from source with Go:

```sh
go install github.com/bitwise-media-group/evolve/cmd/evolve@latest
```

Or build this checkout:

```sh
make build
./evolve version
```

## Quick start

From the root of a plugin repository:

```sh
evolve doctor
evolve run checks
evolve run triggers
evolve run evals
evolve report
```

To run the full pipeline:

```sh
evolve run all
```

To make evaluation failures fail CI:

```sh
evolve run all --strict
evolve report --check
```

By default, `run` commands warn about failed checks or evals but exit `0` when the run itself completes. `--strict`
changes those failures to exit `1`; usage, configuration, and runtime errors exit `2`.

## Running evals

`evolve run checks` performs static validation only. It does not start agent CLIs.

```sh
evolve run checks
```

`evolve run triggers` runs each authored trigger prompt several times and records whether the expected skill activated.

```sh
evolve run triggers --model anthropic,openai --runs 5
```

`evolve run evals` runs behavioral cases in temporary workspaces, then grades the outputs with deterministic assertions
and any configured LLM judge.

```sh
evolve run evals --model anthropic,openai --jobs 4 --max-turns 12 --timeout 900
```

Useful run filters and debug flags:

- `--plugin a,b` (alias `--plugins`): restrict the run to one or more plugins. Repeatable, or comma-separated.
- `--skill x,y` (alias `--skills`): restrict the run to one or more skills. Repeatable, or comma-separated.
- `--model anthropic,openai` (alias `--models`): pick providers / model ids, or `all`. Repeatable, or comma-separated.
- `--eval case-id`: restrict `run evals` to one behavioral case.
- `--new`: run only work with missing or stale stored results.
- `--modified`: rerun only cases whose authored content changed since their stored results (trigger frontmatter or
  definition; eval skill files or definition), fingerprinted alongside the results.
- `--keep-workspaces`: leave temporary workspaces behind for debugging.
- `--count-only`: compute token usage without running agents.
- `--stale-results keep|drop`: decide what to do with stored results outside the `models` restriction.

## Interactive TUI

On an interactive terminal, `evolve run triggers`, `run evals`, and `run all` open a full-screen TUI: first a selection
form to scope the run, then a live dashboard that streams results as agents finish. Pass `--no-tui` (or set
`EVOLVE_NO_TUI=1`) for the plain line-based output used in CI and non-TTY pipes — both paths drive the same engine, so
the run is identical either way.

### Selection form

The form is a set of focusable panes you tab between to choose what runs:

- **Filters** — the same `new` / `modified` / `failed` scoping that the run flags expose.
- **Harnesses** — the agent CLIs to drive; any whose CLI is off `PATH` is shown disabled.
- **Models** — individual models grouped under a per-provider header row, so you can toggle one model or a whole
  provider at once. Models unsupported by the enabled harnesses are shown disabled.
- **Plugins / Skills / Cases** — a tree of every trigger and behavioral case. Each row shows whether it is forced on,
  forced off, or auto-queued for all / some / none of the enabled models; a legend under the tree names every glyph.

Move between panes with `tab` / `shift+tab` (or `1`–`4` to jump), `↑↓` / `jk` to move within a pane, `←→` / `hl` to fold
the tree, `space` to toggle, and `g` / `G` for the ends. Tab on to the **RUN** / **CANCEL** buttons, or just press `r`
to run and `esc` to cancel. The form previews exactly what will execute — it and the engine resolve through the same
plan, so they cannot drift.

### Live dashboard

![evolve live run dashboard](docs/assets/dashboard-1200.png)

Once a run starts, the dashboard streams progress:

- A title bar with running pass / fail / error tallies, elapsed time, rolled-up cost, and an overall progress bar.
- An **Execution** tree (plugin → skill → model → case) carrying per-node rollup columns.
- A tabbed **Rollup** (Summary / Providers / Plugins / Skills), a **Runs** log of every execution in plan order, and a
  **Details** pane showing in-flight cases and the selected case's authored spec.

Selecting a run in any pane moves the selection everywhere; `f` follows the live execution, `enter` jumps to its detail,
and `g` / `G` plus `^d` / `^u` scroll. See [DESIGN.md → TUI](DESIGN.md) for the full wiring.

## Reports

`evolve report` rebuilds repository-level rollups from stored per-skill results:

```sh
evolve report
evolve report --check
```

The report command writes `EVALUATION.md` plus a machine-readable rollup using the configured results format. In
marketplace and multi-plugin repositories, it also includes per-plugin detail pages.

Thresholds default to `0.5` for triggers and `0.66` for evals; they can be set in `.evolve.<ext>` or passed directly:

```sh
evolve report --check --min-triggers-pass-rate 0.95 --min-evals-pass-rate 0.90
```

## Commands

Top-level commands:

- `evolve doctor`: check harness runner CLIs, credentials, and counting APIs.
- `evolve models`: show the effective provider / model / harness matrix and pricing metadata.
- `evolve report`: regenerate evaluation rollups from stored results.
- `evolve run`: run static checks, trigger checks, behavioral evals, or the full pipeline.
- `evolve version`: print build metadata.

Run-tier commands:

- `evolve run checks`
- `evolve run triggers`
- `evolve run evals`
- `evolve run all`

Common global flags:

- `--root PATH`: repository root to operate on.
- `--layout auto|single|multi|marketplace`: repository layout.
- `--results-format json|jsonc|yaml`: results and rollup format.
- `--json`: emit machine-readable JSONL progress.
- `-v, --verbose`: enable debug logging.

See [docs/cli/evolve.md](docs/cli/evolve.md) for the generated command reference.

## Configuration

`evolve` reads at most one config file from the repository root:

- `.evolve.yaml`
- `.evolve.yml`
- `.evolve.json`
- `.evolve.jsonc`

Settings are layered in this order:

1. Built-in defaults.
2. The config file.
3. `EVOLVE_*` environment variables.
4. Explicit CLI flags.

Common settings:

- `layout`
- `models`
- `harnesses`
- `cache_dir`
- `results_format`
- `max_turns`
- `stale_results`
- `checks.*`
- `report.thresholds.*`
- `providers.<name>.models`

Read [docs/config/index.md](docs/config/index.md) for the full generated configuration reference and annotated example
configs.

## Development

Common targets:

```sh
make fmt
make test
make lint
make docs
make smoke
make pr
```

Notes:

- `make docs` regenerates committed CLI, manpage, and config docs under `docs/`.
- `make smoke` runs the live end-to-end test in `e2e/` and requires the relevant provider CLI and credentials.
- `tools/` is a separate Go module for pinned developer CLIs.
- `e2e/` is a separate Go module for live smoke coverage and fixture repositories.

## Project layout

```text
cmd/evolve/   cobra CLI entrypoint and subcommands
internal/     core packages by concern
docs/         generated CLI, manpage, and config reference
schemas/      JSON Schemas for eval and report data
e2e/          separate module for end-to-end smoke coverage
tools/        separate module for pinned developer tooling
security/     code-scanning and security notes
```

## Further reading

- **[Documentation site](https://oss.bitwisemedia.uk/evolve/)** — getting started, authoring evaluations, configuration,
  and the TUI guide.
- [DESIGN.md](DESIGN.md) for architecture, engine boundaries, and TUI wiring.
- [docs/cli/evolve.md](docs/cli/evolve.md) for generated command documentation.
- [docs/config/index.md](docs/config/index.md) for the full config surface.
