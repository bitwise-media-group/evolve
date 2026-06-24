# Authoring evaluations

evolve grades a coding-agent skill across three tiers: **Tier 0** static checks, **Tier 1** trigger accuracy, and **Tier
2** behavioral evals. You author the Tier 1 and Tier 2 definitions; evolve runs them and writes committed results. This
section walks the whole path, smallest first.

## Where to start

1. **[Triggers](triggers.md)** — the cheapest eval and the place to begin: does the skill fire for the right prompts and
   stay quiet for the wrong ones?
2. **[Behavioral evals](evals.md)** — drive the agent on a real task in a throwaway workspace, then grade what it
   produced; this is where input files and fixtures come in.
3. **[Assertions](assertions.md)** — the full set of graded conditions, from deterministic file/regex/command checks to
   the LLM judge.
4. **[How evaluations run](execution.md)** — what evolve actually does at runtime: the temporary workspace, the agent
   invocation, grading, the baseline, and how results are written.

## The eval directory

Each eval directory holds the authored definitions plus the committed results the sweeps write:

```text
evals/<skill>/
├── triggers.<ext>     # Tier 1 — trigger-accuracy prompts
├── evals.<ext>        # Tier 2 — behavioral eval cases
├── files/             # optional — source staged into the workspace at its real path
├── fixtures/<name>/   # optional — shared scaffolds (e.g. a go.mod) staged by basename
└── results.<ext>      # committed model results
```

Supported formats are `json`, `jsonc`, `yaml` and `yml`; for a given basename, only one matching file may exist. Point
your editor at the schemas in [`schemas/`](https://github.com/bitwise-media-group/evolve/tree/main/schemas) via a
`"$schema"` key for validation and completion.

## Results

One committed `results.<ext>` per skill stores both eval kinds, keyed by `provider/model-id`. A sweep rewrites only the
entries it ran, so diffs stay scoped. Output is deterministic — sorted keys, fixed field order, rounded floats, trailing
newline — so reports re-render identically as the live matrix moves. [How evaluations run](execution.md#writing-results)
covers the write step in detail.

!!! tip "Reruns & resuming"

    `--new` runs only work with missing or stale results; `--modified` reruns only cases whose authored content changed
    since their stored results; `--failed` reruns only cases that didn't pass. All keep finished entries, so an
    interrupted sweep resumes cleanly.
