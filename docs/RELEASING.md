# Releasing

This repository uses Semantic Versioning.

## Release process

1. Ensure `main` is green in CI.
2. Update `CHANGELOG.md`.
3. Confirm `VERSION` matches the intended release.
4. Run `make release-check`.
5. Create an annotated tag:
   - `git tag -a v0.1.0 -m "Release v0.1.0"`
   - `git push origin v0.1.0`
6. The `release` GitHub Actions workflow builds platform artifacts and creates a GitHub Release with generated release notes.

## Versioning guidance

- Patch: bug fixes, test-only hardening, and docs corrections without API breakage
- Minor: backward-compatible features or operational improvements
- Major: breaking public API or compatibility changes

## Release notes

- Summarize user-visible changes from `CHANGELOG.md`
- Call out breaking changes explicitly
- Include the validation suite used for the release candidate
