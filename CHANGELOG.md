# Changelog

## [0.3.0](https://github.com/bitwise-media-group/evolve/compare/v0.2.1...v0.3.0) (2026-06-23)


### ⚠ BREAKING CHANGES

* **provider:** the `default_models` config key is renamed to `models`, internal/provider is removed, and committed results previously keyed under the copilot/cursor/antigravity providers are re-keyed to their canonical vendor model (anthropic/*, openai/*, google/*).
* results schema is now v3. input_tokens holds fresh (uncached) input only; cache reads/writes moved to cache_read_input_tokens and cache_creation_input_tokens. Results files written by older versions are reset on load.
* **provider:** the builtin Cursor model ids `composer-1` and `sonnet-4.5` are removed; select `cursor/composer-2.5` instead.

### Features

* add --modified flag to rerun cases whose content changed ([8fda8f4](https://github.com/bitwise-media-group/evolve/commit/8fda8f49637442d5fa0cc8c289e0d080ccd1827b))
* **cli:** add hidden --profile flag for cpu/memory pprof ([e1d00b6](https://github.com/bitwise-media-group/evolve/commit/e1d00b60df852e4754ed3164d5ddb78387561a23))
* **cli:** wire plain-output run commands with selection and sandbox flags ([5da5c7c](https://github.com/bitwise-media-group/evolve/commit/5da5c7c742d6ce664b11c66bc1b1349237560970))
* **config:** generate .evolve config JSON Schema from viper keys ([da8e085](https://github.com/bitwise-media-group/evolve/commit/da8e085497cd17c4ce9417fe9b81757fbd8228df))
* **docs:** publish a zensical documentation site ([50671ce](https://github.com/bitwise-media-group/evolve/commit/50671ce707ade2c2a3453feb63a913b8e812a449))
* honest eval token reporting and default_models-scoped results ([f500d83](https://github.com/bitwise-media-group/evolve/commit/f500d83d4fabbd0449d1c565bb4b40a219760097))
* **provider:** add Antigravity (agy) CLI provider ([3d7782b](https://github.com/bitwise-media-group/evolve/commit/3d7782b5467651e1f396bd0479eb258884c96e09))
* **provider:** add GitHub Copilot CLI provider ([5c4cbb7](https://github.com/bitwise-media-group/evolve/commit/5c4cbb7b3752bb7ee9c183bc95861754adefd724))
* raise default max_turns from 10 to 20 ([1162049](https://github.com/bitwise-media-group/evolve/commit/1162049416f1903be35aa9835bf6f62d470d6940))
* **results:** record eval assertion counts and auto-upgrade older schemas ([b18a336](https://github.com/bitwise-media-group/evolve/commit/b18a33645c78dc53a02749a3e220158ddcf40ea7))
* **run:** add --failed flag to select previously-failing triggers/evals ([f356f52](https://github.com/bitwise-media-group/evolve/commit/f356f52ee9fbb4e0babd977fd2c66dd72b84b4ff))
* **run:** add --plugin filter and multi-value --skill/--model ([f81fd3f](https://github.com/bitwise-media-group/evolve/commit/f81fd3f74938c8a771cdf8c45b70bcc953562d54))
* **run:** add interleaved per-skill sweep ([5f635e5](https://github.com/bitwise-media-group/evolve/commit/5f635e5c70c4b7bc91c224dc579f2763b6929002))
* **run:** add Reporter observer and Catalog/Plan/Filter selection seam ([6595070](https://github.com/bitwise-media-group/evolve/commit/65950709cc5044d8b9bd7ca72e74db7b88d2c30e))
* **run:** baseline benchmarks, run history, and regression/improvement deltas ([4933dfb](https://github.com/bitwise-media-group/evolve/commit/4933dfb66218b350d1d1f5faac55b0117ddc59f3))
* **runner:** sandbox agent runs to protect source repositories ([ee97c6d](https://github.com/bitwise-media-group/evolve/commit/ee97c6d78d8cd25056a1a1e0f3858c8a962e7e32))
* **run:** per-case trigger/eval selection with preselection reasons ([8468d80](https://github.com/bitwise-media-group/evolve/commit/8468d8031738156e00c93e5ec7c269cceb1a2309))
* **run:** retain run-scoped workspaces and surface output/log paths ([6167995](https://github.com/bitwise-media-group/evolve/commit/61679956928848005ec4e2aa33160ebe60fc492a))
* **telemetry:** OpenTelemetry instrumentation and --telemetry-dir flag ([cecd01e](https://github.com/bitwise-media-group/evolve/commit/cecd01ed600dcad9617faaf215493a66e3d99505))
* **tui:** drive the selection form from a stateful plan.Session ([ba8ee34](https://github.com/bitwise-media-group/evolve/commit/ba8ee34addff2d791b9cf51281900613dd9d2fa8))
* **tui:** interactive selection form and live run dashboard ([c4c4ce8](https://github.com/bitwise-media-group/evolve/commit/c4c4ce89ab0f03cce53000408fb9f6bc0241dcb5))
* **tui:** reclaim selection-form space and mirror dashboard styling ([ad6c545](https://github.com/bitwise-media-group/evolve/commit/ad6c545437629765234d458bc0b53104eb01d6f2))
* **tui:** render the EVOLVE wordmark in per-letter colour ([46e4e08](https://github.com/bitwise-media-group/evolve/commit/46e4e08c2221083723624796d9d335db08f62bb4))
* **tui:** rework the run dashboard with a navigable execution tree ([ad2d01b](https://github.com/bitwise-media-group/evolve/commit/ad2d01b17eee7498b853cd9f6bd21ccb1e8aee76))
* **tui:** seed queued rows from prior results and update them live ([4024d6c](https://github.com/bitwise-media-group/evolve/commit/4024d6c3efd6addb7887c102c262890754f3e554))
* **tui:** show prior results for cases a partial run does not re-run ([78e19a8](https://github.com/bitwise-media-group/evolve/commit/78e19a8813b21930d810ba7eb656ddd0d8f4342b))
* **tui:** tint pane headings with each pane's accent color ([fbc3e49](https://github.com/bitwise-media-group/evolve/commit/fbc3e499c1f43bad6db1c7f3e7a8548bb006a534))
* **tui:** upgrade to bubbletea v2 and recolor to cyberdream ([82f9863](https://github.com/bitwise-media-group/evolve/commit/82f986377e20a26bb94f7fbdad42131745d6bc75))


### Bug Fixes

* **make:** build golangci-lint before linting ([276d7fa](https://github.com/bitwise-media-group/evolve/commit/276d7fafa16ac883ad8fb3a2084da8d2917f1f65))
* **plan:** restrict both tiers when a skill is queued via one ([126fa16](https://github.com/bitwise-media-group/evolve/commit/126fa16771f1e7fb3b983d46fab78f84286edabe))
* **provider:** disable agent CLIs' own OS sandbox under evolve's ([360849d](https://github.com/bitwise-media-group/evolve/commit/360849d26acff2595cc91f2b0b86d60d1b09697f))
* **provider:** drop the antigravity Claude Sonnet 4.6 model ([50b6c06](https://github.com/bitwise-media-group/evolve/commit/50b6c063aea09885769d397faa8e1df3b14a058d))
* **provider:** replace Cursor models with Composer 2.5 ([46ab5e2](https://github.com/bitwise-media-group/evolve/commit/46ab5e29c0b1fa60fda693e52c562605cdaf218c))
* **provider:** split codex cached input tokens off the headline input figure ([8622320](https://github.com/bitwise-media-group/evolve/commit/8622320808ee2e078d090490f51bfe8000ea62aa))
* **run:** don't pre-select unfillable units under --new ([62ef48b](https://github.com/bitwise-media-group/evolve/commit/62ef48ba37e08ae5651781e7da5dc738ef49f124))
* **run:** serialize PlainReporter writes for concurrent use ([2fff1a8](https://github.com/bitwise-media-group/evolve/commit/2fff1a83a4f2e7ec1492cc3a38a3db3576c96b9b))
* **run:** surface claude stdout errors on the runtime-error path ([912a0a0](https://github.com/bitwise-media-group/evolve/commit/912a0a0d0ce6bef19b1acd12a104efc4a653d2fb))
* strip ANSI escape sequences from captured execution output ([5ae2cff](https://github.com/bitwise-media-group/evolve/commit/5ae2cff0a655bc9f07349f2d9d3b76adff1cde3e))
* **tui:** spin only running rows; mark queued rows pending ([67d84ee](https://github.com/bitwise-media-group/evolve/commit/67d84ee8a0b797b35c96ad6763a8077dc865ba98))


### Performance Improvements

* **tui:** render only on-screen dashboard rows ([8743990](https://github.com/bitwise-media-group/evolve/commit/8743990a860ad46a9ff710538821a9091702d98e))


### Code Refactoring

* **provider:** split provider into harness, provider, and model ([aadbfaf](https://github.com/bitwise-media-group/evolve/commit/aadbfaf6009622ba05c2cccd430f637f9f31d5e8))

## [0.2.1](https://github.com/bitwise-media-group/evolve/compare/v0.2.0...v0.2.1) (2026-06-17)


### Features

* **evals:** surface runtime failures and isolate token-counting credentials ([e08452b](https://github.com/bitwise-media-group/evolve/commit/e08452b34d54aea123a640c4c201585310743385)), closes [#9](https://github.com/bitwise-media-group/evolve/issues/9)
* **provider:** accept EVOLVE_CLAUDE_CODE_OAUTH_TOKEN for token counting ([1003ac9](https://github.com/bitwise-media-group/evolve/commit/1003ac99adc0e359e867acf200672a9e5f3759ac)), closes [#9](https://github.com/bitwise-media-group/evolve/issues/9)

## [0.2.0](https://github.com/bitwise-media-group/evolve/compare/v0.1.0...v0.2.0) (2026-06-13)


### ⚠ BREAKING CHANGES

* **evalspec:** bare-array eval files and inline files content (name -> content maps) are no longer accepted; use the envelope and on-disk fixtures.
* **run:** `evolve run cases`, --case, --min-cases-pass-rate, the cases_min_pass_rate config key, and the cases.json file name are gone; use the evals spellings.
* **encfmt:** .evolve.toml is no longer a recognized config file; use yaml, yml, json, or jsonc.
* **results:** results files bump to schema 2; stored schema 1 files are reinitialized on the next run.
* **cmd:** `evolve run check` is now `evolve run checks`.

### Features

* **encfmt:** JSONC and YAML for eval, results, and config files ([6ed3dd7](https://github.com/bitwise-media-group/evolve/commit/6ed3dd736f2ae27c9ba995489f6ba90829749e93))
* **evalspec:** adopt the skill-creator eval superset ([#4](https://github.com/bitwise-media-group/evolve/issues/4)) ([6ed3dd7](https://github.com/bitwise-media-group/evolve/commit/6ed3dd736f2ae27c9ba995489f6ba90829749e93))
* **results:** schema v2, a superset of skill-creator grading.json ([6ed3dd7](https://github.com/bitwise-media-group/evolve/commit/6ed3dd736f2ae27c9ba995489f6ba90829749e93))
* **schemas:** publish JSON Schemas with conformance tests ([6ed3dd7](https://github.com/bitwise-media-group/evolve/commit/6ed3dd736f2ae27c9ba995489f6ba90829749e93))


### Code Refactoring

* **cmd:** rename `run check` to `run checks` ([6ed3dd7](https://github.com/bitwise-media-group/evolve/commit/6ed3dd736f2ae27c9ba995489f6ba90829749e93))
* **run:** rename behavioral "cases" to "evals" ([6ed3dd7](https://github.com/bitwise-media-group/evolve/commit/6ed3dd736f2ae27c9ba995489f6ba90829749e93))

## 0.1.0 (2026-06-12)


### ⚠ BREAKING CHANGES

* **checks:** `evolve run check` previously required `license: MIT` in every SKILL.md. Repositories relying on that default must now set checks.license: MIT (or pass --license MIT); repositories whose skills declare a license without configuring one will start failing.

### Features

* add evolve, a CLI that evaluates coding-agent plugins ([ae3c896](https://github.com/bitwise-media-group/evolve/commit/ae3c896af84fdc6f4a6c85c99c494aacca549cfe))
* **checks:** make the SKILL.md license rule opt-in ([29c7d13](https://github.com/bitwise-media-group/evolve/commit/29c7d130e6ed286e80f9f44c4bc56969f9be0b76))
* **release:** group release notes by kind with author credit ([55bb90d](https://github.com/bitwise-media-group/evolve/commit/55bb90d19676eac710adfc27a5599f2739bb6e08))
* **release:** windows builds, cosign signing, homebrew cask, attestations ([5b1ab4b](https://github.com/bitwise-media-group/evolve/commit/5b1ab4bb33ce8b4d9926cf3f1e9b361205e8867b))
* **runner:** split process-tree kill into per-platform files ([fbe89a3](https://github.com/bitwise-media-group/evolve/commit/fbe89a3b7e8daf02c9b08c713b07cf6a69ab9217))
