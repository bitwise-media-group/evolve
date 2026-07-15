## evolve run evals

Run Tier 2 behavioral evals: agent sessions graded by assertions

```
evolve run evals [flags]
```

### Options

```
      --baseline               benchmark each eval without the skill (its lift), recomputed only when the eval or its fixtures change (disable with --baseline=false; config: baseline) (default true)
      --count-only             skip agent runs; only compute token usage per model
      --eval string            only run the eval with this id
      --failed                 only run evals that did not pass on a previous run (combine with --new to also rerun missing ones)
      --harness strings        only drive models with these harnesses: claude, codex, gemini, cursor, copilot, antigravity, grok (repeatable / comma-separated; alias: --harnesses; filters within config harnesses)
  -h, --help                   help for evals
      --jobs int               concurrent agent runs (default: ceil(cpus/2)) (default 4)
      --judge-model string     claude model that grades llm assertions (default "claude-sonnet-4-6")
      --keep-workspaces        keep throwaway workspaces for debugging
      --max-turns int          max agent turns per eval (config: max_turns; a per-eval max_turns overrides both) (default 20)
      --model strings          provider ids / canonical model ids, or "all" (repeatable / comma-separated; alias: --models; filters within config models)
      --modified               only run evals whose authored skill content or case definition changed since their stored results
      --new                    only run evals whose stored results are missing values a rerun could fill
      --no-tui                 disable the interactive TUI even on a terminal (also: EVOLVE_NO_TUI=1)
      --plugin strings         only run evals for these plugins (repeatable / comma-separated; alias: --plugins)
      --skill strings          only run evals for these skills (repeatable / comma-separated; alias: --skills)
      --stale-results string   keep|drop stored results for models outside the models restriction (default: prompt on a terminal, else keep)
      --timeout int            seconds per agent run (default 600)
```

### Options inherited from parent commands

```
      --json                    emit machine-readable JSONL progress on stdout
      --layout string           repository layout: auto, marketplace, multi, or single (default "auto")
      --no-sandbox              disable the OS sandbox that confines agent writes to the workspace (config: sandbox.enabled)
      --results-format string   format for results files and the EVALUATION rollup: json, jsonc, or yaml (default: config results_format or json)
      --root string             repository root to operate on (default: walk up from the current directory)
      --strict                  exit 1 when checks or evals fail (default: warn and exit 0)
      --telemetry-dir string    write OpenTelemetry traces/metrics/logs as JSON to this directory (default: off; overrides OTEL_* env vars)
  -v, --verbose                 enable debug logging
```

### SEE ALSO

* [evolve run](evolve_run.md)	 - Run the eval tiers: static checks, trigger accuracy, behavioral evals

