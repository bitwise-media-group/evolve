# Authoring evaluations

Each eval directory holds the authored definitions plus the committed results the sweeps write:

```text
evals/<skill>/
├── triggers.<ext>     # Tier 1 — trigger-accuracy prompts
├── evals.<ext>        # Tier 2 — behavioral eval cases
└── results.<ext>      # committed model results
```

Supported formats are `json`, `jsonc`, `yaml` and `yml`; for a given basename, only one matching file may exist.

## Triggers (Tier 1)

`triggers.<ext>` lists prompts and the skill expected to activate. `evolve run triggers` runs each prompt several times
(`--runs N`) and records whether the expected skill fired — scored per model.

```sh
evolve run triggers --model anthropic,openai --runs 5
```

## Evals (Tier 2)

`evals.<ext>` lists behavioral cases. `evolve run evals` runs each case in a throwaway workspace, then grades the output
with deterministic **assertions** (files, regexes, commands) and any configured **LLM judge**. Per-case knobs include
`max_turns`, `timeout_seconds`, `allowed_tools` and `skip_providers`.

```sh
evolve run evals --model anthropic,openai --jobs 4 --max-turns 12 --timeout 900
```

## Results

One committed `results.<ext>` per skill stores both eval kinds, keyed by `provider/model-id`. A sweep rewrites only the
entries it ran, so diffs stay scoped. Output is deterministic — sorted keys, fixed field order, rounded floats, trailing
newline — so reports re-render identically as the live matrix moves.

!!! tip "Reruns & resuming" `--new` runs only work with missing or stale results; `--modified` reruns only cases whose
authored content changed since their stored results. Both keep finished entries, so an interrupted sweep resumes
cleanly.

JSON Schemas for every authored and emitted file live in `schemas/` — point your editor at the raw URLs via a
`"$schema"` key for validation and completion.
