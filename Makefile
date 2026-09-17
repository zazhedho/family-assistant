GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo "$$(go env GOPATH)/bin/golangci-lint")
GOLANGCI_LINT_VERSION := v2.13.2

.PHONY: lint lint-install run debug hook-install

lint:
	@echo "Running golangci-lint..."
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		echo "golangci-lint is not installed. Run: make lint-install"; \
		exit 1; \
	fi
	@$(GOLANGCI_LINT) run --config .golangci.yml --timeout=5m ./...
	@echo "golangci-lint passed."

lint-install:
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

run:
	@go run main.go

debug: lint
	@go run main.go

hook-install:
	@git config core.hooksPath .githooks
	@chmod +x .githooks/pre-commit .githooks/pre-push
	@echo "Git hooks installed. Lint will run on commit and push."
