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
./cmd/evolve/runui.go  # TUI gating + the form -> engine -> dashboard wiring for `run`
./cmd/evolve/docs.go   # hidden command that regenerates docs/cli, docs/man, and docs/config

./internal/cli/...     # shared command plumbing: global Options, config layering,
                       # provider/repo/threshold resolution
./internal/run/...     # the three eval engines: checks, triggers, evals
./internal/tui/...     # interactive selection form + live run dashboard (bubbletea)
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

Because `runner` is that single exec chokepoint, it also enforces filesystem isolation: agent CLIs run in full-auto
(`--dangerously-skip-permissions` and the like), so `cmd.Dir` alone would not stop a run from wandering into other
checkouts. When `Exec.Sandbox` is enabled (the default), every command is wrapped in an OS sandbox — `sandbox-exec` on
macOS, `bubblewrap` on Linux — that denies writes under the configured `sandbox.protected_roots` (default: the parent of
the repo under test) while re-permitting the per-run workspace. It is a denylist, not an allowlist: reads, the network,
and writes to dependency caches stay open so build tooling (`go mod download`, `npm ci`, `uv sync`, `terraform init`,
and unknown future tools) keeps working — the sandbox only protects source repositories. It fails closed (an enabled
sandbox with no available helper errors rather than running unconfined); `--no-sandbox` / `sandbox.enabled=false` opts
out.

Several agent CLIs sandbox their own shell commands the same way (Claude Code and codex both use macOS Seatbelt), and
Seatbelt cannot nest — a second `sandbox-exec` inside evolve's aborts every shell command with
`Operation not permitted`, silently degrading a run rather than failing it. So when evolve's sandbox is active the
providers disable the agent's own (`run.Options.HostSandboxed`, threaded into `TriggerSpec`/`EvalSpec`): Claude via
`--settings` with `{"sandbox":{"enabled":false}}`, codex via `--sandbox danger-full-access`, gemini via
`GEMINI_SANDBOX=false`. evolve's outer sandbox is then the sole layer and still covers everything (file tools included,
not just shell). The fallback is symmetric: with evolve unconfined (`--no-sandbox`) the agent keeps its own sandbox as
the only protection. A `managed-settings.json` that forces Claude's sandbox on still wins, so those hosts must use
`--no-sandbox`.

The CLI reference in `docs/cli` and the man pages in `docs/man` are generated from the cobra command tree, and the
configuration reference plus annotated example config files in `docs/config` from `internal/configdoc`'s schema (all via
`make docs`) and committed, so reviewing a flag or config change shows the documentation diff alongside the code.

## TUI

