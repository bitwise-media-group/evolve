# evolve

`evolve` is a Go CLI for evaluating coding-agent plugins and plugin repositories. It validates plugin structure, checks
whether skills trigger for the right prompts, runs behavioral eval suites in throwaway workspaces, and writes committed
Markdown/JSON rollups for review and CI.

The pipeline is split into three tiers:

- Tier 0 `checks`: static validation of manifests, schemas, skill metadata, and repository shape.
- Tier 1 `triggers`: prompt-level checks that verify the expected skill activates.
- Tier 2 `evals`: behavioral cases that run real agent CLIs and grade the result.

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

## Providers

`evolve` can run the built-in provider set:

- Anthropic
- OpenAI
- Google
- Cursor
- GitHub Copilot
- Antigravity

Each provider needs its runner CLI on `PATH` and whatever credentials that CLI requires. Run `evolve doctor` from a
plugin repository to check the local environment, credentials, provider CLIs, and token-counting access.

## Install

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

On an interactive terminal, `run triggers`, `run evals`, and `run all` open a TUI for toggling filters, harnesses,
models, and cases before showing live progress. Use `--no-tui` or `EVOLVE_NO_TUI=1` for plain output.

## Reports

`evolve report` rebuilds repository-level rollups from stored per-skill results:

```sh
evolve report
evolve report --check
```

The report command writes `EVALUATION.md` plus a machine-readable rollup using the configured results format. In
marketplace and multi-plugin repositories, it also includes per-plugin detail pages.

Thresholds can be set in `.evolve.<ext>` or passed directly:

```sh
evolve report --check --min-triggers-pass-rate 0.95 --min-evals-pass-rate 0.90
```

## Commands

Top-level commands:

- `evolve doctor`: check provider CLIs, credentials, and counting APIs.
- `evolve models`: show the effective provider/model matrix and pricing metadata.
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

Read [docs/config/configuration.md](docs/config/configuration.md) for the full generated configuration reference and
annotated example configs.

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

- [DESIGN.md](DESIGN.md) for architecture, engine boundaries, and TUI wiring.
- [docs/cli/evolve.md](docs/cli/evolve.md) for generated command documentation.
- [docs/config/configuration.md](docs/config/configuration.md) for the full config surface.
