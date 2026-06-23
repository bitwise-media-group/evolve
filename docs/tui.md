# TUI

On an interactive terminal, `evolve run triggers`, `run evals` and `run all` open a full-screen TUI: first a **selection
form** to scope the run, then a **live dashboard** that streams results as agents finish. Pass `--no-tui` (or set
`EVOLVE_NO_TUI=1`) for the plain line-based output used in CI and non-TTY pipes — both paths drive the same engine, so
the run is identical either way.

## Selection form

![The Evolve selection form](assets/form.png){ loading=lazy }

A set of focusable panes you tab between to choose what runs:

- **Filters** — the same `new` / `modified` / `failed` scoping the run flags expose.
- **Harnesses** — the agent CLIs to drive; any whose CLI is off `PATH` is shown disabled.
- **Models** — individual models grouped under a per-provider header row, so you can toggle one model or a whole
  provider at once. Models unsupported by the enabled harnesses are shown disabled.
- **Plugins / Skills / Cases** — a tree of every trigger and behavioral case. Each row shows whether it is forced on,
  forced off, or auto-queued for all / some / none of the enabled models; a legend under the tree names every glyph.

| Keys                         | Action                       |
| ---------------------------- | ---------------------------- |
| `tab` / `shift+tab`, `1`–`4` | Move between / jump to panes |
| `↑` `↓` / `j` `k`            | Move within a pane           |
| `←` `→` / `h` `l`            | Fold the tree                |
| `space`                      | Toggle                       |
| `g` / `G`                    | Jump to the ends             |
| `r`                          | Run                          |
| `esc`                        | Cancel                       |

The form previews exactly what will execute — it and the engine resolve through the same plan, so they cannot drift.

## Live dashboard

![The Evolve live run dashboard](assets/dashboard.png){ loading=lazy }

Once a run starts, the dashboard streams progress:

- A **title bar** with running pass / fail / error tallies, elapsed time, rolled-up cost, and an overall progress bar.
- An **Execution** tree (plugin → skill → model → case) carrying per-node rollup columns.
- A tabbed **Rollup** (Summary / Providers / Plugins / Skills), a **Runs** log of every execution in plan order, and a
  **Details** pane showing in-flight cases and the selected case's authored spec.

Selecting a run in any pane moves the selection everywhere.

| Keys                   | Action                            |
| ---------------------- | --------------------------------- |
| `f`                    | Follow the live execution         |
| `enter`                | Jump to the selected run's detail |
| `g` / `G`, `^d` / `^u` | Scroll                            |

See **DESIGN.md → TUI** in the repo for the full wiring.
