## evolve run evals

Run Tier 2 behavioral evals: agent sessions graded by assertions

```
evolve run evals [flags]
```

### Options

```
      --count-only           skip agent runs; only compute token usage per model
      --eval string          only run the eval with this id
  -h, --help                 help for evals
      --jobs int             concurrent agent runs (default: ceil(cpus/2)) (default 4)
      --judge-model string   claude model that grades llm assertions (default "claude-sonnet-4-6")
      --keep-workspaces      keep throwaway workspaces for debugging
      --models string        comma-separated provider names / model ids, or "all" (default: config default_models or "anthropic")
      --new                  only run evals whose stored results are missing values a rerun could fill
      --skill string         only run evals for this skill
      --timeout int          seconds per agent run (default 600)
```

### Options inherited from parent commands

```
      --json                    emit machine-readable JSONL progress on stdout
      --layout string           repository layout: auto, marketplace, multi, or single (default "auto")
      --results-format string   format for results files and the EVALUATION rollup: json, jsonc, or yaml (default: config results_format or json)
      --root string             repository root to operate on (default: walk up from the current directory)
      --strict                  exit 1 when checks or evals fail (default: warn and exit 0)
  -v, --verbose                 enable debug logging
```

### SEE ALSO

* [evolve run](evolve_run.md)	 - Run the eval tiers: static checks, trigger accuracy, behavioral evals

