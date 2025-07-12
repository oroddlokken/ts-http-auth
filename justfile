# Format the Go codebase
fmt:
	gofmt -s -w ./cmd/ ./internal/

# Lint the Go codebase
lint:
	golangci-lint run ./...
