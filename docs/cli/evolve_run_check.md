## evolve run check

Run Tier 0 static checks: skill frontmatter, manifests, marketplace consistency

```
evolve run check [flags]
```

### Options

```
  -h, --help             help for check
      --no-marketplace   skip marketplace manifest validation
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

