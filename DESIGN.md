# Design

evolve is a single Go CLI that evaluates coding-agent plugins: static checks (Tier 0), trigger-accuracy evals (Tier 1),
behavioral evals (Tier 2), and committed Markdown/JSON reports. The README covers usage; this document records the
conventions the codebase follows.

## Ergonomics

Commands are flat verbs, except the eval tiers, which nest under `run`:

```text
evolve <verb>
evolve run checks|triggers|evals|all
```

`evolve run all` chains the tiers — check, triggers, evals, then report — and keeps going past tier failures so later
tiers still produce signal.

Global flags (`--root`, `--layout`, `--json`, `-v`) apply to every command. Configuration layers, lowest precedence
first: built-in defaults, the optional `.evolve.<ext>` config file at the repository root (YAML, JSON, JSONC, or TOML —
at most one), `EVOLVE_*` environment variables, then explicit flags.

Exit codes are part of the interface: 0 means the run completed, 2 means a usage or configuration error stopped it.
Failed checks or evals print a `WARN:` line but still exit 0 by default; passing `--strict` to a `run` subcommand
restores exit 1 on failures (`cli.ErrFailures`). `report --check` always exits 1 on threshold breaches.

## File layout

```text
./go.mod
./cmd/evolve/main.go   # main entry point and root command (package main)
./cmd/evolve/<verb>.go # one file per subcommand
./cmd/evolve/docs.go   # hidden command that regenerates docs/cli, docs/man, and docs/config

./internal/cli/...     # shared command plumbing: global Options, config layering,
                       # provider/repo/threshold resolution
./internal/run/...     # the three eval engines: checks, triggers, evals
./internal/<area>/...  # one package per remaining concern (grade, report, results,
                       # runner, workspace, ...)

./docs/cli/...         # generated command reference (make docs)
./docs/man/...         # generated man pages (make docs)
./docs/config/...      # generated configuration reference + annotated examples (make docs)
./e2e/...              # separate module: live smoke test plus fixture repositories and golden files
./tools/go.mod         # pinned developer CLIs (addlicense, golangci-lint, goreleaser, syft)
```

If a concern spans areas, it gets its own package under `./internal` with a clear but concise name. Every internal
package carries its package documentation in a `doc.go`.

## Architecture

Each subcommand lives in its own `cmd/evolve/<verb>.go` as a package-level `<verb>Cmd` var plus a `<Verb>Flags` struct,
with the file's `init()` registering its flags and adding the command to its parent (`rootCmd`, or `runCmd` for the eval
tiers). Shared global state lives in the package-level `opts` (`cli.Options`).

`internal/cli` owns the resolved global state (`Options`) and the helpers that turn it into a detected repository, an
effective provider set, a token counter, and report thresholds. The engines (`run`, `report`) take everything they need
as explicit options — the trigger and case engines embed the shared `run.Options` — and write through the interfaces
they declare, so they test against fakes; `runner` is the only package that touches `os/exec`.

The CLI reference in `docs/cli` and the man pages in `docs/man` are generated from the cobra command tree, and the
configuration reference plus annotated example config files in `docs/config` from `internal/configdoc`'s schema (all via
`make docs`) and committed, so reviewing a flag or config change shows the documentation diff alongside the code.
