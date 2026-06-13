## evolve

Evaluate coding-agent plugins: static checks, trigger accuracy, behavioral evals, reports

### Options

```
  -h, --help                    help for evolve
      --json                    emit machine-readable JSONL progress on stdout
      --layout string           repository layout: auto, marketplace, multi, or single (default "auto")
      --results-format string   format for results files and the EVALUATION rollup: json, jsonc, or yaml (default: config results_format or json)
      --root string             repository root to operate on (default: walk up from the current directory)
  -v, --verbose                 enable debug logging
```

### SEE ALSO

* [evolve completion](evolve_completion.md)	 - Generate the autocompletion script for the specified shell
* [evolve doctor](evolve_doctor.md)	 - Check each provider: runner CLI on PATH, credential set, counting API reachable
* [evolve models](evolve_models.md)	 - Print the effective provider/model matrix with pricing and provenance
* [evolve report](evolve_report.md)	 - Regenerate EVALUATION.md and EVALUATION.json from the stored results
* [evolve run](evolve_run.md)	 - Run the eval tiers: static checks, trigger accuracy, behavioral evals
* [evolve version](evolve_version.md)	 - Print the build version

