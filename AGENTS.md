# Agent orientation

Fast map of this repository so a new session can act without re-exploring. For _why_ the code is shaped this way —
conventions, the engine/reporter architecture, and how the TUI is wired — read [DESIGN.md](DESIGN.md); for end-user
usage read [README.md](README.md). This file is the "where things are"; DESIGN.md is the "how it fits together".

## What this is

`evolve` is a single Go CLI (module `github.com/bitwise-media-group/evolve`, entry `cmd/evolve`) that evaluates
coding-agent plugins across tiers: static checks (Tier 0), trigger-accuracy evals (Tier 1), and behavioral evals (Tier
2), then writes committed Markdown/JSON reports. It drives real agent CLIs (Anthropic, OpenAI, Google, Cursor, Copilot,
Antigravity) in throwaway workspaces and grades the results.

## Layout

```text
cmd/evolve/        CLI: package main, one file per verb (cobra). See "Commands" below.
internal/          All library code, one package per concern. See "Packages" below.
docs/              GENERATED, committed (make docs): cli/ (command ref), man/ (man pages),
                   config/ (configuration.md + schema + annotated example configs).
schemas/           JSON Schemas for eval/results/report files (embedded via schemas.go).
e2e/               SEPARATE Go module: live smoke test + fixture repos/ and golden/ outputs.
tools/             SEPARATE Go module: pinned dev CLIs (golangci-lint, goreleaser, syft, addlicense).
security/          Committed code-scanning notes.
.github/workflows/ CI, release (goreleaser), CodeQL, auto-merge.
```

Generated/build outputs not to edit by hand: `docs/` (run `make docs`), `dist/` (goreleaser), `./evolve` (built binary),
`node_modules/` (markdownlint/prettier tooling only).

## Commands (`cmd/evolve/`)

`main.go` is the root command and shared `opts`; each verb is `<verb>.go` registering itself in `init()`. Verbs:

- `run` (parent) → `run checks`, `run triggers`, `run evals`, `run all` — the eval tiers; `run all` chains them.
- `report` — regenerate EVALUATION.md / EVALUATION.json from committed results.
- `models`, `doctor`, `version` — list the model matrix, environment diagnostics, version.
- `docs` (hidden) — regenerate `docs/`.
- `runui.go` — interactive-TUI gating and the form→engine→dashboard wiring shared by the interactive `run` paths (the
  `run_*.go` files fall back to plain output when the TUI is off). **See DESIGN.md → TUI.**

## Packages (`internal/`)

- `cli` — shared command plumbing: global `Options`, layered `.evolve` config, provider/repo/threshold resolution.
- `run` — the three eval engines (`checks.go`, `triggers.go`, `evals.go`, `sweep.go`), the execution `plan.go`, and the
  `Reporter` seam (`reporter.go`) the TUI and plain output both implement.
- `tui` — the interactive bubbletea selection form and live run dashboard; a presentation layer over `run`. **See
  DESIGN.md → TUI for the full wiring.**
- `provider` — the agent providers: model matrices + pricing, runner-CLI command construction, output parsing.
- `runner` — executes provider command specs; the only package touching `os/exec` (so engines test against a fake).
- `grade` — assertion evaluation: deterministic checks (files/regex/commands) plus an LLM judge.
- `workspace` — builds the throwaway project dirs each agent session runs in.
- `results` — the committed per-skill `results.<ext>` files beside each skill's evals.
- `report` — renders results into EVALUATION.md / EVALUATION.json.
- `evalspec` — parses authored triggers/evals definitions.
- `manifest` — parses plugin/marketplace manifests and SKILL.md frontmatter.
- `layout` — detects the repo shape (single/multi/marketplace) and enumerates plugins + eval sets.
- `tokencount` — caches provider-reported input-token counts (from official counting APIs, never a local tokenizer).
- `encfmt` — reads/writes JSON, JSONC, YAML behind one data model.
- `configdoc` — renders the configuration reference and annotated example configs.
- `telemetry` — OpenTelemetry setup: picks the exporter (JSON files when `--telemetry-dir`/`telemetry.dir` is set, else
  `autoexport` from `OTEL_*`, else disabled), builds the slog→OTEL fanout handler, decorates the `run.Reporter` to turn
  engine events into metrics/logs, and owns provider shutdown. The engines reach the global tracer/meter directly, so
  only this package imports `internal/run` (no cycle). Off by default.
- `version` — build/version info.

## Build, test, run

`Makefile` targets (Go 1.x via `go.mod`): `build`, `run`, `test`, `test-coverage`, `fuzz`, `smoke` (e2e module), `lint`
(golangci-lint), `fmt`, `tidy`, `docs` (regenerate `docs/`), `snapshot`/`release` (goreleaser), `pr`, `ci`. Lint config
is `.golangci.yaml`; markdown/prettier via `.markdownlint-cli2.yaml` / `.prettierrc.yaml`.

After changing flags or config options, run `make docs` so the committed reference diff lands with the code. After
touching engine output formats, the `e2e/` golden files may need updating (its own module — `cd e2e`).

## Conventions

- Every `internal` package carries a `doc.go` package comment — read it first when entering a package (`tui` is the lone
  exception; its overview is the `app.go` package comment and DESIGN.md → TUI).
- Conventional Commits; commit signing is handed off via a `commit.sh` script (see the global agent instructions) rather
  than committed from a sandbox.
- Clean breaks over backward-compat shims: drop problematic formats rather than add deprecation aliases.
- No `*T`-pointer helper functions (e.g. `func ip(v int) *int { return &v }`). Go 1.26's `new(expr)` allocates and
  initializes in one call, so write `new(1400)` / `new(0.004)` directly. Mind the type: `new(expr)` uses the
  expression's own default type, not the assignment context — for a `*float64` field write `new(1.0)`, not `new(1)`
  (which is `*int`).
