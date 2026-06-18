# evolve

A single CLI that evaluates coding-agent plugins: static checks, trigger-accuracy evals, behavioral evals, and
Markdown/JSON reports. It is a Go rewrite of the Python harness that lived in the skills marketplace repo, installable
once and runnable against any plugin repository. The generated command reference lives in
[docs/cli](docs/cli/evolve.md).

## Install

### Homebrew (macOS)

```sh
# homebrew 6 requires end-users to trust formulae / casks in a tap
brew trust --cask bitwise-media-group/tap/evolve
# OR to trust all future formulae / casks in our tap
brew trust --tap bitwise-media-group/tap
# install the cask from the tap
brew install --cask bitwise-media-group/tap/evolve
```

The cask lives in [bitwise-media-group/homebrew-tap](https://github.com/bitwise-media-group/homebrew-tap), is updated by
every release, and installs shell completions alongside the prebuilt binary.

### Release archives

Every [release](https://github.com/bitwise-media-group/evolve/releases) ships `evolve_<version>_<os>_<arch>` archives
(`.tar.gz`, or `.zip` on Windows) for Linux, macOS, and Windows on amd64 and arm64, plus `checksums.txt`, a Sigstore
bundle per binary, and an SPDX SBOM per archive. Download, extract, and put `evolve` somewhere on your `PATH`:

```sh
version=0.1.0
curl -fsSLO "https://github.com/bitwise-media-group/evolve/releases/download/v${version}/evolve_${version}_darwin_arm64.tar.gz"
tar -xzf "evolve_${version}_darwin_arm64.tar.gz" evolve
```

To verify what you downloaded: `checksums.txt` covers the archives, a GitHub
[SLSA build-provenance attestation](https://github.com/bitwise-media-group/evolve/attestations) covers every file listed
in `checksums.txt`, and the Sigstore bundles sign the extracted binaries (keyless, with the release workflow's GitHub
OIDC identity):

```sh
# verify checksum
curl -fsSLO "https://github.com/bitwise-media-group/evolve/releases/download/v${version}/checksums.txt"
shasum -a 256 --check --ignore-missing checksums.txt

# verify provenance and signature of the artifact with github
gh attestation verify "evolve_${version}_darwin_arm64.tar.gz" --repo bitwise-media-group/evolve

# verify provenance and signature of the binary with cosign
curl -fsSLO "https://github.com/bitwise-media-group/evolve/releases/download/v${version}/evolve_darwin_arm64.sigstore.json"
cosign verify-blob \
  --bundle evolve_darwin_arm64.sigstore.json \
  --certificate-identity https://github.com/bitwise-media-group/evolve/.github/workflows/release.yaml@refs/heads/main \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  evolve
```

### Go

To pin evolve as a tool dependency of a Go module (tracked in its `go.mod`, run via `go tool`):

```sh
go get -tool github.com/bitwise-media-group/evolve/cmd/evolve@latest
go tool evolve --help
```

Or to install it user-wide into `$(go env GOBIN)`:

```sh
go install github.com/bitwise-media-group/evolve/cmd/evolve@latest
```

Both build from source with your Go toolchain; the resulting binary reports its version as `dev` rather than a release
version, and shell completions are not installed.

## Commands

```text
evolve run checks     Tier 0 — skill frontmatter, plugin manifests, marketplace consistency, eval definitions
evolve run triggers   Tier 1 — does the right skill activate on the right prompts? (headless agent sessions)
evolve run evals      Tier 2 — full coding tasks graded by expectations and assertions (files, regexes, commands, LLM judge)
evolve run all        check → triggers → evals → report
evolve report         Regenerate EVALUATION.md + the EVALUATION rollup from stored results (--check gates on thresholds)
evolve doctor         Per provider: runner CLI on PATH? credential set? counting API reachable?
evolve models         The effective provider/model matrix with pricing, capabilities, and provenance
```

Global flags: `--root PATH` (default: walk up from the working directory), `--layout auto|marketplace|multi|single`,
`-v`. Exit codes: 0 ok, 1 check/eval/threshold failures, 2 usage or configuration errors.

## Repository layouts

The tool auto-detects three shapes (override with `--layout`):

| Layout        | Marker                                 | Eval definitions + results   | Skills                        |
| ------------- | -------------------------------------- | ---------------------------- | ----------------------------- |
| `marketplace` | `.claude-plugin/marketplace.json`      | `plugins/<p>/evals/<skill>/` | `plugins/<p>/skills/<skill>/` |
| `multi`       | `plugins/*/.claude-plugin/plugin.json` | `plugins/<p>/evals/<skill>/` | `plugins/<p>/skills/<skill>/` |
| `single`      | `.claude-plugin/plugin.json` at root   | `evals/<skill>/`             | `skills/<skill>/`             |

A `multi` repo is a marketplace repo without marketplace manifests — marketplace checks are skipped. In a `single` repo
the repository root is the plugin and the plugin name comes from its manifest.

Each `evals/<skill>/` directory holds the authored `triggers.<ext>` and `evals.<ext>` definitions — `json`, `jsonc`, or
`yaml` (`.yml` accepted), exactly one extension per file — plus the committed `results.<ext>` the sweeps write. Both
definition files use skill-creator's envelope shape (`{"skill_name": ..., "evals": [...]}`), and the eval object is a
strict superset of skill-creator's, so a skill-creator `evals.json` drops in unchanged. JSON Schemas for every authored
and emitted file live in [schemas/](schemas/); point editors at the raw URLs via a `"$schema"` key for validation and
completion.

## Providers

| Provider    | Runner CLI             | Credential                                              | Triggers | Evals | Token counting |
| ----------- | ---------------------- | ------------------------------------------------------- | -------- | ----- | -------------- |
| anthropic   | `claude`               | `ANTHROPIC_API_KEY` (or OAuth token vars)               | yes      | yes   | yes            |
| openai      | `codex`                | `OPENAI_API_KEY`                                        | yes      | yes   | yes            |
| google      | `gemini`               | `GEMINI_API_KEY` / `GOOGLE_API_KEY`                     | yes      | no    | yes            |
| cursor      | `agent` (cursor-agent) | `CURSOR_API_KEY`                                        | yes      | yes   | no             |
| copilot     | `copilot`              | `COPILOT_GITHUB_TOKEN` (or `GH_TOKEN` / `GITHUB_TOKEN`) | yes      | yes   | no             |
| antigravity | `agy`                  | OAuth login via `agy` (no API-key env var)              | yes      | yes   | no             |

Cursor, Copilot, and Antigravity expose no token-counting API and their CLIs report no usage or cost, so their
estimate/measured figures render as `n/a` in reports — structurally absent, not zero. They run other vendors' models
behind config-driven ids: pin them via `providers.cursor.models` / `providers.copilot.models` /
`providers.antigravity.models` (the builtin defaults are conservative; `agent models` and `agy models` print the live
lists for Cursor and Antigravity). Copilot and Antigravity emit no structured output, so their trigger detection is
best-effort path matching on the CLI's stdout. The LLM judge for `llm` assertions always runs through `claude` (pinned
via `--judge-model`) so grading stays comparable across providers.

Select models with `--models`: provider names (`anthropic`), model ids (`claude-fable-5`), provider-qualified ids
(`cursor/sonnet-4.5`), or `all` — comma-separated.

## Configuration: `.evolve.<ext>`

Optional, at the repo root, as `.evolve.yaml`, `.yml`, `.json`, or `.jsonc` — at most one. Flags > `EVOLVE_*` env >
config > builtins. The full option reference and annotated example files for each format live in
[docs/config/configuration.md](docs/config/configuration.md) (regenerated by `make docs`).

```json
{
  "layout": "marketplace",
  "default_models": ["anthropic", "cursor/sonnet-4.5"],
  "results_format": "json",
  "checks": {
    "license": "MIT",
    "description_pattern": "Use (when|after|before)",
    "max_skill_lines": 500,
    "require_codex_manifest": true,
    "forbid_hooks": true,
    "marketplace": true
  },
  "report": {
    "thresholds": {
      "triggers_min_pass_rate": 0.8,
      "evals_min_pass_rate": 0.9,
      "models": ["anthropic/claude-fable-5"]
    }
  },
  "providers": {
    "cursor": {
      "models": [{ "id": "sonnet-4.5", "display": "Cursor — Sonnet 4.5" }]
    }
  }
}
```

`providers.<name>.models` replaces that provider's builtin matrix (ids, display names, `input_per_mtok` /
`output_per_mtok` pricing). `checks.*` exposes every rule the static checks apply, so other organizations can use the
tool without forking. The same structure applies in every format; JSONC additionally tolerates comments and trailing
commas.

## Results: `evals/<skill>/results.<ext>`

One committed file per skill (format chosen by `results_format` / `--results-format`: `json`, `jsonc`, or `yaml`) holds
both eval kinds, keyed by `provider/model-id` (provider-qualified because Cursor runs other vendors' models). A triggers
sweep rewrites only the run models' entries under `triggers`; evals likewise — diffs stay scoped to what actually ran.
Highlights:

- Each per-eval result is a superset of skill-creator's `grading.json`: an `expectations` array with
  `text`/`passed`/`evidence` per entry (deterministic checks get a derived statement; the authored assertion is echoed
  alongside), a `summary` with `passed`/`failed`/`total`/`pass_rate`, and a `timing` block. Tooling written against
  skill-creator output reads evolve results unchanged.
- `hits`/`runs` are exact integers; rates and the 0.5 pass threshold are derived at render time.
- `estimate` (counting-API tokens for SKILL.md + prompt, priced at input rate) and `measured` (live-session usage) are
  omitted — not nulled — where a provider cannot produce them.
- `pricing` is snapshotted per entry (`null` when unpublished) so old reports re-render identically as the live matrix
  moves. Absent and `null` mean the same thing everywhere.
- Output is deterministic: sorted keys, fixed field order, rounded floats, trailing newline. Switching `results_format`
  preserves history and removes the stale sibling.

`--new` reruns only entries with missing values a rerun could actually fill; interrupted sweeps keep every model that
finished, so Ctrl-C + `--new` is the resume story.

## Migrating from skill-creator

Evolve's eval schemas are a superset of the ones in
[anthropics/skills skill-creator](https://github.com/anthropics/skills/blob/main/skills/skill-creator/references/schemas.md),
verified by an executable conformance test against its verbatim examples. To migrate a skill:

1. Copy the skill directory into `skills/<skill>/` (or `plugins/<p>/skills/<skill>/`).
2. Move its `evals/` content to `evals/<skill>/`: `evals/evals.json` becomes `evals/<skill>/evals.json` verbatim —
   integer ids, `files` paths (the leading `evals/` segment still resolves), `expectations`, and `expected_output` all
   load unchanged. Fixture files under `evals/files/` stage into the agent workspace by basename; other `files/` paths
   keep their layout relative to `files/`.
3. Run `evolve run checks` to validate, then `evolve run evals`.

`expectations` grade through the LLM judge (before any `assertions`, in authored order) and `expected_output` is passed
to the judge as author context — never auto-asserted, so migrated suites keep their pass rates. Evolve's extras
(deterministic `assertions`, `max_turns`, `timeout_seconds`, `allowed_tools`, `skip_providers`, token counts, costs) are
additive. `schemas/benchmark.schema.json` and `schemas/history.schema.json` freeze superset contracts for the
benchmark/improve artifacts ahead of the evolve modes that will emit them.

### Breaking changes from earlier evolve versions

- `cases.json` is now `evals.<ext>` with the envelope shape, fixture-path `files` (inline file content is gone — put
  fixtures on disk next to the eval file), and `evolve run evals` / `--eval` / `evals_min_pass_rate` replace their
  `cases` spellings.
- Results files with schema 1 are reinitialized on the next run (commit before upgrading); the section key is now
  `evals` and graded entries live under `expectations`.
- `.evolve.toml` is no longer read — convert the config to yaml/json/jsonc.

## Reports

`evolve report` writes a root `EVALUATION.md` (per-plugin rollups; in `single` layout also the per-query detail),
per-plugin `EVALUATION.md` pages in marketplace/multi layouts, and a machine-readable `EVALUATION.<ext>` rollup (same
`results_format` selection). Cells show `—` for not-measured-yet and `n/a` for can-never-measure. `report --check` exits
1 when configured pass-rate thresholds are breached — or when a threshold model has no stored results at all.

## Development

```sh
make help       # list targets
make pr         # full local gate: license, tidy, fmt, vet, test, fuzz, build, snapshot
make smoke      # live end-to-end `run all` on the marketplace fixture (claude CLI + credentials; not part of `pr`)
```

Dev CLIs (addlicense, goreleaser, syft) are pinned in `tools/go.mod` and run via `go tool -modfile`; Node lint tools
(prettier, markdownlint-cli2) are pinned in `package-lock.json`. Releases are tag-driven via GoReleaser
(`.github/workflows/release.yaml`).
