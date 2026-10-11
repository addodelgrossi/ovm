SHELL := /bin/sh

GO_TOOLCHAIN := go1.27.2
WAILS_VERSION := v2.16.0
GOLANGCI_LINT_VERSION := v2.14.0
ACTIONLINT_VERSION := v1.7.12
GORELEASER_VERSION := v2.12.7
TAG ?= v0.1.0
GO := GOTOOLCHAIN=$(GO_TOOLCHAIN) go
FRONTEND_DIR := desktop/frontend
UNAME_S := $(shell uname -s)

ifeq ($(UNAME_S),Darwin)
DESKTOP_ENV := MACOSX_DEPLOYMENT_TARGET=13.0 CGO_CFLAGS=-mmacosx-version-min=13.0 CGO_LDFLAGS=-mmacosx-version-min=13.0
else
DESKTOP_ENV :=
endif

.PHONY: help format format-check lint actionlint vet test test-race frontend-install frontend-check frontend-test frontend-build build-cli build-desktop ci clean release-snapshot package-macos package-linux validation-gate

help:
	@printf '%s\n' \
		'format          Format project sources' \
		'format-check    Verify source formatting' \
		'lint            Run golangci-lint' \
		'actionlint       Validate GitHub Actions workflows' \
		'vet             Run go vet' \
		'test            Run Go tests' \
		'test-race       Run Go tests with the race detector' \
		'frontend-check  Run frontend formatting, lint, and type checks' \
		'frontend-test   Run frontend tests once' \
		'frontend-build  Build frontend assets' \
		'build-cli       Build the local CLI' \
		'build-desktop   Build the native Wails application' \
		'ci              Run the complete CI validation gate' \
		'release-snapshot Build CLI release archives locally without publishing' \
		'package-macos   Build the universal macOS DMG (dry-run version)' \
		'package-linux   Build the Ubuntu package for this machine (dry-run version)' \
		'validation-gate Check docs/validation/$$TAG.json (TAG=v0.1.0)'

format:
	@files="$$(find . -name '*.go' -type f -not -path './.git/*' -not -path '*/node_modules/*')"; \
	if [ -n "$$files" ]; then gofmt -w $$files; fi
	cd $(FRONTEND_DIR) && npm run format

format-check:
	@files="$$(find . -name '*.go' -type f -not -path './.git/*' -not -path '*/node_modules/*')"; \
	unformatted="$$(if [ -n "$$files" ]; then gofmt -l $$files; fi)"; \
	if [ -n "$$unformatted" ]; then printf 'Unformatted Go files:\n%s\n' "$$unformatted"; exit 1; fi
	cd $(FRONTEND_DIR) && npm run format:check

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

actionlint:
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

frontend-install:
	cd $(FRONTEND_DIR) && npm ci

frontend-check:
	cd $(FRONTEND_DIR) && npm run format:check && npm run lint && npm run check

frontend-test:
	cd $(FRONTEND_DIR) && npm test

frontend-build:
	cd $(FRONTEND_DIR) && npm run build

build-cli:
	@mkdir -p build
	$(GO) build -trimpath -o build/ovm ./cmd/ovm

build-desktop: frontend-build
	cd desktop && $(DESKTOP_ENV) $(GO) run github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION) build -clean
	cd $(FRONTEND_DIR) && npx prettier --write --ignore-path /dev/null "wailsjs/go/**/*.{ts,js}"

ci: format-check lint actionlint vet test test-race frontend-check frontend-test frontend-build build-cli build-desktop

clean:
	rm -rf build dist desktop/build/bin desktop/frontend/dist/assets desktop/frontend/dist/index.html

DRYRUN_VERSION = 0.0.0-dryrun+$(shell git rev-parse --short=12 HEAD)
COMMIT = $(shell git rev-parse HEAD)
COMMIT_DATE = $(shell git log -1 --format=%cI)

release-snapshot:
	$(GO) run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) release --snapshot --clean --skip=sbom

package-macos: frontend-build
	scripts/package-macos.sh "$(DRYRUN_VERSION)" "$(COMMIT)" "$(COMMIT_DATE)"

package-linux: frontend-build
	scripts/package-linux.sh "$(DRYRUN_VERSION)" "$(COMMIT)" "$(COMMIT_DATE)"

validation-gate:
	$(GO) run ./internal/tools/validationgate docs/validation/$(TAG).json $(TAG)
