# Behavioral evals

A trigger proves a skill _fires_. A **behavioral eval** (Tier 2) proves it does the job. evolve drops the agent into a
throwaway workspace, hands it a real task with the skill installed, and grades whatever it leaves behind — the files it
wrote, the commands that now pass, the tools it actually reached for.

This page walks from the smallest possible eval up to one that seeds a multi-file project with shared fixtures. Grading
itself — the assertion types and the LLM judge — has its own page, [Assertions](assertions.md); here we cover the case
around them.

## The smallest eval

An eval needs an `id`, a `prompt`, and at least one thing to grade. The leanest cases need no input files at all — a
scaffolding task starts from an empty workspace. This is [`go-project`](https://github.com/bitwise-media-group/skills)'s
`project-scaffold`, trimmed:

```json
{
    "id": "project-scaffold",
    "prompt": "Scaffold a new Go service called orderd with our canonical cmd / tools-module / Makefile layout.",
    "allowed_tools": "Read Write Edit Glob Grep Skill Bash(go *) Bash(gofmt *) Bash(mkdir *)",
    "assertions": [
        { "type": "file_exists", "path": "go.mod" },
        { "type": "file_exists", "path": "cmd/orderd/main.go" },
        { "type": "file_exists", "path": "tools/go.mod" },
        { "type": "regex", "path": "Makefile", "pattern": "^pr:" },
        { "type": "command", "run": "go vet ./...", "requires": "go" }
    ]
}
```

The agent reads the task, the skill teaches it _how_ this project should be laid out, and the assertions check that the
result matches: the files exist, the `Makefile` has a `pr` target, and the whole thing actually vets.

## Anatomy of a case

| Field             | Required        | Meaning                                                                          |
| ----------------- | --------------- | -------------------------------------------------------------------------------- |
| `id`              | yes             | Stable identifier; the results key. Lowercase-kebab by convention                |
| `prompt`          | yes             | The task sent to the agent                                                       |
| `assertions`      | one of these \* | Deterministic + judge checks — see [Assertions](assertions.md)                   |
| `expectations`    | one of these \* | Plain-language statements, each graded by the LLM judge before the assertions    |
| `name`            | no              | Human-readable label surfaced in reports                                         |
| `expected_output` | no              | Prose description of success; context for the judge, never graded on its own     |
| `files`           | no              | Input paths staged into the workspace before the run (below)                     |
| `allowed_tools`   | no              | Space-separated tool allowlist for the agent (e.g. `Read Write Edit Bash(go *)`) |
| `max_turns`       | no              | Cap on agent turns for this case; overrides the run's `--max-turns`              |
| `timeout_seconds` | no              | Wall-clock cap for this case; overrides the run's `--timeout`                    |
| `skip_providers`  | no              | Provider ids to skip for this case                                               |

\* A case must declare at least one `expectations` entry **or** one `assertions` entry, or it fails to load.

`allowed_tools` is worth tuning per case. It both protects the workspace and sharpens the signal: scoping `Bash` to
`Bash(go *) Bash(gofmt *)` lets the agent build and format but not reach for unrelated tooling, so a pass reflects the
skill rather than the model improvising around it.

## Seeding the workspace: `files`

Most evals aren't built from nothing — they hand the agent a starting point to edit. The `files` array lists input
paths, **relative to the eval's own directory**, that evolve stages into the workspace before the run. Where each lands
follows one rule:

- A path under **`files/`** stages at its path _relative to `files/`_, preserving the tree. `files/internal/cli/root.go`
  → `internal/cli/root.go`.
- **Any other path** stages by **basename** at the workspace root. `fixtures/clidemo/go.mod` → `go.mod`.

That single rule is the whole `files`-vs-`fixtures` distinction. By convention:

- **`files/`** mirrors the workspace tree — the source the agent will read and edit, at its real path.
- **`fixtures/<name>/`** holds a shared scaffold (almost always a `go.mod`) that many cases reference to get identical
  build context without duplicating it under each one.

Here is [`go-style`](https://github.com/bitwise-media-group/skills)'s `cli-subcommand`, which seeds an existing CLI and
asks for a new subcommand:

```json
{
    "id": "cli-subcommand",
    "prompt": "Add a serve subcommand (internal/cli/serve.go) with a --port flag, also set via MYCLI_PORT, per our Go style.",
    "allowed_tools": "Read Write Edit Glob Grep Skill Bash(go *) Bash(gofmt *) Bash(mkdir *)",
    "files": ["fixtures/clidemo/go.mod", "files/cmd/mycli/main.go", "files/internal/cli/root.go"],
    "assertions": [
        { "type": "file_exists", "path": "internal/cli/serve.go" },
        { "type": "regex", "path": "internal/cli/serve.go", "pattern": "cobra\\.Command" },
        { "type": "regex", "path": "internal/cli/serve.go", "pattern": "viper" }
    ]
}
```

The workspace the agent sees is:

```text
go.mod                       # from fixtures/clidemo/go.mod (basename)
cmd/mycli/main.go            # from files/cmd/mycli/main.go (path under files/)
internal/cli/root.go         # from files/internal/cli/root.go
```

`clidemo`'s `go.mod` isn't empty — it `require`s cobra and viper — so the staged project compiles and the assertions can
lean on the real toolchain.

### When to reach for `files/` over a fixture

Use `files/` whenever the **path matters** — multi-file layouts, files in subdirectories, or basenames that would
collide. `go-project`'s `pin-tool` is the textbook case: it stages both a root `go.mod` and a `tools/go.mod`.

```json
{
    "id": "pin-tool",
    "prompt": "Pin goreleaser as a developer tool and add a Makefile snapshot target, per our Go tooling conventions.",
    "files": ["files/go.mod", "files/cmd/app/main.go", "files/tools/go.mod", "files/Makefile"]
}
```

Two files are named `go.mod`. Under the basename rule they'd both stage to the workspace root and clobber each other;
under `files/` they keep their distinct paths (`go.mod` and `tools/go.mod`). That's the signal to author them in
`files/` rather than as fixtures.

### Fixtures can hold more than they stage

A `fixtures/<name>/` directory is just a folder of candidate inputs — evolve stages only the paths a case actually
names. `go-testing`'s `fuzzdemo` fixture is a complete, compiling package (`go.mod`, `parse.go`, _and_ a reference
`parse_test.go`), but the `fuzz-target` eval references only `fixtures/fuzzdemo/go.mod`, pairing it with a fresh
`files/parse.go` for the agent to work against:

```json
{
    "id": "fuzz-target",
    "prompt": "Add a fuzz test with a seed corpus for the Parse function to parse_test.go, following our Go fuzzing conventions.",
    "files": ["fixtures/fuzzdemo/go.mod", "files/parse.go"],
    "assertions": [
        { "type": "regex", "path": "parse_test.go", "pattern": "func Fuzz" },
        { "type": "regex", "path": "parse_test.go", "pattern": "f\\.Add\\(" },
        { "type": "command", "run": "go test ./...", "requires": "go" }
    ]
}
```

The extra files in the fixture are convenient for developing and sanity-checking the eval by hand; they simply don't
enter the workspace unless listed.

!!! note "Paths can't escape the workspace"

    `files` entries are resolved relative to the eval directory and staged inside the workspace; a path that would land
    outside it is rejected at load. A leading `evals/` segment is tolerated so skill-creator's skill-root-relative paths
    drop in unchanged.

## Grading the result

Once the agent finishes, the case is graded by its `expectations` and `assertions`:

- **`assertions`** are concrete, mostly deterministic checks — does a file exist, does a pattern match, does
  `go test ./...` pass, did the agent actually call a tool. Cheap, fast, reproducible.
- **`expectations`** are plain-language statements handed to an LLM judge, for the holistic parts a regex can't capture
  ("the summary explains why each test case was chosen").

Both, the full type list, and the judge's mechanics are documented in [Assertions](assertions.md). The golang evals lean
deterministic — `regex` over the edited file plus a `command` that runs `go vet`/`gofmt` — and add a judge check only
where prose is the only way to express success.

## Running them

```sh
evolve run evals --model anthropic,openai --jobs 4 --max-turns 12 --timeout 900
```

`--jobs` sets how many agent runs go in parallel; `--max-turns` and `--timeout` are the per-case defaults a case's own
`max_turns` / `timeout_seconds` override. By default each eval also runs once _without_ the skill installed — its
**baseline** — so reports show the skill's lift, not just its absolute score. The full mechanics of staging, sandboxing,
and grading are in [How evaluations run](execution.md).

Every field above is validated by the
[`evals` JSON Schema](https://raw.githubusercontent.com/bitwise-media-group/evolve/main/schemas/evals.schema.json);
point your editor at it via the `"$schema"` key for completion and inline errors.
