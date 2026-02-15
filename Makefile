# Makefile for spacemolt-agent-server

.PHONY: test test-race lint vet check-all clean build update-server-docs generate-mcp-tools update-mcp

# Run standard tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	go test -race -timeout=5m ./...

# Run golangci-lint
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found. Install with:"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$$(go env GOPATH)/bin"; \
		exit 1; \
	fi

# Run go vet
vet:
	@echo "Running go vet..."
	go vet ./...

# Run staticcheck if available
staticcheck:
	@echo "Running staticcheck..."
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "staticcheck not found. Install with:"; \
		echo "  go install honnef.co/go/tools/cmd/staticcheck@latest"; \
	fi

# Run all checks
check-all: vet lint staticcheck test-race
	@echo ""
	@echo "================================"
	@echo "All checks passed!"
	@echo "================================"

# Build all binaries
build:
	@echo "Building binaries..."
	go build -o bin/agent-server ./cmd/agent-server
	go build -o bin/auto-explorer ./cmd/auto-explorer
	go build -o bin/auto-miner ./cmd/auto-miner
	@echo "Binaries built in ./bin/"

# Build with race detector (for development/testing)
build-race:
	@echo "Building with race detector..."
	go build -race -o bin/agent-server-race ./cmd/agent-server
	@echo "Race-detecting binary built: ./bin/agent-server-race"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/
	go clean -testcache

# Show race detector usage help
help-race:
	@echo "Race Detector Usage:"
	@echo ""
	@echo "  make test-race          - Run all tests with race detector"
	@echo "  make build-race         - Build binaries with race detector for manual testing"
	@echo "  go test -race ./...     - Run tests with race detection"
	@echo "  go run -race main.go    - Run program with race detection"
	@echo ""
	@echo "Note: Race detector adds ~5-10x overhead (slower, more memory)"
	@echo "      Use only for testing, not production"

# Update server documentation from spacemolt.com
update-server-docs:
	@echo "Fetching latest API documentation from spacemolt.com..."
	go run ./cmd/update-server-docs

# Generate MCP tools from OpenAPI spec
generate-mcp-tools:
	@echo "Generating MCP tool definitions from OpenAPI spec..."
	go run ./cmd/generate-mcp-tools

# Update MCP bridge with latest API (fetch docs + regenerate tools)
update-mcp: update-server-docs generate-mcp-tools
	@echo ""
	@echo "================================"
	@echo "MCP bridge updated successfully!"
	@echo "================================"
	@echo ""
	@echo "Changes:"
	@echo "  - server_docs/openapi.json updated from game server"
	@echo "  - cmd/mcp-ws-bridge/tools_generated.go regenerated"
	@echo ""
	@echo "Next steps:"
	@echo "  1. Review changes: git diff server_docs/ cmd/mcp-ws-bridge/"
	@echo "  2. Test bridge: make test"
	@echo "  3. Commit if satisfied: git add -A && git commit -m 'chore: update MCP tools from latest API'"
