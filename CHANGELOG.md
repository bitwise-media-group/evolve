# Changelog

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
