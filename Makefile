.PHONY: all build test test-race bench bench-smoke fuzz-smoke fuzz-long soak-smoke soak-long govulncheck docker-build clean examples run-basic run-rest run-ws run-prod fmt lint check ci release-check help

all: build test

build:
	@echo "Building repository..."
	@go build ./...
	@echo "Build successful!"

examples:
	@echo "Building examples..."
	@mkdir -p bin
	@go build -o bin/basic_usage ./examples/basic_usage
	@go build -o bin/rest_api ./examples/rest_api
	@go build -o bin/websocket_chat ./examples/websocket_chat
	@go build -o bin/httpserverd ./cmd/httpserverd
	@echo "Artifacts built successfully in bin/"

test:
	@echo "Running tests..."
	@go test ./... -v

test-race:
	@echo "Running tests with race detector..."
	@go test ./... -race -v

bench:
	@echo "Running benchmarks..."
	@go test ./tests/benchmark -run '^$$' -bench=. -benchmem

bench-smoke:
	@echo "Running benchmark smoke suite..."
	@go test ./tests/benchmark -run '^$$' -bench=. -benchmem -benchtime=100ms

fuzz-smoke:
	@echo "Running fuzz smoke tests..."
	@go test ./internal/request -run '^$$' -fuzz=FuzzParserParse -fuzztime=2s
	@go test ./internal/router -run '^$$' -fuzz=FuzzRouterServeHTTP -fuzztime=2s
	@go test ./internal/server -run '^$$' -fuzz=FuzzValidatePath -fuzztime=2s

fuzz-long:
	@echo "Running longer fuzz validation..."
	@FUZZ_TIME=$${FUZZ_TIME:-30s}; \
	go test ./internal/request -run '^$$' -fuzz=FuzzParserParse -fuzztime=$$FUZZ_TIME && \
	go test ./internal/router -run '^$$' -fuzz=FuzzRouterServeHTTP -fuzztime=$$FUZZ_TIME && \
	go test ./internal/server -run '^$$' -fuzz=FuzzValidatePath -fuzztime=$$FUZZ_TIME

soak-smoke:
	@echo "Running soak smoke validation..."
	@HTTP_SERVER_RUN_SOAK=1 \
	HTTP_SERVER_SOAK_DURATION=$${HTTP_SERVER_SOAK_DURATION:-3s} \
	HTTP_SERVER_SOAK_CONCURRENCY=$${HTTP_SERVER_SOAK_CONCURRENCY:-24} \
	HTTP_SERVER_SOAK_MALFORMED_WORKERS=$${HTTP_SERVER_SOAK_MALFORMED_WORKERS:-2} \
	go test ./tests/soak -run TestServerSoak -count=1 -v

soak-long:
	@echo "Running longer soak validation..."
	@HTTP_SERVER_RUN_SOAK=1 \
	HTTP_SERVER_SOAK_DURATION=$${HTTP_SERVER_SOAK_DURATION:-30s} \
	HTTP_SERVER_SOAK_CONCURRENCY=$${HTTP_SERVER_SOAK_CONCURRENCY:-64} \
	HTTP_SERVER_SOAK_MALFORMED_WORKERS=$${HTTP_SERVER_SOAK_MALFORMED_WORKERS:-2} \
	go test ./tests/soak -run TestServerSoak -count=1 -v

govulncheck:
	@echo "Running govulncheck..."
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./...

docker-build:
	@echo "Building container image..."
	@docker build -t http-server:local .

clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf dist/
	@rm -f *.test
	@rm -f *.out
	@echo "Clean complete!"

run-basic:
	@go run ./examples/basic_usage

run-rest:
	@go run ./examples/rest_api

run-ws:
	@go run ./examples/websocket_chat

run-prod:
	@go run ./cmd/httpserverd

fmt:
	@echo "Formatting code..."
	@go fmt ./...

lint:
	@echo "Linting code..."
	@go vet ./...

check: fmt lint
	@echo "Running checks..."
	@go test ./... -race -cover

ci: build test test-race bench-smoke fuzz-smoke govulncheck

release-check: build test test-race bench-smoke govulncheck docker-build
	@echo "Release checks completed."

help:
	@echo "HTTP Server - Makefile commands:"
	@echo "  make build         - Build the repository"
	@echo "  make examples      - Build example binaries and httpserverd"
	@echo "  make test          - Run all tests"
	@echo "  make test-race     - Run tests with race detector"
	@echo "  make bench         - Run the full benchmark suite"
	@echo "  make bench-smoke   - Run a short benchmark smoke suite"
	@echo "  make fuzz-smoke    - Run short fuzz smoke tests"
	@echo "  make fuzz-long     - Run longer fuzz validation"
	@echo "  make soak-smoke    - Run short soak/load validation"
	@echo "  make soak-long     - Run longer soak/load validation"
	@echo "  make govulncheck   - Run govulncheck"
	@echo "  make docker-build  - Build the production container image"
	@echo "  make run-prod      - Run the production reference server"
	@echo "  make ci            - Run the CI-equivalent validation suite"
	@echo "  make release-check - Run the release readiness suite"
