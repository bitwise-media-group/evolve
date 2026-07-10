# evolve — everything lives in mise tasks: the go-cli archetype (build/lint/
# test/release machinery + pinned tools) comes from the shared toolchain
# submodule at .mise/, selected in the root mise.toml; tasks.toml carries the
# repo-local tasks (ui, bench, smoke, docs, run) and the redefined gates (the
# GOOS lint matrix, the e2e module, fuzz defaults, evolve's ci/pr sequences).
# This Makefile is only the thin forwarding shim — `make <task>` == `mise run <task>`.
include .mise/mise.mk
