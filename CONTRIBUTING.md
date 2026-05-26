# Contributing

## Scope

This repository treats correctness, transport safety, and operational clarity as first-order concerns. Changes should preserve the current validation baseline and extend it when behavior changes.

## Development workflow

1. Create a topic branch from `main`.
2. Make focused changes with matching tests or validation updates.
3. Run the local validation suite before opening a PR:
   - `make build`
   - `make test`
   - `make test-race`
   - `make bench-smoke`
   - `make fuzz-smoke`
   - `make govulncheck`
4. Update `CHANGELOG.md` for user-visible behavior changes.
5. Open a pull request using the repository template.

## Coding expectations

- Keep dependencies minimal. The core library should remain dependency-light.
- Prefer small, isolated changes over broad rewrites.
- Add tests for regressions and edge cases.
- Document operational consequences when config defaults or runtime behavior change.

## Commit and PR expectations

- Use precise commit messages.
- Link the relevant issue or explain why the change is untracked.
- Keep PR descriptions explicit about risk, validation, and rollback impact.

## Security reporting

Do not open a public issue for undisclosed vulnerabilities. Follow `SECURITY.md`.
