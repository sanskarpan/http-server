# GitHub Admin Policy

## Desired repository policy

The intended `main` branch policy is:
- pull-request-only changes
- at least 1 approving review
- stale review dismissal on new commits
- code-owner review required
- conversation resolution required
- required status checks:
  - `build`
  - `test`
  - `race`
  - `benchmark-smoke`
  - `fuzz-smoke`
  - `govulncheck`
  - `docker-build`
- merge strategy:
  - squash merge enabled
  - merge commit disabled
  - rebase merge disabled
  - delete branch on merge enabled

## Current constraint

The repository is currently private on a GitHub plan that returns `403 Upgrade to GitHub Pro or make this repository public to enable this feature` for branch protection and ruleset APIs.

The merge-strategy settings that do not require branch protection are already applied:
- squash merge enabled
- merge commit disabled
- rebase merge disabled
- delete branch on merge enabled

The workflow files and required-check names are also in place, but enforcement cannot be applied through `gh api` until either:
- the repository is made public, or
- the account is upgraded to a plan that supports branch protection on private repositories

## Commands to apply once the hosting plan allows it

```bash
gh api -X PUT repos/sanskarpan/http-server/branches/main/protection \
  -F required_status_checks.strict=true \
  -f required_status_checks.contexts[]='build' \
  -f required_status_checks.contexts[]='test' \
  -f required_status_checks.contexts[]='race' \
  -f required_status_checks.contexts[]='benchmark-smoke' \
  -f required_status_checks.contexts[]='fuzz-smoke' \
  -f required_status_checks.contexts[]='govulncheck' \
  -f required_status_checks.contexts[]='docker-build' \
  -F enforce_admins=true \
  -F required_pull_request_reviews.dismiss_stale_reviews=true \
  -F required_pull_request_reviews.require_code_owner_reviews=true \
  -F required_pull_request_reviews.required_approving_review_count=1 \
  -F required_conversation_resolution=true \
  -F restrictions=
```

```bash
gh repo edit sanskarpan/http-server \
  --delete-branch-on-merge \
  --enable-merge-commit=false \
  --enable-rebase-merge=false \
  --enable-squash-merge
```
