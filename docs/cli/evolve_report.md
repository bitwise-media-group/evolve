## evolve report

Regenerate EVALUATION.md and EVALUATION.json from the stored results

```
evolve report [flags]
```

### Options

```
      --check                          fail when pass rates breach the configured thresholds
      --cobertura string               also write a Cobertura XML coverage file to this path (overrides report.cobertura)
  -h, --help                           help for report
      --junit string                   also write a JUnit XML test-results file to this path (overrides report.junit)
      --migrate                        upgrade stored results files to the latest schema before generating the reports
      --min-evals-pass-rate float      minimum eval pass rate (0..1) for --check
      --min-triggers-pass-rate float   minimum trigger pass rate (0..1) for --check
      --stale-results string           keep|drop stored results for models outside the models restriction (default: prompt on a terminal, else keep)
      --strict                         require the configured model matrix: --check holds every defined model to the thresholds, and --cobertura covers a skill only when every defined model has a current result
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

