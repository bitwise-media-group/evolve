## evolve run checks

Run Tier 0 static checks: skill frontmatter, manifests, marketplace consistency

```
evolve run checks [flags]
```

### Options

```
  -h, --help             help for checks
      --license string   license every SKILL.md must declare; overrides checks.license (default: the field is forbidden)
      --no-marketplace   skip marketplace manifest validation
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

