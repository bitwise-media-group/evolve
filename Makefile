# one -ignore flag per non-empty line in .licenseignore (quoted to avoid shell globbing)
LICENSE_HOLDER := 'Bitwise Media Group Ltd'
LICENSE_IGNORE := $(foreach pattern,$(shell cat .licenseignore 2>/dev/null),-ignore '$(pattern)')

# Developer tasks. `make help` lists targets; `make pr` is the full local gate.
APP_PKG := ./cmd/evolve
MODULE  := $(shell go list -m)

# Version metadata stamped into the binary via -ldflags. GoReleaser injects the
# same vars at the same import path ($(MODULE)/internal/version) on tagged releases.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.BuildDate=$(DATE)

# Fuzzing: `make fuzz` runs one target (FUZZ=) for FUZZTIME.
FUZZ_PKG ?= ./internal/manifest
FUZZ     ?= FuzzFrontmatter
FUZZTIME ?= 20s

# Go developer CLIs (addlicense, goreleaser) are pinned in tools/go.mod — a
# separate module so their dependency graphs never touch the application's go.mod —
# and invoked with `go tool -modfile=tools/go.mod <name>`: compiled into the build
# cache on first use, no GOBIN, no binaries to manage. -modfile anchors on the root
# go.mod and runs the tool in the current directory, so relative paths just work.
# Do not add a go.work that `use`s tools/ — -modfile cannot be used in workspace mode.

# Smoke: the live end-to-end test in e2e/ — its own module, so the root
# `go test ./...` never picks it up. See e2e/smoke_test.go for what it asserts.
SMOKE_MODEL ?= claude-haiku-4-5

.DEFAULT_GOAL := help

.PHONY: help
help: ## list available targets
	@ grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

.PHONY: pr
pr: tidy fmt lint test fuzz build docs snapshot smoke commit ## full local gate, then run any pending ./commit.sh

.PHONY: ci
ci: lint test fuzz build docs snapshot ## continuous integration gate

.PHONY: commit
commit: ## run ./commit.sh (agent-prepared commit batch) if present
	@ if [ -x ./commit.sh ]; then ./commit.sh; fi

# Install the pinned Node tools exactly as locked in package-lock.json.
# Re-runs only when package.json / the lockfile change.
node_modules: package.json package-lock.json
	@ npm ci --ignore-scripts --no-fund
	@ touch node_modules

# Platform build-tag matrix. The host GOOS only compiles its own files, so the
# linters skip every other platform's build-constrained source (the sandbox_*.go /
# proc_*.go split in internal/runner). Vetting/linting under each GOOS in turn
# covers them all: linux and darwin pick up sandbox_{linux,darwin}.go + proc_unix.go,
# windows picks up proc_windows.go + sandbox_other.go (!linux && !darwin). gofmt
# itself ignores build tags, so `go fmt` already reaches every file.
#
# go vet is part of the go command and analyses the target GOOS while running on the
# host, so `GOOS=… go vet` just works. `go tool golangci-lint`, though, would
# cross-*build* golangci-lint for that GOOS and then fail to exec the foreign binary
# on the host — so build one host binary up front and run it under each GOOS instead.
LINT_GOOS ?= linux darwin windows

.PHONY: fmt
fmt: node_modules ## format the go code, prose, and config (gofmt + prettier)
	@ go tool -modfile=tools/go.mod addlicense -c $(LICENSE_HOLDER) -l mit -s=only $(LICENSE_IGNORE) .
	@ go fmt ./...
	@ go -C e2e fmt ./...
	@ bin=$$(mktemp -d "$${TMPDIR:-/tmp}/evolve-fmt.XXXXXX"); ret=0; \
		go build -modfile=tools/go.mod -o "$$bin/" github.com/golangci/golangci-lint/v2/cmd/golangci-lint; \
		for goos in $(LINT_GOOS); do GOOS=$$goos "$$bin/golangci-lint" run --fix || { ret=1; break; }; done; \
		rm -rf "$$bin"; exit $$ret
	@ npm run format
	@ npm run lint:fix

tidy: go.mod e2e/go.mod tools/go.mod ## tidy the go module references
	@ rm -f go.sum; go mod tidy
	@ rm -f tools/go.sum; go -C tools mod tidy
	@ rm -f e2e/go.sum; go -C e2e mod tidy

.PHONY: lint
lint: node_modules ## lint the go code (across the LINT_GOOS build-tag matrix)
	@ go tool -modfile=tools/go.mod addlicense -check -c $(LICENSE_HOLDER) -l mit -s=only $(LICENSE_IGNORE) .
	@ bin=$$(mktemp -d "$${TMPDIR:-/tmp}/evolve-lint.XXXXXX"); ret=0; \
		go build -modfile=tools/go.mod -o "$$bin/" github.com/golangci/golangci-lint/v2/cmd/golangci-lint; \
		for goos in $(LINT_GOOS); do \
			echo "  lint: GOOS=$$goos"; \
			GOOS=$$goos go vet ./... && GOOS=$$goos "$$bin/golangci-lint" run || { ret=1; break; }; \
		done; \
		rm -rf "$$bin"; exit $$ret
	@ go tool -modfile=tools/go.mod govulncheck ./...
	@ go -C e2e vet ./...
	@ npm run lint
	@ npm run format:check

# -covermode=atomic is the race-safe counter mode `-race` requires. gocover-cobertura
# is pinned in tools/go.mod and run via `go tool` — no `go install` of an @latest tool.
# Coverage lands in coverage/ where the reusable CI workflow uploads it to Codecov.
.PHONY: test
test: ## run the unit tests (+ fuzz seed corpora)
	@ mkdir -p coverage
	@ go test -race -covermode=atomic -coverprofile=coverage/coverage.out ./...
	@ go tool -modfile=tools/go.mod gocover-cobertura <coverage/coverage.out >coverage/cobertura-coverage.xml

.PHONY: fuzz
fuzz: ## fuzz one target (FUZZ=FuzzParse FUZZTIME=20s FUZZ_PKG=./internal/evalspec)
	@ go test -run '^$$' -fuzz '^$(FUZZ)$$' -fuzztime $(FUZZTIME) $(FUZZ_PKG)

.PHONY: smoke
smoke: ## real `evolve run all` on the marketplace fixture (SMOKE_MODEL=claude-haiku-4-5, 1 run, 1 job; needs the claude CLI + credentials)
	@ command -v claude >/dev/null 2>&1 || { echo "smoke: claude CLI not found in PATH" >&2; exit 2; }
	@ SMOKE_MODEL=$(SMOKE_MODEL) go -C e2e test -v -count=1 -run '^TestSmoke$$' .

.PHONY: build
build: ## build the binary (./evolve) with version ldflags
	@ CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o evolve $(APP_PKG)

.PHONY: run
run: build ## build and run locally (override args via ARGS=...)
	@ ./evolve $(ARGS)

.PHONY: docs
docs: build ## regenerate the cli reference (docs/cli, docs/man) and config docs (docs/config)
	@ ./evolve docs --out docs/cli --format markdown
	@ ./evolve docs --out docs/man --format man
	@ ./evolve docs --out docs/config --format config

# --skip=sign: cosign keyless signing needs the GitHub Actions OIDC token, so
# it only works in the release workflow — locally it would fail or prompt.
.PHONY: snapshot
snapshot: ## build local snapshot (binaries, no publish, no signing)
	@ go tool -modfile=tools/go.mod goreleaser release --snapshot --clean --skip=sign

.PHONY: release
release: ## build and publish a release (needs a vX.Y.Z tag + creds)
	@ go tool -modfile=tools/go.mod goreleaser release --clean
