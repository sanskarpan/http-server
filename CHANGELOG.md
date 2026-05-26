# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog and this repository follows Semantic Versioning.

## [Unreleased]

### Added
- GitHub Actions workflows for CI, release packaging, and scheduled long-run validation.
- A production reference server at `cmd/httpserverd` with JSON access logs, liveness/readiness endpoints, and a Prometheus-compatible metrics surface.
- Deployment artifacts including a multi-stage `Dockerfile`, `compose.yaml`, and `deploy/production.env.example`.
- Governance files including `LICENSE`, `SECURITY.md`, `CONTRIBUTING.md`, `CODEOWNERS`, and issue/PR templates.
- Operations, release, and validation documentation under `docs/`.

### Changed
- The public module path now matches the published GitHub repository: `github.com/sanskarpan/http-server`.
- README claims now reflect validated behavior instead of implying unverified RFC/performance guarantees.
- Request execution is now scheduled per request instead of per connection, preventing keep-alive sockets from starving the worker pool during long-running load.

## [0.1.0] - 2026-05-26

### Added
- Hardened request parsing, response handling, router behavior, static file serving, worker pool lifecycle, and WebSocket validation.
- Benchmarks, fuzz harnesses, soak tests, and security validation with `govulncheck`.
