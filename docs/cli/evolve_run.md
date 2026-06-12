## evolve run

Run the eval tiers: static checks, trigger accuracy, behavioral cases

### Options

```
  -h, --help     help for run
      --strict   exit 1 when checks or evals fail (default: warn and exit 0)
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
* [evolve run all](evolve_run_all.md)	 - Run everything: check, triggers, cases, then regenerate reports
* [evolve run cases](evolve_run_cases.md)	 - Run Tier 2 behavioral evals: agent sessions graded by assertions
* [evolve run check](evolve_run_check.md)	 - Run Tier 0 static checks: skill frontmatter, manifests, marketplace consistency
* [evolve run triggers](evolve_run_triggers.md)	 - Run Tier 1 trigger-accuracy evals through headless agent sessions

