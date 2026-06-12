# Developer tasks. `make help` lists targets; `make pr` is the full local gate.

APP     := evolve
APP_PKG := ./cmd
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

# Run the Node lint/format CLIs straight from node_modules so the versions pinned
# in package.json / package-lock.json are used — never a global or npx copy.
NPMBIN := ./node_modules/.bin

# Go developer CLIs (addlicense, goreleaser) are pinned in tools/go.mod — a
# separate module so their dependency graphs never touch the application's go.mod —
# and invoked with `go tool -modfile=tools/go.mod <name>`: compiled into the build
# cache on first use, no GOBIN, no binaries to manage. -modfile anchors on the root
# go.mod and runs the tool in the current directory, so relative paths just work.
# Do not add a go.work that `use`s tools/ — -modfile cannot be used in workspace mode.

# Smoke: a real `evolve run all` (live `claude` CLI + credentials) against a
# throwaway copy of the marketplace fixture. The fixture's evals are toy specs
# a live model may legitimately fail, so eval failures are tolerated (without
# --strict they warn and exit 0; an exit 1 is tolerated too); the gate is that
# the pipeline runs end-to-end and produces its artifacts.
SMOKE_MODEL ?= claude-haiku-4-5

.DEFAULT_GOAL := help
.PHONY: help pr fmt tidy vet lint lint-fix license test fuzz build run docs snapshot release smoke

help: ## list available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

pr: license tidy fmt vet lint-fix test fuzz build docs snapshot ## Full local gate — must pass before committing

# Install the pinned Node tools exactly as locked in package-lock.json.
# Re-runs only when package.json / the lockfile change.
node_modules: package.json package-lock.json
	npm ci
	@touch node_modules

fmt: node_modules ## format the go code, prose, and config (gofmt + prettier)
	go fmt ./...
	$(NPMBIN)/prettier --write .

tidy: ## tidy the go module references
	@ rm -f go.sum; go mod tidy
	@ rm -f tools/go.sum; go -C tools mod tidy

vet: node_modules ## vet the go code and lint markdown (go vet + markdownlint-cli2)
	go vet ./...
	$(NPMBIN)/markdownlint-cli2 "**/*.md"

lint: ## lint the go code
	go tool -modfile=tools/go.mod golangci-lint run

lint-fix: ## lint the go code and auto-fix issues
	go tool -modfile=tools/go.mod golangci-lint run --fix

license-check: ## check license headers
	go tool -modfile=tools/go.mod addlicense -check cmd internal

license: ## inject license headers
	go tool -modfile=tools/go.mod addlicense -c 'BitWise Media Group Ltd' -l mit -s=only -v cmd internal

test: ## run the unit tests (+ fuzz seed corpora)
	go test ./...

fuzz: ## fuzz one target (FUZZ=FuzzParse FUZZTIME=20s FUZZ_PKG=./internal/evalspec)
	go test -run '^$$' -fuzz '^$(FUZZ)$$' -fuzztime $(FUZZTIME) $(FUZZ_PKG)

smoke: build ## real `evolve run all` on the marketplace fixture (SMOKE_MODEL=claude-haiku-4-5, 1 run, 1 job; needs the claude CLI + credentials)
	@command -v claude >/dev/null 2>&1 || { echo "smoke: claude CLI not found in PATH" >&2; exit 2; }
	@tmp=$$(mktemp -d "$${TMPDIR:-/tmp}/evolve-smoke.XXXXXX"); \
	trap 'rm -rf "$$tmp"' EXIT; \
	cp -R testdata/repos/marketplace "$$tmp/repo"; \
	EVOLVE_CACHE_DIR="$$tmp/cache" ./$(APP) run all --root "$$tmp/repo" \
		--models $(SMOKE_MODEL) --runs 1 --jobs 1 --timeout 120; \
	status=$$?; \
	if [ "$$status" -gt 1 ]; then echo "smoke: evolve run all exited $$status" >&2; exit "$$status"; fi; \
	for f in plugins/alpha/evals/alpha-skill/results.json plugins/beta/evals/beta-skill/results.json; do \
		grep -q '"executed": true' "$$tmp/repo/$$f" \
			|| { echo "smoke: $$f is missing executed agent runs" >&2; exit 1; }; \
	done; \
	test -s "$$tmp/repo/EVALUATION.md" && test -s "$$tmp/repo/EVALUATION.json" \
		|| { echo "smoke: reports were not regenerated" >&2; exit 1; }; \
	echo "smoke: ok — pipeline ran end-to-end with $(SMOKE_MODEL) (evolve exit $$status; toy-eval failures tolerated)"

build: ## build the binary (./$(APP)) with version ldflags
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(APP) $(APP_PKG)

run: build ## build and run locally (override args via ARGS=...)
	./$(APP) $(ARGS)

docs: build ## regenerate the cli reference (docs/cli, docs/man) and config docs (docs/config)
	./$(APP) docs --out docs/cli --format markdown
	./$(APP) docs --out docs/man --format man
	./$(APP) docs --out docs/config --format config

# --skip=sign: cosign keyless signing needs the GitHub Actions OIDC token, so
# it only works in the release workflow — locally it would fail or prompt.
snapshot: ## build local snapshot (binaries, no publish, no signing)
	go tool -modfile=tools/go.mod goreleaser release --snapshot --clean --skip=sign

release: ## build and publish a release (needs a vX.Y.Z tag + creds)
	go tool -modfile=tools/go.mod goreleaser release --clean
