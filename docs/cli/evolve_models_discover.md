## evolve models discover

List vendor-served models and add new ones to the .evolve config

### Synopsis

Discover queries each vendor's model-listing API (using the same credentials
as token counting; vendors without a set key are skipped) and marks every model
already in the effective registry with where it lives (builtin or the config
file).

Interactively, the models render in a fuzzy-find picker: type to filter, tab to
select any number of new models, enter to write them into the repository's
.evolve config (created as .evolve.yaml when missing). Because a
providers.<id>.models override replaces that provider's builtin list, the first
injected entry for a provider also seeds the list with its builtin models, so
the effective matrix only ever grows.

Vendor listing APIs publish no pricing, so injected entries carry none until
edited by hand; their costs render as unpublished in reports.

```
evolve models discover [flags]
```

### Options

```
  -h, --help     help for discover
      --no-tui   print the discovered models instead of opening the interactive picker
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

* [evolve models](evolve_models.md)	 - Print the effective model matrix with pricing, harnesses, and provenance

