# Assertions

A Tier 2 eval succeeds when its **assertions** pass. Each assertion is one graded condition checked against the agent's
final response and the throwaway workspace it left behind. There are two families:

- **Deterministic checks** — `file_exists`, `file_absent`, `regex`, `not_regex`, `command`, `tool_call`. These inspect
  files, output, exit codes, or observed tool calls. They are cheap, fast, and reproducible.
- **The LLM judge** — `llm`. A pinned model reads the assertion and the agent's work and returns a verdict. Reserve it
  for holistic or subjective checks the deterministic ones can't express.

Every assertion is one entry in an eval's `assertions` array:

```json
{
    "id": "parametrize",
    "prompt": "Write parametrized tests for the parse function …",
    "files": ["files/src/myapp/kv.py"],
    "assertions": [
        { "type": "file_exists", "path": "tests/test_kv.py" },
        {
            "type": "regex",
            "path": "tests/test_kv.py",
            "pattern": "pytest\\.mark\\.parametrize"
        }
    ]
}
```

An eval must declare at least one `expectations` entry or one `assertions` entry, or loading fails validation.

## How assertions are graded

- **Each assertion is pass / fail / skipped.** Most return pass or fail. A few return _skipped_ when the check can't run
  — a `command` whose `requires` binary is missing, or a `tool_call` against a harness that can't report tool calls.
  Skipped assertions count neither for nor against the case.
