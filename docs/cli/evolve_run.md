## evolve run

Run the eval tiers: static checks, trigger accuracy, behavioral evals

### Options

```
  -h, --help         help for run
      --no-sandbox   disable the OS sandbox that confines agent writes to the workspace (config: sandbox.enabled)
      --strict       exit 1 when checks or evals fail (default: warn and exit 0)
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
* [evolve run all](evolve_run_all.md)	 - Run everything: checks, triggers, evals, then regenerate reports
* [evolve run checks](evolve_run_checks.md)	 - Run Tier 0 static checks: skill frontmatter, manifests, marketplace consistency
* [evolve run evals](evolve_run_evals.md)	 - Run Tier 2 behavioral evals: agent sessions graded by assertions
* [evolve run triggers](evolve_run_triggers.md)	 - Run Tier 1 trigger-accuracy evals through headless agent sessions

