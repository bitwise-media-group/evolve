## evolve view

Browse the stored results in a web browser (filter, sort, snapshot)

### Synopsis

Serve the committed results as an interactive web report: filter by provider, model, plugin, skill, type, and pass/fail; sort and toggle between per-case and rollup views; and save a self-contained HTML snapshot of the current view.

The server is read-only and binds to localhost. While it runs it watches the results files, so a concurrent `evolve run` (or any process that rewrites them) refreshes an open browser. With --out it writes a snapshot file and exits without serving.

```
evolve view [flags]
```

### Options

```
  -h, --help         help for view
      --no-open      do not open the report in a browser
      --out string   write a self-contained HTML snapshot to this path and exit (no server)
      --port int     localhost port to serve on (default: pick a free port)
```

### Options inherited from parent commands

```
      --json                    emit machine-readable JSONL progress on stdout
      --layout string           repository layout: auto, marketplace, multi, or single (default "auto")
      --results-format string   format for results files and the EVALUATION rollup: json, jsonc, or yaml (default: config results_format or json)
      --root string             repository root to operate on (default: walk up from the current directory)
      --telemetry-dir string    write OpenTelemetry traces/metrics/logs as JSON to this directory (default: off; overrides OTEL_* env vars)
  -v, --verbose                 enable debug logging
```

### SEE ALSO

* [evolve](evolve.md)	 - Evaluate coding-agent plugins: static checks, trigger accuracy, behavioral evals, reports

