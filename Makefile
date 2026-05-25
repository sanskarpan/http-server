.PHONY: all build test bench fuzz-smoke fuzz-long soak-smoke soak-long clean examples run-basic run-rest run-ws

# Build all
all: build test

# Build library
build:
	@echo "Building HTTP Server library..."
	@go build ./internal/... ./pkg/...
	@echo "Build successful!"

# Build examples
examples:
	@echo "Building examples..."
	@mkdir -p bin
	@go build -o bin/basic_usage ./examples/basic_usage
	@go build -o bin/rest_api ./examples/rest_api
	@go build -o bin/websocket_chat ./examples/websocket_chat
	@echo "Examples built successfully in bin/"

# Run tests
test:
	@echo "Running tests..."
	@go test ./... -v

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	@go test ./... -race -v

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	@go test ./tests/benchmark -run '^$$' -bench=. -benchmem

# Run short fuzz smoke tests
fuzz-smoke:
	@echo "Running fuzz smoke tests..."
	@go test ./internal/request -run '^$$' -fuzz=FuzzParserParse -fuzztime=2s
	@go test ./internal/router -run '^$$' -fuzz=FuzzRouterServeHTTP -fuzztime=2s
	@go test ./internal/server -run '^$$' -fuzz=FuzzValidatePath -fuzztime=2s

# Run longer fuzz validation (override FUZZ_TIME as needed)
fuzz-long:
	@echo "Running longer fuzz validation..."
	@FUZZ_TIME=$${FUZZ_TIME:-30s}; \
	go test ./internal/request -run '^$$' -fuzz=FuzzParserParse -fuzztime=$$FUZZ_TIME && \
	go test ./internal/router -run '^$$' -fuzz=FuzzRouterServeHTTP -fuzztime=$$FUZZ_TIME && \
	go test ./internal/server -run '^$$' -fuzz=FuzzValidatePath -fuzztime=$$FUZZ_TIME

# Run short soak/load validation
soak-smoke:
	@echo "Running soak smoke validation..."
	@HTTP_SERVER_RUN_SOAK=1 \
	HTTP_SERVER_SOAK_DURATION=$${HTTP_SERVER_SOAK_DURATION:-3s} \
	HTTP_SERVER_SOAK_CONCURRENCY=$${HTTP_SERVER_SOAK_CONCURRENCY:-24} \
	HTTP_SERVER_SOAK_MALFORMED_WORKERS=$${HTTP_SERVER_SOAK_MALFORMED_WORKERS:-2} \
	go test ./tests/soak -run TestServerSoak -count=1 -v

# Run longer soak/load validation
soak-long:
	@echo "Running longer soak validation..."
	@HTTP_SERVER_RUN_SOAK=1 \
	HTTP_SERVER_SOAK_DURATION=$${HTTP_SERVER_SOAK_DURATION:-30s} \
	HTTP_SERVER_SOAK_CONCURRENCY=$${HTTP_SERVER_SOAK_CONCURRENCY:-64} \
	HTTP_SERVER_SOAK_MALFORMED_WORKERS=$${HTTP_SERVER_SOAK_MALFORMED_WORKERS:-2} \
	go test ./tests/soak -run TestServerSoak -count=1 -v

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f *.test
	@rm -f *.out
	@echo "Clean complete!"

# Run basic example
run-basic:
	@go run ./examples/basic_usage

# Run REST API example
run-rest:
	@go run ./examples/rest_api

# Run WebSocket chat example
run-ws:
	@go run ./examples/websocket_chat

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

# Lint code
lint:
	@echo "Linting code..."
	@go vet ./...

# Check for code issues
check: fmt lint
	@echo "Running checks..."
	@go test ./... -race -cover

# Help
help:
	@echo "HTTP Server - Makefile commands:"
	@echo "  make build       - Build the library"
	@echo "  make examples    - Build all examples"
	@echo "  make test        - Run all tests"
	@echo "  make test-race   - Run tests with race detector"
	@echo "  make bench       - Run benchmarks"
	@echo "  make fuzz-smoke  - Run short fuzz smoke tests"
	@echo "  make fuzz-long   - Run longer fuzz validation"
	@echo "  make soak-smoke  - Run short soak/load validation"
	@echo "  make soak-long   - Run longer soak/load validation"
	@echo "  make clean       - Clean build artifacts"
	@echo "  make run-basic   - Run basic example"
	@echo "  make run-rest    - Run REST API example"
	@echo "  make run-ws      - Run WebSocket chat example"
	@echo "  make fmt         - Format code"
	@echo "  make lint        - Lint code"
	@echo "  make check       - Run all checks"
