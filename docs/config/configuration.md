# Configuration

evolve reads an optional config file named `.evolve.<ext>` from the repository root (`--root`),
where `<ext>` is one of `yaml`, `yml`, `json`, or `jsonc`. At most one config file may
exist — two formats side by side is an error. Settings layer lowest precedence first: built-in
defaults, the config file, `EVOLVE_*` environment variables, then explicit flags.

## Options

| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `layout` | string | `"auto"` | Repository layout: auto, marketplace, multi, or single. |
| `default_models` | list of strings | `["anthropic"]` | Model spec used when --models is omitted: provider names, model ids, provider-qualified ids (cursor/composer-2.5), or all. |
| `cache_dir` | string | unset — the OS user cache dir | Directory holding the token-count cache. |
| `results_format` | string | `"json"` | Format for committed results files and the EVALUATION rollup: json, jsonc, or yaml. |
| `max_turns` | int | `20` | Default maximum agent turns per behavioral eval; --max-turns and a per-eval max_turns override it. |
| `stale_results` | string | unset — prompt on a terminal, otherwise keep | How run/report treat stored results for models outside default_models: keep or drop. --stale-results overrides. |
| `sandbox.enabled` | bool | `true` | Confine agent writes with an OS sandbox (sandbox-exec on macOS, bubblewrap on Linux); --no-sandbox overrides for one run. |
| `sandbox.protected_roots` | list of strings | unset — the parent directory of the repository under test | Directories kept read-only to agent runs so an escaping agent cannot modify other source repositories; the workspace stays writable. Reads, the network, and tool caches outside these roots are unaffected. |
| `checks.license` | string | unset — the license field is forbidden | License every SKILL.md must declare; when unset, skills must not declare one. |
| `checks.description_pattern` | string | `"Use (when\|after\|before)"` | Regex every skill description must match. |
| `checks.max_skill_lines` | int | `500` | Maximum SKILL.md line count. |
| `checks.require_codex_manifest` | bool | `true` | Require .codex-plugin/plugin.json beside Claude's manifest. |
| `checks.forbid_hooks` | bool | `true` | Forbid a hooks/ directory in plugins. |
| `checks.marketplace` | bool | `true` | Validate marketplace manifests (marketplace layout only). |
| `report.thresholds.triggers_min_pass_rate` | float | unset — no gate | Minimum triggers pass rate (0-1); report --check exits 1 below it. |
| `report.thresholds.evals_min_pass_rate` | float | unset — no gate | Minimum evals pass rate (0-1); report --check exits 1 below it. |
| `report.thresholds.models` | list of strings | unset — every model with stored results | Model keys (provider/model-id) the thresholds apply to. |

## Provider overrides

`providers.<name>.models` replaces that provider's builtin model matrix; providers without an
entry keep their builtin models. Each list entry is an object:

| Field | Type | Description |
| --- | --- | --- |
| `id` | string | Model id passed to the runner CLI (required). |
| `display` | string | Human-readable name shown in reports (default: the id). |
| `input_per_mtok` | float | Input price in USD per million tokens (omit when unpublished). |
| `output_per_mtok` | float | Output price in USD per million tokens (omit when unpublished). |

## Annotated examples

Generated alongside this page, each with every default value set and a comment per option —
copy one to the repository root:

- [`.evolve.yaml`](.evolve.yaml)
- [`.evolve.jsonc`](.evolve.jsonc)

## Schema

[`config.schema.json`](config.schema.json) is the JSON Schema (draft 2020-12) for the config
file, generated from the same keys. The annotated examples above already point at its published
URL — a `# yaml-language-server: $schema=…` comment in the YAML, a `"$schema"` key in the JSONC —
so an editor offers completion and validation as soon as you copy one to the repository root.
