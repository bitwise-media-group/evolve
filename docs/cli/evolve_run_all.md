## evolve run all

Run everything: check, triggers, cases, then regenerate reports

```
evolve run all [flags]
```

### Options

```
      --count-only        skip agent runs; only compute token usage per model
  -h, --help              help for all
      --jobs int          concurrent agent runs (default: ceil(cpus/2)) (default 4)
      --keep-workspaces   keep throwaway workspaces for debugging
      --models string     comma-separated provider names / model ids, or "all" (default: config default_models or "anthropic")
      --new               only run evals whose stored results are missing values a rerun could fill
      --runs int          runs per query (triggers tier) (default 3)
      --skill string      only run evals for this skill
      --timeout int       seconds per agent run (default 120 triggers, 600 cases)
```

### Options inherited from parent commands

```
      --json            emit machine-readable JSONL progress on stdout
      --layout string   repository layout: auto, marketplace, multi, or single (default "auto")
      --root string     repository root to operate on (default: walk up from the current directory)
      --strict          exit 1 when checks or evals fail (default: warn and exit 0)
  -v, --verbose         enable debug logging
```

### SEE ALSO

* [evolve run](evolve_run.md)	 - Run the eval tiers: static checks, trigger accuracy, behavioral cases

