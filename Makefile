.PHONY: build test test-verbose test-race coverage coverage-html lint clean deps check install install-hooks help

BINARY=dayshift
PKG=./cmd/dayshift

build:
	go build -o $(BINARY) $(PKG)

install:
	go install $(PKG)
	@echo "Installed $(BINARY) to $$(if [ -n "$$(go env GOBIN)" ]; then go env GOBIN; else echo "$$(go env GOPATH)/bin"; fi)"

test:
	go test ./...

test-verbose:
	go test -v ./...

test-race:
	go test -race ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo ""
	@echo "To view HTML coverage report, run: go tool cover -html=coverage.out"

coverage-html: coverage
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

lint:
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest" && exit 1)
	golangci-lint run

clean:
	rm -f $(BINARY)
	rm -f coverage.out
	rm -f coverage.html

deps:
	go mod download
	go mod tidy

check: test lint

install-hooks:
	@ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit
	@echo "✓ pre-commit hook installed (.git/hooks/pre-commit → scripts/pre-commit.sh)"

help:
	@echo "Available targets:"
	@echo "  build         - Build the binary"
	@echo "  test          - Run all tests"
	@echo "  test-verbose  - Run tests with verbose output"
	@echo "  test-race     - Run tests with race detection"
	@echo "  coverage      - Run tests with coverage report"
	@echo "  coverage-html - Generate HTML coverage report"
	@echo "  lint          - Run golangci-lint"
	@echo "  clean         - Clean build artifacts"
	@echo "  deps          - Download and tidy dependencies"
	@echo "  check         - Run tests and lint"
	@echo "  install       - Build and install to Go bin directory"
	@echo "  install-hooks - Install git pre-commit hook"
	@echo "  help          - Show this help"
