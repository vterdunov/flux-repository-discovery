# Live GitHub discovery fixtures

`mise run test-e2e` builds the current binary with the race detector, starts its
real HTTP server, and discovers these persistent private repositories through
`api.github.com`. Tests only read GitHub metadata. They never create, change,
archive, or delete repositories at runtime, so concurrent CI runs share fixtures.

| Repository | Purpose | Topics |
| --- | --- | --- |
| [frd-e2e-api](https://github.com/vterdunov/frd-e2e-api) | Selected production backend | `frd-e2e`, `gitops`, `backend`, `production` |
| [frd-e2e-worker](https://github.com/vterdunov/frd-e2e-worker) | Selected staging backend | `frd-e2e`, `gitops`, `backend`, `staging` |
| [frd-e2e-excluded](https://github.com/vterdunov/frd-e2e-excluded) | Matches inclusion, rejected by exclusion | `frd-e2e`, `gitops`, `backend`, `production`, `skip-discovery` |
| [frd-e2e-unmatched](https://github.com/vterdunov/frd-e2e-unmatched) | Active repository outside the topic selection | `frd-e2e`, `docs` |
| [frd-e2e-archived](https://github.com/vterdunov/frd-e2e-archived) | Matching archived repository is always omitted | `frd-e2e`, `gitops`, `backend`, `production` |

All five are owned by `vterdunov` and are not forks. Their IDs, visibility,
archive/fork flags, and exact topic sets are pinned in [fixtures.json](fixtures.json).
The preflight reads each fixture directly and checks this metadata, including
negative fixtures. Missing access or changed fixtures fail the test explicitly.
These direct lookups validate the fixtures; discovery itself uses owner catalogs.
Avoid reusing the `frd-e2e-` prefix or the `frd-e2e` topic for unrelated repositories.

The HTTP assertions check complete, numerically sorted inputs including stable
IDs, owner, name, and full name. Scenarios cover `topics.all` / `topics.any`, AND
within a rule, OR across rules, exact names, regular expressions, case folding,
topic and exact-name exclusions, archived repositories, source deduplication,
and the exact empty response `{"inputs":[]}`. A real CLI dry-run replaces the
production selection with staging, verifies the added/removed inputs, and checks
that the published generation and inputs remain unchanged. Shutdown uses SIGTERM.

## CI credentials

Create a fine-grained personal access token owned by `vterdunov`, selecting only
the five repositories above. It needs the automatically included **Metadata:
read** permission; no repository contents or write permissions are required.
Give it an expiration and rotate it before expiry. Save it as repository Actions
secret **`FRD_E2E_GITHUB_TOKEN`** in `vterdunov/flux-repository-discovery`.

The default workflow `GITHUB_TOKEN` is an installation token and cannot call the
personal-account `/user/repos` endpoint used by discovery. That endpoint supports
fine-grained PATs and GitHub App user tokens, as documented in the
[GitHub repository API](https://docs.github.com/en/rest/repos/repos#list-repositories-for-the-authenticated-user).

CI runs the live test on pushes to `main` and same-repository PRs, except
Dependabot. Fork and Dependabot PRs skip only this secret-dependent job and still
run the offline checks and image build. Image publishing waits for both checks
and the live test. A missing or expired secret fails the live job, never skips it.

## Local run

Export `FRD_E2E_GITHUB_TOKEN` through your secret manager, then run:

```sh
mise run test-e2e
```

To use an already authenticated `gh` session for a one-time local check without
writing its credential to disk or CI:

```sh
mise exec -- bash -euo pipefail -c 'FRD_E2E_GITHUB_TOKEN="$(gh auth token)" mise run test-e2e'
```

The live test is opt-in locally and excluded from `mise run check`. The normal
lint task includes its build tag. The test uses temporary binaries and candidate
files, dynamic loopback ports, bounded deadlines, and redacted process logs.
It waits for the first completed scan, not just `/readyz`. Once a scan completes,
failed sources or mismatched inputs fail immediately; assertions are not retried.

This test does not cover GitHub App installation authentication, organization
catalogs, forks, or Flux reconciliation. Those remain covered by the existing
component and Flux integration suites. It exercises real pagination when the
owner's accessible catalog spans multiple pages, without requiring a fixed
number of unrelated repositories.
