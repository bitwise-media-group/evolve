# Triggers

Triggers are the **Tier 1** eval, and the right place to start. A trigger suite asks one question of a skill: do the
prompts that _should_ activate it actually do — and do the prompts that shouldn't leave it alone? It is the cheapest
signal worth measuring. A skill that never fires is dead weight; one that fires on everything is noise. Trigger accuracy
catches both before you spend a single behavioral run.

No code runs and nothing is graded by a model here — evolve drives the agent with each query against a workspace that
holds _every_ skill, and watches whether your skill is the one it reaches for.

## The file

Authored triggers live beside the skill they exercise:

```text
evals/<skill>/triggers.<ext>     # json, jsonc, yaml, or yml — one file per basename
```

The envelope is a `triggers` array; `skill_name` is an optional echo of the directory name (the directory stays
authoritative, and a mismatch only warns):

```json
{
    "$schema": "https://raw.githubusercontent.com/bitwise-media-group/evolve/main/schemas/triggers.schema.json",
    "skill_name": "go-testing",
    "triggers": [
        { "query": "Add table-driven tests for this Go parser", "should_trigger": true },
        { "query": "Add a Go fuzz test for this parsing function", "should_trigger": true },
        { "query": "Write pytest tests for this module", "should_trigger": false },
        { "query": "Set up the GitHub Actions release workflow for our Go repo", "should_trigger": false }
    ]
}
```

| Field            | Required | Meaning                                                                    |
| ---------------- | -------- | -------------------------------------------------------------------------- |
| `query`          | yes      | The prompt sent to the agent, verbatim                                     |
| `should_trigger` | yes      | Whether the skill under test is _expected_ to activate for this query      |
| `skip_providers` | no       | Provider ids to exclude for this one query (e.g. a model that can't judge) |

## Your first triggers

Start with a handful of **positives** — the real phrasings a user would type when they want this skill. Vary them: an
imperative ("Add table-driven tests for this Go parser"), a question ("How do I run a single fuzz target with go
test?"), and a review-style ask ("Review my Go tests — should I be using testify here?"). The set above is drawn from
[`go-testing`](https://github.com/bitwise-media-group/skills); each names the language and the task concretely, which is
how a user actually asks.

A positives-only suite looks great and tells you nothing — every skill scores 100% if the bar is "fire on anything
plausible." The signal is in the negatives.

## Negatives are where the signal lives

The hard cases are the **near-misses**: prompts close enough to tempt a false activation. The most valuable ones come
from a plugin's _sibling_ skills. The golang plugin ships five skills, so each one's negatives are largely the others'
positives. From [`go-style`](https://github.com/bitwise-media-group/skills):

```json
{
    "triggers": [
        { "query": "Refactor this Go code to wrap errors properly", "should_trigger": true },
        { "query": "Convert these log.Printf calls to slog", "should_trigger": true },

        { "query": "Generate markdown documentation for my cobra CLI", "should_trigger": false },
        { "query": "Write table-driven tests for this Go function", "should_trigger": false },
        { "query": "Set up GoReleaser for my Go project", "should_trigger": false },
        { "query": "Scaffold a new Go service with cmd and internal directories", "should_trigger": false },
        { "query": "Refactor this Rust code to use idiomatic error handling", "should_trigger": false }
    ]
}
```

Every negative is deliberate. The first four belong to a _different_ golang skill — `go-docs`, `go-testing`,
`go-release`, `go-project` — so they pin the boundary between adjacent skills, the place real activation mistakes
happen. The last is the same task (idiomatic error handling) in the wrong language. Two patterns to copy:

- **Cross-list siblings.** For each skill, add its siblings' headline positives as your negatives. If `go-style` fires
  on "Set up GoReleaser", that's the bug the `go-release` positive would hide.
- **Same task, wrong domain.** Take a real positive and swap the language or framework ("Add structured logging to my
  Express app" for `go-style`). These catch a skill that keys off the verb instead of the context.

!!! tip "Aim for a balanced set"

    A suite that is 90% positives over-reports accuracy. Roughly matching positives and negatives — and weighting the
    negatives toward sibling skills — makes the score mean something.

## Running them

```sh
evolve run triggers --model anthropic,openai --runs 5
```

Each query is run `--runs` times per model (default `3`). Use an odd number so a query can't land on a 50/50 tie, and
raise it when you want to separate a flaky skill from a decisive one — `--runs` trades cost for confidence.

## How a query is scored

- **Each run is a hit or a miss.** A _hit_ means the agent reached for your skill — it invoked the `Skill` tool for it,
  or opened its `SKILL.md`. (The exact detection is covered in [How evaluations run](execution.md).)
- **A query passes when its hit-rate falls on the expected side of 50%:** `≥ 0.5` when `should_trigger` is `true`,
  `< 0.5` when it is `false`. So a `should_trigger: true` query with 3-of-5 hits passes; a `should_trigger: false` query
  needs a _minority_ of hits.
- **The skill's per-model trigger score is the share of queries that passed.** Reports break it down so you can see
  exactly which query dragged it down.

Because scoring is per-query and the same prompts run for every model, two models' trigger scores are directly
comparable — that is the whole point of pinning the queries rather than the activations.

## Skipping a provider

If one provider can't fairly run a query, exclude it for that query with `skip_providers` (provider ids, e.g.
`"anthropic"`, `"openai"`). The query still runs for everyone else, and the skipped provider's score is computed over
the queries it did run.

```json
{ "query": "Add a seed corpus to this Go fuzz target", "should_trigger": true, "skip_providers": ["google"] }
```

---

Once a skill triggers reliably, the next question is whether it does the _job_ — that's a behavioral eval. Continue to
[Behavioral evals](evals.md). Every field above is validated by the
[`triggers` JSON Schema](https://raw.githubusercontent.com/bitwise-media-group/evolve/main/schemas/triggers.schema.json);
point your editor at it via the `"$schema"` key for completion and inline errors.
