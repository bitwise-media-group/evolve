## evolve run triggers

Run Tier 1 trigger-accuracy evals through headless agent sessions

```
evolve run triggers [flags]
```

### Options

```
      --count-only             skip agent runs; only compute token usage per model
      --failed                 only run evals that did not pass on a previous run (combine with --new to also rerun missing ones)
  -h, --help                   help for triggers
      --jobs int               concurrent agent runs (default: ceil(cpus/2)) (default 4)
      --keep-workspaces        keep throwaway workspaces for debugging
      --max-turns int          max agent turns per eval (config: max_turns; a per-eval max_turns overrides both) (default 20)
      --model strings          provider names / model ids, or "all" (repeatable / comma-separated; alias: --models; default: config default_models or "anthropic")
      --modified               only run evals whose authored skill content or case definition changed since their stored results
      --new                    only run evals whose stored results are missing values a rerun could fill
      --no-tui                 disable the interactive TUI even on a terminal (also: EVOLVE_NO_TUI=1)
      --plugin strings         only run evals for these plugins (repeatable / comma-separated; alias: --plugins)
      --runs int               runs per query (default 3)
      --skill strings          only run evals for these skills (repeatable / comma-separated; alias: --skills)
      --stale-results string   keep|drop stored results for models outside default_models (default: prompt on a terminal, else keep)
      --timeout int            seconds per agent run (default 120)
```

### Options inherited from parent commands

```
      --json                    emit machine-readable JSONL progress on stdout
      --layout string           repository layout: auto, marketplace, multi, or single (default "auto")
      --no-sandbox              disable the OS sandbox that confines agent writes to the workspace (config: sandbox.enabled)
      --results-format string   format for results files and the EVALUATION rollup: json, jsonc, or yaml (default: config results_format or json)
      --root string             repository root to operate on (default: walk up from the current directory)
      --strict                  exit 1 when checks or evals fail (default: warn and exit 0)
  -v, --verbose                 enable debug logging
```

### SEE ALSO

* [evolve run](evolve_run.md)	 - Run the eval tiers: static checks, trigger accuracy, behavioral evals

