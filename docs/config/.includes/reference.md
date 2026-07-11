| Key | Type | Default | Description |
| --- | --- | --- | --- |
| `version` | string | unset — any evolve version may run | Terraform-style semver constraint ("0.4.0", "~> 0.4", ">= 0.4, < 1") the evolve binary must satisfy before run triggers/evals/all or report will rewrite results. Non-release builds (dev and snapshot/prerelease versions) warn and skip the check. |
| `layout` | string | `"auto"` | Repository layout: auto, marketplace, multi, or single. |
| `models` | list of strings | unset — every model runnable by an available harness | Restriction on which models exist: provider ids, canonical model ids (anthropic/claude-sonnet-4-6), or all. Unlisted models are unavailable. --model filters within it. |
| `harnesses` | list of strings | unset — every harness found on PATH | Restriction on which agent CLIs (claude, codex, gemini, cursor, copilot, antigravity, grok) may drive models. --harness filters within it. |
| `cache_dir` | string | unset — the OS user cache dir | Directory holding the token-count cache. |
| `results_format` | string | `"json"` | Format for committed results files and the EVALUATION rollup: json, jsonc, or yaml. |
| `telemetry.dir` | string | unset — telemetry disabled | Directory for the OpenTelemetry JSON exporter (traces.json, metrics.json, logs.json); the --telemetry-dir flag overrides it and both win over OTEL_* env vars. |
| `max_turns` | int | `20` | Default maximum agent turns per behavioral eval; --max-turns and a per-eval max_turns override it. |
| `baseline` | bool | `true` | Benchmark each eval without the skill (the skill's lift over no skill), recomputed only when the eval or its fixtures change. --baseline overrides for one run. |
| `stale_results` | string | unset — prompt on a terminal, otherwise keep | How run/report treat stored results for models outside the `models` restriction: keep or drop. --stale-results overrides. |
| `sandbox.enabled` | bool | `true` | Confine agent writes with an OS sandbox (sandbox-exec on macOS, bubblewrap on Linux); --no-sandbox overrides for one run. |
| `sandbox.protected_roots` | list of strings | unset — the parent directory of the repository under test | Directories kept read-only to agent runs so an escaping agent cannot modify other source repositories; the workspace stays writable. Reads, the network, and tool caches outside these roots are unaffected. |
| `checks.license` | string | unset — the license field is forbidden | License every SKILL.md must declare; when unset, skills must not declare one. |
| `checks.description_pattern` | string | `"Use (when\|after\|before)"` | Regex every skill description must match. |
| `checks.max_skill_lines` | int | `500` | Maximum SKILL.md line count. |
| `checks.ideal_skill_lines` | int | `200` | Ideal SKILL.md line count for the advisory size signal (full at or below; zero at the cap). |
| `checks.signals` | bool | `true` | Emit the advisory skill-quality signals after run checks; the --no-signals flag forces them off. |
| `checks.plugin_manifests` | list of strings | `["claude","codex"]` | Plugin manifests every plugin must ship: claude (.claude-plugin/plugin.json) and/or codex (.codex-plugin/plugin.json). With both, a hooks/ directory is forbidden (codex and claude hooks.json are incompatible). |
| `checks.marketplace` | bool | `true` | Validate marketplace manifests (marketplace layout only). |
| `report.thresholds.triggers_min_pass_rate` | float | `0.5` | Minimum triggers pass rate (0-1); report --check exits 1 below it and the run dashboard rolls a failing group to an orange check while its rate stays at or above it. |
| `report.thresholds.evals_min_pass_rate` | float | `0.66` | Minimum evals pass rate (0-1); report --check exits 1 below it and the run dashboard rolls a failing group to an orange check while its rate stays at or above it. |
| `report.thresholds.models` | list of strings | unset — every model with stored results | Model keys (provider/model-id) the thresholds apply to. |
| `report.thresholds.maturity` | list of strings | `["stable","unstable","prerelease"]` | Plugin maturity levels whose Tier 1/Tier 2 evidence issues fail report --check; other levels only warn (evidence still renders). A plugin's maturity comes from its manifest version: stable (>= 1.0.0), unstable (< 1.0.0), or prerelease (a SemVer prerelease tag). The --maturity flag overrides it. |
| `report.junit` | string | unset — no JUnit file written | Path for a JUnit XML test-results file (one testcase per eval/trigger case per model); the --junit flag overrides it. |
| `report.cobertura` | string | unset — no Cobertura file written | Path for a Cobertura XML coverage file marking each skill covered by a current eval result; the --cobertura flag overrides it. |
| `report.strict` | bool | `false` | Require the configured model matrix: report --check holds every defined model to the thresholds, and the Cobertura output covers a skill only when every defined model has a current result. The --strict flag overrides it. |
