## evolve report

Regenerate EVALUATION.md and EVALUATION.json from the stored results

```
evolve report [flags]
```

### Options

```
      --check                          fail when pass rates breach the configured thresholds
  -h, --help                           help for report
      --min-cases-pass-rate float      minimum case pass rate (0..1) for --check
      --min-triggers-pass-rate float   minimum trigger pass rate (0..1) for --check
```

### Options inherited from parent commands

```
      --json            emit machine-readable JSONL progress on stdout
      --layout string   repository layout: auto, marketplace, multi, or single (default "auto")
      --root string     repository root to operate on (default: walk up from the current directory)
  -v, --verbose         enable debug logging
```

### SEE ALSO

* [evolve](evolve.md)	 - Evaluate coding-agent plugins: static checks, trigger accuracy, behavioral cases, reports

