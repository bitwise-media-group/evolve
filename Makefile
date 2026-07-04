# evolve — the common Go build/lint/test/release machinery lives in the shared
# Makefile library (bitwise-media-group/make), consumed as the `make/` submodule
# and included below. Only evolve's repo-specific knobs and long-tail targets
# live here; the canonical lint/build/test/ci/pr contract comes from go-cli.mk.
APP     := evolve
APP_PKG := ./cmd/evolve

# The report-viewer SPA is embedded into the binary via the withui build tag;
# the `ui` target below builds internal/web/ui/dist before go-build compiles.
BUILD_TAGS := withui

# `go test -fuzz` accepts a single package, so point fuzz at one target/package.
FUZZ_PKG := ./internal/manifest
FUZZ     := FuzzFrontmatter

# Gate extensions. The archetype's gates stop at tidy/fmt/lint/test/build(/commit);
# evolve's also run fuzz, bench, docs, snapshot, and smoke. Declared before the
# include so these prerequisites are made first, in this order — make runs
# prerequisites left-to-right, each at most once, so the archetype's repeats are
# skipped and `commit` still lands last in `pr`.
pr: tidy fmt lint test fuzz bench build docs snapshot smoke
ci: lint test fuzz build docs snapshot

include make/go-cli.mk

# ---- repo-local targets (the long tail the library intentionally omits) -----

# Platform build-tag matrix. The host GOOS only compiles its own files, so the
# library's single-GOOS go-lint/go-fmt skip every other platform's
# build-constrained source (the sandbox_*.go / proc_*.go split in
# internal/runner). Linting the remaining platforms in turn covers them all.
# golangci-lint is a host binary, so run it under each GOOS env var — a
# cross-built golangci-lint could not exec on the host. gofmt itself ignores
# build tags, so the host pass already reaches every file.
LINT_GOOS       ?= linux darwin windows
LINT_GOOS_EXTRA := $(filter-out $(shell go env GOOS),$(LINT_GOOS))

.PHONY: go-lint-goos go-fmt-goos
lint: go-lint-goos lint-e2e
go-lint-goos: $(GOLANGCI_LINT)
	@	ret=0; \
		for goos in $(LINT_GOOS_EXTRA); do \
			echo "lint: GOOS=$$goos"; \
			GOOS=$$goos "$(GOLANGCI_LINT)" run || { ret=1; break; }; \
		done; \
		exit $$ret

fmt: go-fmt-goos fmt-e2e
go-fmt-goos: $(GOLANGCI_LINT)
	@	ret=0; \
		for goos in $(LINT_GOOS_EXTRA); do \
			echo "fmt: GOOS=$$goos"; \
			GOOS=$$goos "$(GOLANGCI_LINT)" run --fix || { ret=1; break; }; \
		done; \
		exit $$ret

# e2e is its own module (the root ./... never picks it up), so lint/format/tidy
# it explicitly alongside the root module.
.PHONY: lint-e2e fmt-e2e tidy-e2e
lint-e2e: $(GOLANGCI_LINT)
	@ echo "lint: e2e"
	@ cd e2e && "$(GOLANGCI_LINT)" run
fmt-e2e: $(GOLANGCI_LINT)
	@ echo "fmt: e2e"
	@ cd e2e && "$(GOLANGCI_LINT)" run --fix
tidy: tidy-e2e
tidy-e2e:
	@ rm -f e2e/go.sum; go -C e2e mod tidy

# Report-viewer SPA: built into internal/web/ui/dist and embedded by go-build
# via -tags withui (BUILD_TAGS above). dist/ is git-ignored (a built bundle,
# like site/); the file target reruns npm only when the UI sources change.
UI_DIR  := internal/web/ui
UI_SRCS := $(UI_DIR)/package.json $(UI_DIR)/package-lock.json $(UI_DIR)/index.html \
	$(UI_DIR)/vite.config.ts $(UI_DIR)/tsconfig.json $(shell find $(UI_DIR)/src -type f 2>/dev/null)

$(UI_DIR)/dist/index.html: $(UI_SRCS)
	@ npm --prefix $(UI_DIR) ci --no-fund --no-audit
	@ npm --prefix $(UI_DIR) run build

.PHONY: ui
ui: $(UI_DIR)/dist/index.html ## build the embedded report-viewer SPA (internal/web/ui/dist)

# the withui build embeds ui/dist, so it must exist before go-build compiles
go-build: ui

# Benchmarks: `make bench` runs BENCH (a -bench regexp) over BENCH_PKG.
BENCH     ?= .
BENCH_PKG ?= ./...
.PHONY: bench
bench: ## run benchmarks (BENCH=. all, BENCH=DashboardView one; BENCH_PKG=./internal/tui; profile with BENCH_FLAGS='-cpuprofile=cpu.prof')
	@ go test -run '^$$' -bench '$(BENCH)' -benchmem $(BENCH_FLAGS) $(BENCH_PKG)

# Smoke: the live end-to-end test in e2e/ — its own module, so the root
# `go test ./...` never picks it up. See e2e/smoke_test.go for what it asserts.
SMOKE_MODEL ?= claude-haiku-4-5
.PHONY: smoke
smoke: ## real `evolve run all` on the marketplace fixture (SMOKE_MODEL=claude-haiku-4-5, 1 run, 1 job; needs the claude CLI + credentials)
	@ command -v claude >/dev/null 2>&1 || { echo "smoke: claude CLI not found in PATH" >&2; exit 2; }
	@ SMOKE_MODEL=$(SMOKE_MODEL) go -C e2e test -v -count=1 -run '^TestSmoke$$' .

.PHONY: run
run: build ## build and run locally (override args via ARGS=...)
	@ ./$(APP) $(ARGS)

# Regenerate the CLI/config reference from the built binary, then render the
# site. (Kept repo-local: generating a CLI reference is app-specific. `serve`,
# `docs-build`, and the zensical plumbing come from the library's docs.mk.)
.PHONY: docs
docs: build sync ## regenerate the cli reference (docs/cli, docs/man) and config docs (docs/config)
	@ ./$(APP) docs --out docs/cli --format markdown
	@ ./$(APP) docs --out docs/man --format man
	@ ./$(APP) docs --out docs/config --format config
	@ uv run zensical build
