## evolve report

Regenerate EVALUATION.md and EVALUATION.json from the stored results

```
evolve report [flags]
```

### Options

```
      --check                          fail when pass rates breach the configured thresholds
  -h, --help                           help for report
      --min-evals-pass-rate float      minimum eval pass rate (0..1) for --check
      --min-triggers-pass-rate float   minimum trigger pass rate (0..1) for --check
      --stale-results string           keep|drop stored results for models outside the models restriction (default: prompt on a terminal, else keep)
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

