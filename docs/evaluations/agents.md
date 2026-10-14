# Hosted agents

Plugins and skills run inside an agent CLI on your machine. A **hosted agent** is different: it is already deployed
behind a protocol endpoint, and inference happens on its server. evolve evaluates it the same way it evaluates a skill —
behavioral evals graded by assertions, committed results, a report — but each eval is a **scripted conversation** with
the endpoint instead of a CLI session, and the run matrix is the agent's **endpoints** instead of a list of models.

The first supported protocol is [AG-UI 1.0](https://docs.ag-ui.com/spec/1.0). evolve speaks it over HTTP + SSE, answers
the agent's human-in-the-loop (HIL) requests from pre-canned responses, and checks every event it receives against the
1.0 rules.

!!! note "evolve does not start the agent"

    Endpoints must already be serving when a sweep starts. evolve sends prompts and HIL answers; it never deploys, builds,
    or launches the agent from its definition.

## Repository layout

A repository holds plugins **or** agents, never both. Each agent's evals sit beside its declarative definition:

=== "Several agents (`agents`)"

    ```text
    agents/
    ├── support/
    │   ├── agent.py            # the agent's definition — whatever your framework uses
    │   ├── SYSTEM_PROMPT.md
    │   ├── evals.json          # the agent suite
    │   ├── files/              # optional fixtures for command assertions and the judge
    │   └── results.json        # committed results
    └── billing/
        └── …
    ```

    One agent per directory under `agents/`; a directory counts only when it holds an `evals.<ext>`. The agent's name is
    its directory name.

=== "One agent (`agent`)"

    ```text
    agent/
    ├── agent.py
    ├── SYSTEM_PROMPT.md
    ├── evals.json
    └── results.json
    ```

    The repository is the agent. It is named by `agent.name` in the evals file, or else the repository root's basename.

Plugin markers win over agent markers, so a plugin whose subagents live in an `agents/` folder (which holds no evals
file) still detects as a plugin. `--layout agents` or `--layout agent` forces the kind.

Agent repositories write only the root `EVALUATION.md` / `EVALUATION.<format>`; the agent tables and each agent's case
tables live in that one report.

## The agent suite

An evals file is explicitly one kind. A **skill suite** is `{skill_name?, models?, evals}`; an **agent suite** is
`{agent, evals}` — the `agent` block is what makes it one, and `skill_name`/`models` are rejected beside it.
`run checks` reports a mismatch as an error: an agent block in a plugin repository, or a missing one in an agent
repository.

```json
{
    "agent": {
        "name": "support",
        "protocol": "ag-ui",
        "endpoints": {
            "local": { "url": "http://localhost:8000/" },
            "staging": {
                "url": "https://staging.example.com/agents/support",
                "display": "Support (staging)",
                "headers": { "Authorization": "Bearer ${SUPPORT_AGENT_TOKEN}" },
                "forwarded_props": { "tenant": "eval" }
            }
        }
    },
    "evals": [
        {
            "id": "refund-requires-approval",
            "prompt": "Refund order 1234 in full.",
            "tools": [
                {
                    "name": "confirm_refund",
                    "description": "Ask the user to approve a refund",
                    "parameters": { "type": "object", "properties": { "orderId": { "type": "string" } } }
                }
            ],
            "state": { "customer": "c-42" },
            "context": [{ "description": "locale", "value": "en-GB" }],
            "forwarded_props": { "channel": "web" },
            "responses": [
                { "tool": "^confirm_refund$", "args": "\"orderId\":\\s*\"1234\"", "result": { "approved": true } },
                {
                    "interrupt": "confirmation",
                    "message": "(?i)refund",
                    "status": "resolved",
                    "payload": { "approved": true }
                }
            ],
            "followups": ["Thanks, email me the receipt."],
            "assertions": [
                { "type": "outcome", "expect": "completed" },
                { "type": "hitl", "tool": "^confirm_refund$" },
                { "type": "tool_sequence", "tools": ["^confirm_refund$", "^issue_refund$"] },
                { "type": "tool_call", "tool": "^issue_refund$", "max": 1, "result": "\"status\":\\s*\"ok\"" },
                { "type": "state", "pointer": "/refunds/0/orderId", "equals": "1234" },
                { "type": "conformance" },
                "Confirms the amount and that a receipt was emailed"
            ]
        }
    ]
}
```

### The `agent` block

| Field       | Required | Meaning                                                                                                |
| ----------- | -------- | ------------------------------------------------------------------------------------------------------ |
| `name`      | no       | The agent's name. In the `agents` layout it must equal the directory name.                             |
| `protocol`  | no       | The protocol the agent speaks. Defaults to `ag-ui`, the only one supported today.                      |
| `endpoints` | yes      | Map of kebab-case endpoint names to where the agent is served. At least one; `all` is a reserved word. |

Each endpoint:

| Field             | Required | Meaning                                                                                        |
| ----------------- | -------- | ---------------------------------------------------------------------------------------------- |
| `url`             | yes      | Absolute `http(s)` URL the runs are POSTed to.                                                 |
| `display`         | no       | Display name in reports (default: the endpoint name).                                          |
| `headers`         | no       | Request headers — typically auth.                                                              |
| `forwarded_props` | no       | AG-UI `forwardedProps` every run to this endpoint carries; an eval's own merge over it by key. |

**Secrets stay out of the file.** `${VAR}` in a `url` or header value is expanded from the environment when a sweep
starts, and only for the endpoints that sweep selects — so `report`, `run checks`, and a `--endpoint local` run never
need the staging token. An unset variable is a configuration error (exit 2) naming the agent, the endpoint, and the
variable. Headers are never written to results, logs, or the workspace artifacts; the expanded values are scrubbed from
any error message evolve records; and a redirect is followed only within the endpoint's own origin, so credentials are
never forwarded to another host.

Endpoint names work like **environments**: two agents that both define `local` share the `local` selection, and
`--endpoint local` runs every agent's local endpoint.

### Per-eval fields

An agent eval has the usual `id`, `name`, `prompt`, `expected_output`, `files`, `expectations`, `assertions`,
`max_turns`, and `timeout_seconds` (see [Behavioral evals](evals.md)), plus the conversation script:

| Field             | Protocol | Meaning                                                                                                        |
| ----------------- | -------- | -------------------------------------------------------------------------------------------------------------- |
| `responses`       | any      | Pre-canned answers to the agent's HIL requests (below).                                                        |
| `followups`       | any      | User turns sent, in order, each time a run ends with nothing pending.                                          |
| `tools`           | ag-ui    | Frontend tools offered to the agent: `{name, description, parameters?}`. Calls to them are answered by evolve. |
| `state`           | ag-ui    | Initial shared state (default `{}`).                                                                           |
| `context`         | ag-ui    | Application context sent with every run: `[{description, value}]`.                                             |
| `forwarded_props` | ag-ui    | `forwardedProps`, merged over the endpoint's.                                                                  |

None of these are allowed in a skill suite. Two run controls change meaning for an agent: `max_turns` caps the number of
**runs** (request/response exchanges) per conversation, and `timeout_seconds` bounds the **whole conversation**. `files`
still stage into the local workspace — for `command` assertions and the judge — and are **not** sent to the agent.

### Answering human-in-the-loop requests

An AG-UI agent asks for input in two ways, and `responses` answer both. Responses match by regex in **any order** —
robust to an agent that asks in a different order from run to run — and each is used once unless it sets `repeat: true`.

**Frontend tool calls.** When a run ends with tool calls to frontend tools left unanswered, each must match a tool
response; evolve then appends one `tool` message per call and runs again.

| Field    | Meaning                                                                                       |
| -------- | --------------------------------------------------------------------------------------------- |
| `tool`   | Regex on the tool name.                                                                       |
| `args`   | Optional regex on the call's JSON arguments.                                                  |
| `result` | The answer. A string is sent verbatim; any other JSON value is serialized compactly.          |
| `error`  | Answer with this error instead of a result (exactly one of `result` and `error` is required). |
| `repeat` | Reuse the response for every matching call.                                                   |

The pending calls are the run outcome's `pendingToolCallIds` when the agent sends them, and otherwise the calls this run
started to declared `tools` that got no `TOOL_CALL_RESULT`.

**Interrupts.** When a run finishes with the `interrupt` outcome, every interrupt must match an interrupt response; the
next run then carries a `resume` entry for each.

| Field       | Meaning                                                             |
| ----------- | ------------------------------------------------------------------- |
| `interrupt` | Regex on the interrupt's `reason` (`confirmation`, `tool_call`, …). |
| `message`   | Optional regex on the interrupt's message.                          |
| `tool`      | Optional regex on the name of the interrupt's linked tool call.     |
| `status`    | `resolved` (default) or `cancelled`.                                |
| `payload`   | The resume payload — any JSON value but `null`.                     |
| `repeat`    | Reuse the response for every matching interrupt.                    |

If any pending call or interrupt has no matching response, evolve sends **nothing** — a partial resume is forbidden by
the spec — and the conversation ends **`awaiting_input`**. `run checks` warns about a tool response whose pattern
matches none of the eval's declared tools.

LangGraph's legacy `on_interrupt` custom event is not supported: configure the integration to emit the 1.0 interrupt
outcome.

## How a conversation runs

Every run POSTs an AG-UI `RunAgentInput` with `Accept: text/event-stream`. It carries a `threadId` fixed for the eval, a
new `runId`, `protocolVersion: "1.0"`, the current `state`, the `tools` and `context`, the merged `forwardedProps`, and
the accumulated `messages` — rebuilt from the streamed text, tool-call, and tool-result events, or replaced outright by
a `MESSAGES_SNAPSHOT`. Activity messages are stripped. A response may replay earlier runs of the thread before the
requested one; their events are validated but not folded into the conversation.

After each run:

| The run ended with                    | evolve                                                                      |
| ------------------------------------- | --------------------------------------------------------------------------- |
| An interrupt outcome                  | resumes every interrupt from `responses`, or stops as **`awaiting_input`**. |
| Pending frontend tool calls           | answers each from `responses` and runs again, or stops as `awaiting_input`. |
| The cancelled outcome                 | stops as **`cancelled`**.                                                   |
| Nothing pending                       | sends the next `followups` entry, or stops as **`completed`**.              |
| The run cap reached with work to send | stops as **`max_turns`**.                                                   |

These **runtime errors** end the conversation unrated — the eval records a `runtime_error`, like a CLI that crashed: a
transport error, a non-2xx response (status plus a body tail), a response that is not SSE, malformed or schema-invalid
event JSON, a stream that does not begin with `RUN_STARTED`, a truncated stream, `RUN_ERROR`, and the conversation
timeout. A truncated run is never success.

The graded output — what `regex` and the judge read as the agent's response — is the assistant text of every run, joined
by blank lines.

### What the workspace holds

Each eval still gets a throwaway workspace (with its `files` staged), and the conversation is written into it under
`.agent/`:

| File              | Contents                                                                                                 |
| ----------------- | -------------------------------------------------------------------------------------------------------- |
| `events.jsonl`    | Every event received, tagged with its run — also the run log the TUI opens with `[l]`.                   |
| `transcript.json` | Runs, outcome, usage, messages, tool calls with arguments and results, HIL exchanges, state, violations. |
| `transcript.md`   | A readable rendering of the conversation, for the judge (and you).                                       |
| `state.json`      | The final shared state.                                                                                  |

So existing assertions keep working against a hosted agent: `regex` checks the output; `file_*`, `command`, and `regex`
with a `path` work on the transcript (`jq -e '.refunds | length == 1' .agent/state.json`); and `llm` uses the judge. The
judge is unchanged — the configured CLI judge runs in the workspace, told that the agent ran remotely and where the
transcript is — so `llm` assertions need the judge's CLI installed, while deterministic-only suites need nothing but the
endpoint.

## Agent-only assertions

Four assertion types grade the conversation itself and are only valid in agent suites; `tool_call` and `tool_sequence`
work for agents and CLI harnesses alike. See [Assertions](assertions.md) for each.

| Type          | Passes when                                                                                     |
| ------------- | ----------------------------------------------------------------------------------------------- |
| `outcome`     | the conversation ended as `expect` (`completed`, `awaiting_input`, `cancelled`, `max_turns`).   |
| `hitl`        | the number of HIL requests matching `tool`/`reason`/`pattern` is within `min`/`max`.            |
| `state`       | the final shared state at an RFC 6901 `pointer` exists, `equals` a value, or matches `pattern`. |
| `conformance` | the agent's event stream has no error-severity violations of AG-UI 1.0.                         |

The `conformance` check covers the run lifecycle (the first event, `runId`/`threadId` agreement, nothing illegal after a
terminal event, no nested `RUN_STARTED`, truncation), open/close pairing of messages, tool calls, reasoning, and steps
(agreeing reopens allowed), the chunk-expansion rules, the outcome invariants (interrupts non-empty with unique ids;
`pendingToolCallIds` equal to the calls actually left unanswered), and `null` sent for an optional field. Unknown events
or fields, retired `THINKING_*` events, and a `STATE_DELTA` that fails to apply are recorded as warnings and never fail
it.

## Running

In an agent repository the selections are the endpoints:

```shell
evolve run checks
evolve run evals                      # every agent, every endpoint it defines
evolve run evals --endpoint local     # only the local endpoints
evolve run all --endpoint staging --keep-workspaces
```

- **`--endpoint name[,name]`** narrows the run to those endpoints. It is rejected in a plugin repository, and `--model`
  / `--harness` are rejected in an agent repository; the root `models` and `harnesses` config keys don't apply.
- **One selection per distinct `<protocol>/<endpoint>`**, keyed in results as `ag-ui/<endpoint>`. Each agent runs only
  on the endpoints its own suite defines.
- There is **no trigger tier** and **no without-skill baseline** for agents, and no token estimate (the agent's prompt
  is not ours to count). `--count-only` has nothing to do.
- `--new`, `--failed`, and `--modified` work as for skills. `--modified` reruns an agent's evals when anything in its
  directory changes — `agent.py`, `SYSTEM_PROMPT.md`, and any other file — except the evals file, the results file, and
  the fixtures the suite stages (a fixture change reruns only the evals that stage it).
- In the TUI the models pane lists **Endpoints** and the harness pane **Protocols**.
- `evolve doctor` lists every agent's endpoints — the URL host and whether each `${VAR}` is set — without calling them.
- Remote (`--remote`) execution does not support agent repositories yet.

## Results

Results land in the agent's directory (`agents/<name>/results.<ext>`), keyed `ag-ui/<endpoint>`. The header records
provider and harness `ag-ui`, the endpoint as the model, its `display`, the agent's content hash, and `pricing: null`.
Each eval result gains a `conversation` summary — runs, outcome, the HIL exchanges, and the conformance violations — see
[Results](results.md).

**Usage** is what the agent reports in `RUN_FINISHED.usage`, summed across runs. AG-UI's `inputTokens` includes cache
reads and writes, so evolve records the remainder as fresh input and the cache figures separately. A cost appears only
when every usage entry names a priced model from the registry.