- **Order is deterministic.** `expectations` (see [the `llm` section](#llm)) expand to `llm` assertions graded first, in
  authored order, followed by the authored `assertions` in order.
- **Patterns are Go [RE2](https://github.com/google/re2/wiki/Syntax) regexes** — no backreferences or lookaround. In a
  JSON or JSONC file every backslash doubles (`\\.`, `\\s`); YAML single-quoted strings keep one.

The closed set of types is `file_exists`, `file_absent`, `regex`, `not_regex`, `command`, `tool_call`, `llm`. Each is
covered below with a realistic example drawn from the
[bitwise-media-group skills](https://github.com/bitwise-media-group/skills).

## `file_exists`

Passes when `path` (workspace-relative) exists after the run. Use it to assert the agent created the file it was asked
to.

```json
{ "type": "file_exists", "path": "tests/test_kv.py" }
```

| Field  | Required | Meaning                                              |
| ------ | -------- | ---------------------------------------------------- |
| `path` | yes      | Path relative to the eval workspace root to stat for |

## `file_absent`

The mirror of `file_exists`: passes when `path` does **not** exist. Use it to assert the agent _didn't_ create something
it shouldn't have.

```json
{ "type": "file_absent", "path": "internal/version/doc.go" }
```

Here the skill should recognise that a single-file package already carries its package comment, so adding a separate
`doc.go` would be redundant — the assertion fails if the agent adds one anyway.

| Field  | Required | Meaning                                              |
| ------ | -------- | ---------------------------------------------------- |
| `path` | yes      | Path relative to the workspace that must _not_ exist |

## `regex`

Passes when `pattern` matches. With a `path`, the pattern is matched against that workspace file's contents; without a
`path`, it's matched against the agent's final response text. Patterns are compiled in multiline mode, so `^` and `$`
anchor to line boundaries.

```json
{
    "type": "regex",
    "path": "tests/test_kv.py",
    "pattern": "pytest\\.mark\\.parametrize"
}
```

A `path`-less variant checks the agent's own report — for example, that it surfaced the command it ran:

```json
{ "type": "regex", "pattern": "terraform validate" }
```

| Field     | Required | Meaning                                                                   |
| --------- | -------- | ------------------------------------------------------------------------- |
| `pattern` | yes      | RE2 pattern, compiled multiline                                           |
| `path`    | no       | Workspace file to match against; when omitted, matches the agent's output |

A `regex` with a `path` whose file is missing fails (there is nothing to match against).

## `not_regex`

The negation of `regex`: passes when `pattern` does **not** match. Use it to assert an anti-pattern is absent.

```json
{
    "type": "not_regex",
    "path": "tests/test_kv.py",
    "pattern": "unittest|TestCase"
}
```

This guards a pytest suite against stdlib `unittest` leaking in.

!!! warning "A missing file fails `not_regex`"

    When `path` is set, the file is read before matching. If the file doesn't exist the assertion **fails** rather than
    vacuously passing — so `not_regex` with a `path` effectively asserts "the file exists _and_ does not contain this".
    To assert a file is gone, use [`file_absent`](#file_absent).

| Field     | Required | Meaning                                                                   |
| --------- | -------- | ------------------------------------------------------------------------- |
| `pattern` | yes      | RE2 pattern, compiled multiline                                           |
| `path`    | no       | Workspace file to match against; when omitted, matches the agent's output |

## `command`

Runs `run` through `/bin/sh -c` in the workspace and passes when the exit code matches `expect_exit` (default `0`). This
is the strongest deterministic check — it runs the real toolchain over the agent's output.

```json
{
    "type": "command",
    "run": "terraform fmt -recursive -check",
    "requires": "terraform"
}
```

Use `cwd` to run inside a subdirectory of the workspace, and chain a setup step before the check:

```json
{
    "type": "command",
    "run": "terraform init -backend=false -input=false >/dev/null && terraform validate",
    "cwd": "modules/widget",
    "requires": "terraform"
}
```

Set `expect_exit` when success means a non-zero code — for instance, asserting a forbidden token is gone (`grep` exits
`1` when it finds no match):

```json
{ "type": "command", "run": "grep -rq 'TODO' src/", "expect_exit": 1 }
```

| Field         | Required | Meaning                                                                             |
| ------------- | -------- | ----------------------------------------------------------------------------------- |
| `run`         | yes      | Shell command run via `/bin/sh -c`                                                  |
| `cwd`         | no       | Workspace-relative directory to run in (default: the workspace root)                |
| `requires`    | no       | A binary that must be on `PATH`; if absent the assertion is **skipped**, not failed |
| `expect_exit` | no       | Exit code that counts as success (default `0`)                                      |

`requires` keeps suites portable: an eval that needs `terraform` or `python3` skips cleanly on a machine without it
instead of reporting a false failure. The command shares the eval's `timeout_seconds`.

## `tool_call`

Passes when the agent actually invoked a matching tool during the run — inspecting _behaviour_, not just the final
artifact. `tool` is a regex matched against each observed tool name; the optional `pattern` is matched against that
call's JSON-serialized arguments. An MCP tool surfaces as `mcp__<server>__<tool>`.

```json
{ "type": "tool_call", "tool": "^Bash$", "pattern": "terraform validate" }
```

This confirms the agent _ran_ `terraform validate` rather than only claiming to. To assert it reached for a specific MCP
tool:

```json
{ "type": "tool_call", "tool": "mcp__github__create_pull_request" }
```

| Field     | Required | Meaning                                                                        |
| --------- | -------- | ------------------------------------------------------------------------------ |
| `tool`    | yes      | Regex matched against an observed tool name (e.g. `^Bash$`, `mcp__github__.*`) |
| `pattern` | no       | Regex matched against the call's JSON-serialized arguments                     |

Tri-state: if the harness under test can't report tool calls the assertion is **skipped**; if it reports calls but none
match, the assertion **fails**. Unlike `regex`, `tool` and `pattern` are not compiled in multiline mode.

## `llm`

Graded by an LLM judge instead of a fixed rule. The judge is pinned to `claude-sonnet-4-6` regardless of the model under
test, so verdicts stay comparable across providers. It reads the assertion text, the agent's final response, the eval's
`expected_output` (as context, never a separate check), and may `Read`/`Glob`/`Grep` the workspace before returning a
pass/fail verdict with a short evidence quote.

```json
{
    "type": "llm",
    "text": "The new tests cover the error paths, not just the happy path."
}
```

| Field  | Required | Meaning                             |
| ------ | -------- | ----------------------------------- |
| `text` | yes      | The statement the judge must verify |

Reserve `llm` for genuinely subjective or holistic judgements; prefer a deterministic check whenever one can express the
same condition, since each `llm` assertion costs a model call.

### Bare-string shorthand

A plain string anywhere in `assertions` is shorthand for `{ "type": "llm", "text": <string> }`:

```json
"assertions": [
  "The commit message follows Conventional Commits.",
  { "type": "file_exists", "path": "CHANGELOG.md" }
]
```

### `expectations`

`expectations` is a top-level array of statements, each expanded into an `llm` assertion and graded **before** the
authored `assertions`, in authored order. Reports tag these with `source: "expectation"` so they're distinguishable from
inline assertions.

```json
{
    "id": "focused-tests",
    "prompt": "Add tests for the new validator and explain your choices.",
    "expectations": [
        "The summary explains why each test case was chosen.",
        "No production code under src/ was modified."
    ],
    "assertions": [{ "type": "file_exists", "path": "tests/test_validator.py" }]
}
```

`expected_output` is related but different: it's the author's prose description of success. The judge sees it as
context, but it is never graded as its own assertion — so migrating a suite that only has `expected_output` keeps its
pass rate.

## A combined example

A single eval typically mixes families: deterministic checks pin the concrete artifacts, and one judge assertion covers
the part only prose can describe.

```json
{
    "id": "parametrize",
    "prompt": "Write parametrized tests for parse in src/myapp/kv.py, including the error paths, in tests/test_kv.py.",
    "allowed_tools": "Read Write Edit Glob Grep Skill Bash(uv *) Bash(pytest *) Bash(python3 *)",
    "files": ["files/src/myapp/kv.py"],
    "expectations": ["The agent explains which error paths it parametrized and why."],
    "assertions": [
        { "type": "file_exists", "path": "tests/test_kv.py" },
        {
            "type": "regex",
            "path": "tests/test_kv.py",
            "pattern": "pytest\\.mark\\.parametrize"
        },
        {
            "type": "regex",
            "path": "tests/test_kv.py",
            "pattern": "pytest\\.raises"
        },
        {
            "type": "not_regex",
            "path": "tests/test_kv.py",
            "pattern": "unittest|TestCase"
        },
        {
            "type": "command",
            "run": "python3 -m py_compile tests/test_kv.py",
            "requires": "python3"
        }
    ]
}
```

For the case around these assertions — the prompt, input `files`/fixtures, and the per-case run controls — see
[Behavioral evals](evals.md). Every field above is validated by the
[`evals` JSON Schema](https://raw.githubusercontent.com/bitwise-media-group/evolve/main/schemas/evals.schema.json);
point your editor at it via a `"$schema"` key for completion and inline errors.