`evolve run triggers`, `run evals`, and `run all` show an interactive full-screen UI — a selection form, then a live run
dashboard — when stdout is a real terminal and the user has not opted out (`--no-tui` / `EVOLVE_NO_TUI`). The check is
`interactiveTUI` in `cmd/evolve/runui.go`; when it returns false the historical line-based path runs unchanged. The UI
is built on
[bubbletea](https://github.com/charmbracelet/bubbletea)/[lipgloss](https://github.com/charmbracelet/lipgloss) and lives
entirely in `internal/tui`, which is a presentation layer over `internal/run` — it computes nothing about a run itself,
it only displays what the engine reports.

### The reporter seam

`run.Reporter` (`internal/run/reporter.go`) is the single contract between the engine and any front end. The engine
never writes progress to stdout directly; it calls `UnitStarted` / `UnitSkipped` / `ItemStarted` / `ItemDone` /
`UnitFinished` / `Warn`. Two implementations exist:

- `run.PlainReporter` — the default when `Options.Reporter` is nil, reproducing the legacy line output exactly, so
  non-TTY runs and the engine tests are untouched by the indirection.
- `tui.tuiReporter` — forwards each call into the bubbletea program as a message via `program.Send`, which is
  goroutine-safe. That matters because `ItemDone` and `Warn` fire from the parallel agent-run goroutines (`--jobs`).

Because the seam is the only coupling, the same `run.Sweep` drives either output with no engine changes.

### Process model

`runWithUI` (`cmd/evolve/runui.go`) runs two goroutines joined by channels:

- The main goroutine runs `p.Run()` — bubbletea's event loop and renderer.
- An engine goroutine blocks on the `runReq` channel. When the user chooses RUN the form sends a `tui.RunRequest`; the
  goroutine invokes the `engine` callback (which calls `run.Sweep` with the reporter attached), then sends
  `tui.RunDone(...)` back into the program.

Quitting is cooperative: closing `progExited` releases the engine goroutine if the user cancels before running;
`cancel()` on the command context stops a sweep already in flight (a resulting `context.Canceled` is swallowed — a user
quit is not an error); `<-engineDone` joins before returning. Token-counter diagnostics are routed through a
`switchWriter` that starts at `io.Discard` and is repointed at `forward(rep)` once the run begins, turning each counter
line into a `Warn` event so it surfaces in the dashboard rather than corrupting the alt-screen.

### Root model and screens

`tui.Model` (`app.go`) is the bubbletea root. It holds two sub-models and a `screen` that advances
`screenForm -> screenDashboard`. `Update` routes by message type: `WindowSizeMsg` fans the size out to both sub-models;
`spinner.TickMsg` drives the dashboard spinner while a run is live; `KeyMsg` is delegated to whichever screen is active;
and the progress messages (`unitStartedMsg`, `itemDoneMsg`, …, `runDoneMsg` — all defined in `messages.go`, one per
`Reporter` method) are applied to the dashboard. `startRun` builds the execution plan with `run.PlanFor` per selected
model, constructs the dashboard, and `tea.Batch`es dispatching the request with starting the spinner.

### Selection form

`formModel` (`form.go`) is a three-pane, lazygit-style screen: a providers/models tree on the left, and triggers and
evals trees stacked on the right. All three are the generic collapsible checkbox `tree` (`tree.go`) whose leaves carry a
tri-state (`nodeOff` / `nodePartial` / `nodeOn`). The initial selection is _derived_, not blank: `run.Needs` returns the
per-model run matrix the non-TUI path would execute (honouring `--new`/`--failed`/`--modified`, `--skill`, `--eval`),
and `deriveStates` turns it into the leaf states — so the form opens already matching flag-only mode, with `--new`'s
partial units shown as partial. `request()` walks the final selection into a `tui.RunRequest` carrying a _per-model_
`run.Filter`, letting each model run a different set of cases (needed so `--new` reruns only the stale units while a
peer runs everything).

### Live dashboard

The dashboard is split across two files: `dashboard.go` holds the state, message handling, and key handling;
`dashboard_view.go` holds all rendering. It is constructed from the plan at run start (`newDashboard`):

- `unitState` is one (skill, model, tier) execution unit; its `caseState` rows are pre-populated from the catalog —
  mirroring the engine's per-provider skips and the selection filter — so pending cases render with their real labels
  before they run, and live updates are matched back by label.
- `apply(msg)` is the reducer: each progress message mutates unit/case status, tallies, metrics, in-flight tracking, and
  the warning ring buffer.
- Units are execution-ordered and grouped plugin → skill → model for the left "Execution" pane. `buildNodeRefs`
  collapses settled and not-yet-started groups to a single row and expands only the active one, and the highlight
  auto-follows the live case until the user moves it (`manual`).
- The view is a header line, the left execution pane (per-plugin progress bars plus the expanded active branch), a right
  column split into a tabbed "Rollup" (Summary / Providers / Plugins / Skills) and an "Executing" detail pane (in-flight
  cases plus the highlighted case's authored spec), and a footer of key hints.
- `now func() time.Time` is injected so elapsed-time rendering is deterministic under test.

### Rendering primitives and tests

`panel.go` draws every framed box — a rounded border with the title, count, and tab strip embedded in the border edges,
lazygit-style — and `panelContentWidth` is the single source of truth for body sizing. `styles.go` holds the ANSI-256
palette (chosen to degrade on limited terminals) plus the cyberdream accent colours for the dashboard's panel borders,
and `util.go` the width-aware `truncate`/`clip` helpers. `tui_test.go` exercises the models directly — feeding `KeyMsg`s
and `Reporter` messages into `Update`/`apply` and asserting on `view()` output — so the whole UI is tested without a
terminal.
