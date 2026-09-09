# GitHub adapter implementation evidence

Date: 2026-09-09. Implementation author is separate from the API/test author.

The API declarations and acceptance tests frozen in [tdd-github.md](tdd-github.md)
were implemented without the implementation author changing those tests.
The later independent fixed-clock harness correction is recorded below. The production adapter uses the
standard library and the existing pinned YAML dependency only.

## Behavior implemented

- Strict server credential registry, PAT env/file references, PKCS#1/PKCS#8 RSA
  App keys, positive IDs, and secret-safe loader errors.
- PAT owner discovery with organization pagination and merged public/private
  personal catalogs; one credential's installation catalog for GitHub Apps.
- Complete pagination or failure, no partial or previous catalog fallback,
  same-scan request reuse, independent source results, numeric-ID ordering.
- Mandatory metadata validation and missing-topics completion; archived/fork
  exclusion before extra metadata requests; conflicting IDs fail closed.
- GitHub API version `2026-03-10`, fixed HTTPS API origin, no redirects, explicit
  API headers, bounded response bodies, request timeouts and context propagation.
- Three-attempt temporary failure budget, context-aware waits, per-credential
  serialization and cooldown shared by every scan using the client.
- RS256 App JWTs, installation account checks including empty catalogs, early
  installation token refresh, synchronized concurrent exchange and one refresh
  after an installation token receives HTTP 401.
- Strict JSON structure, duplicate-key rejection and fail-closed pagination
  links; no upstream response/transport errors in exported error messages.

## Commands and results

Using `MISE_TRUSTED_CONFIG_PATHS` set to this workspace:

```text
mise exec -- go test ./internal/github
ok github.com/vterdunov/flux-repository-discovery/internal/github

mise exec -- go test -race ./internal/github
ok github.com/vterdunov/flux-repository-discovery/internal/github

mise exec -- golangci-lint run ./internal/github/...
0 issues.
```

The first implementation test run exposed a test-clock mismatch: fake Wait
combined with real Now expected an exact two-second delay despite elapsed
scheduler time. Initial production rounding was subsequently removed after the
independent test author fixed the harness clock, as documented in
[tdd-github.md](tdd-github.md). Assertions remained unchanged. Production waits
for the actual remaining cooldown, including far-future reset timestamps without
duration-rounding overflow.

All tests use synthetic credentials and in-memory HTTP transport. No real
GitHub credentials, GitHub network access or live repository mutations occurred.
These checks do not claim live Flux Operator integration coverage.

Final acceptance file hashes verified after hardening GREEN:

```text
6fea845109733d24b73bdcc9d4a9cce19e7453540330c6612638931e0036b63e  internal/github/app_test.go
fc75bc17cddb90a5d1867af840c259ce8dfac27840a8e38a970566d1ac522312  internal/github/client_test.go
44aa2b4c89506e092144f75fe948255d8c58c9ac20ec3128297c62fd4fd6550e  internal/github/credentials_test.go
8f35e32ca3f6fb446da8c485d904cc478d0a96643d5c640470bb59a3f70a83cd  internal/github/regression_test.go
d0b96bade22fcdbd763b5c5d4be6796ba8f660492abdd8bac05912be1f9011c1  internal/github/final_regression_test.go
```

## Independent hardening RED to GREEN

The independent API/test author reproduced integrity gaps before corrective
implementation and recorded them in `docs/tdd-github-regression-red.txt`.
All regression scenarios now pass along with the original acceptance suite:

- Duplicate JSON field comparison includes case aliases, preventing `id`/`ID`
  from overriding mandatory identity fields.
- Conflicting identities, archive/fork flags and known topics are checked
  before exclusion, so a skipped duplicate cannot leave an authoritative copy.
- Organization, installation and public user catalogs validate their scoped
  owner. Only `/user/repos` intentionally includes other accessible owners.
- Pagination preserves all query parameters except the incrementing page and
  requires contiguous pages with exactly one page parameter.
- Far-future cooldown uses the remaining duration without overflow-prone
  rounding. The corrected fixed-clock test remains deterministic.
- Independent credentials scan concurrently with at most four workers. Each
  credential group retains a private same-scan request cache, and all scans
  continue sharing the client's per-credential request gate and token cache.
- Canceling a waiter during another scan's App token refresh leaves the
  original scan running; malformed App metadata/tokens and unsafe request IDs
  remain fail-closed without exposing secrets.

Repeated after the final production edits: `go test ./internal/github`,
`go test -race ./internal/github`, and `golangci-lint run ./internal/github/...`
all pass. The implementation author changed only production Go files in
`internal/github` and this evidence document.

## Final isolation cases

The API/test author recorded two further RED scenarios in
`docs/tdd-github-final-red.txt` before these final production changes:

1. Cross-credential contradictions are reconciled from internal per-scan
   observations, including archived/fork repositories skipped from returned
   catalogs. Every source participating in a contradiction fails without
   repositories, while unrelated sources remain healthy. This keeps the public
   Result API unchanged and requires no extra metadata requests for skipped
   repositories.
2. A live waiter whose App token-exchange leader was canceled retries the
   exchange under its own context instead of inheriting the leader's
   cancellation. The leader's own cancellation remains observable.

The final independent tests are in `internal/github/final_regression_test.go`.
After these corrections the entire GitHub suite passes, the race run passes,
and golangci-lint reports zero issues. All five acceptance file hashes match
those frozen by the independent test author, including the documented
fixed-clock exception. No additional dependencies were introduced.
