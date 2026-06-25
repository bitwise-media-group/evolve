# Viewing in a Browser

`evolve view` serves the committed results as an interactive web report. Where [`evolve report`](index.md) renders a
fixed Markdown rollup, the viewer is for exploring: filter by provider, model, plugin, skill, type, and pass/fail; sort
any column; toggle between a per-case table and a per-model rollup; and save a self-contained HTML snapshot of exactly
what you are looking at.

The server is **read-only** and binds to `localhost` — it reads the same [`results.<ext>`](../evaluations/results.md)
files the report does and never launches runs or mutates anything.

```sh
evolve view                      # build the report, serve it, open a browser
evolve view --no-open            # serve without opening a browser
evolve view --port 8099          # bind a fixed port (default: a free one)
evolve view --out report.html    # write a self-contained snapshot and exit (no server)
```

| Flag        | Description                                                                  |
| ----------- | ---------------------------------------------------------------------------- |
| `--out`     | Write a self-contained HTML snapshot to this path and exit, without serving. |
| `--port`    | Localhost port to serve on (default: pick a free port).                      |
| `--no-open` | Do not open the report in a browser.                                         |

## The interface

A column of filters runs down the left: one always-visible group per facet — **Provider**, **Model**, **Plugin**,
**Skill**, **Type**, **Status** — each listing every value with a live count. Selections multi-select (checkboxes, no
dropdowns): values within a facet are ORed, facets are ANDed, and each count reflects what selecting it would yield
given the other facets' current state.

The table fills the rest. Click any column header to sort ascending, again for descending. A **Cases / Rollup** toggle
switches granularity:

- **Cases** — one row per trigger query or eval case, with its own pass/fail status and metrics. This is the finest
  view; the Status filter operates at this level.
- **Rollup** — one row per (plugin, skill, model, tier) with pass/fail/error tallies, pass rate, average run time, and
  total cost. Click a group to expand its member cases.

Missing figures render as `—`, matching the [Markdown report](index.md).

## Live updates

While `evolve view` runs it watches the results files on disk. Any process that rewrites them — a concurrent
`evolve run`, a teammate's commit you just pulled, a CI artifact — pushes a refresh to the open browser over Server-Sent
Events, so the table stays current without a manual reload. The watch is decoupled from the engine: it works regardless
of how the results were produced.

## Snapshots

The **Snapshot** button (and `evolve view --out`) writes a single self-contained HTML file: the whole dataset and the
current filters/sort are inlined, so it opens offline over `file://` with your view pre-applied — and the recipient can
still re-filter it freely. It is the shareable, frozen counterpart to the live server.

!!! note "Snapshots need the bundled UI"

    The viewer's web assets are embedded into the binary at build time. Release binaries (and a local `make build`)
    carry them; a bare `go install` build does not, and `evolve view` there reports that the UI is not bundled. Use a
    release binary to browse and snapshot.
