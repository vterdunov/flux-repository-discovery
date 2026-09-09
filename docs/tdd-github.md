# GitHub API and tests before implementation

Author: separate API/test agent. Date: 2026-09-09.

This contract was designed from the original requirements and agreed with the root and the
configuration/discovery API author before implementation. No behavior has been
implemented in `internal/github/github.go` or `credentials.go`: these files
contain only declarations and compiling placeholders.

This is a historical handoff record. The completed implementation plan has since been removed; current behavior is documented in the [README](../README.md).

## Consumer API

```go
credentials, err := github.LoadCredentials(credentialsYAML, github.CredentialOptions{})
// Handle err, validate all config credential references using credentials.Names().
client, err := github.NewClient(credentials, github.Options{})
// Handle err. One result exists for every requested source.
results := client.Scan(ctx, cfg.Sources)
```

`Credentials` is an opaque immutable server registry with `Has` and sorted,
caller-owned `Names`. Loader references resolve env or files once; raw values and
untrusted provider errors must never escape through errors.

`Scan` returns `map[config.SourceName]Result`. Each Result contains either a
complete `[]discovery.Repository` or Err and zero repositories. Sources are
independent, matching requests share work within this scan, and consecutive
scans share tokens and rate-limit cooldown only. Repositories are ordered by ID.

`Options` injects an HTTP client, clock, and context-aware wait at external
boundaries. Production URLs remain fixed at `https://api.github.com`. Test
transport invokes `httptest.ResponseRecorder` directly so tests require neither
GitHub access nor loopback sockets.

The adapter completes unavailable topics through `/repos/{owner}/{name}`.
Installation owner identity comes from `/app/installations/{id}` even when
`/installation/repositories` is empty. JWTs are checked independently with RSA
signature verification and decoded claims, without using production signers.

## Cases frozen before implementation

- Strict credentials YAML, unknown/duplicate fields, exact env/file selection,
  malformed key and IDs, missing secrets, secret-safe provider errors.
- Authenticated organization pagination past 100 repositories and same-scan
  request sharing across named sources.
- Personal owner catalog merges public and authenticated private repositories,
  filters other owners, and deduplicates IDs. Private catalog failures cannot
  fall back to public output.
- Partial pagination, malformed JSON/envelopes, missing mandatory metadata,
  missing topics completion, archived/fork exclusion before metadata calls.
- Owner existence, isolated source failure, credential reference preflight,
  untrusted Link/redirect rejection, pagination cycle detection.
- Temporary retries, bounded attempt count, Retry-After/reset cooldown shared
  across scans, cancellation before and during requests.
- GitHub App RS256 signature and claims, installation account validation,
  cached installation token, concurrent token refresh, one refresh on 401.

## RED evidence

```sh
MISE_TRUSTED_CONFIG_PATHS=/Users/vterdunov/work/projects/vterdunov/flux-repository-discovery mise exec -- go test ./internal/github
```

Executed with Go 1.27.1, exit 1. Package and tests compile; positive cases fail
with `credential loading is not implemented`. Failure output is preserved in
`docs/tdd-github-red.txt`. Negative rejection tests currently pass because the
placeholder rejects everything; valid registry/catalog cases distinguish this
from an implementation. An initial harness attempted loopback sockets, which
the sandbox denied; the final harness is fully in memory and the recorded RED
is caused by absent behavior only.

SHA-256 for implementation handoff:

```text
6fea845109733d24b73bdcc9d4a9cce19e7453540330c6612638931e0036b63e  internal/github/app_test.go
6c5d85895bc547f99ba0b44eaa8a5c4bf2fd99698b51943f827d8c0f2c900513  internal/github/client_test.go
44aa2b4c89506e092144f75fe948255d8c58c9ac20ec3128297c62fd4fd6550e  internal/github/credentials_test.go
```

The implementation author must not change these acceptance tests. An incorrect
contract or test requires a concrete proposal and independent review by the
test author, recorded separately. Additional cases are welcome. This workspace
had no Git repository at handoff, so the evidence is file hashes instead of a
commit.

## Independent implementation review and regression RED

Initial implementation passed the frozen acceptance suite. The independent
test author verified all original hashes before inspecting production code.
Review identified observable integrity and isolation gaps; additional tests in
`internal/github/regression_test.go` reproduced them before corrective code:

- Mixed-case JSON aliases `id` and `ID` override identity under Go's JSON decoder.
- An archived/fork duplicate can be discarded before a conflicting active copy
  is detected, leaving a falsely trustworthy active result.
- An unexpected owner inside an owner-scoped organization/installation response
  was silently discarded and converted into successful empty output.
- Pagination could change visibility or page size, or skip an intermediate
  page, while returning success for an incomplete catalog.
- A far-future rate reset overflowed duration rounding into a negative wait.
- A sequential scan let one blocked credential consume the entire context
  deadline before an independent healthy credential was attempted.

The last test uses `testing/synctest` and distinct credentials. Per-credential
request serialization and shared cooldown remain required; independent
credentials must receive an opportunity to finish before the overall deadline.

```sh
mise exec -- go test ./internal/github -run 'Test(JSONField|SkippedRepository|ScopedOrganization|PaginationCannot|MalformedPagination|ExtremeRate|SlowCredential|MalformedApp|GitHubResponse)' -count=1
```

Exit 1, confirmed behavior failures. Output is saved in
`docs/tdd-github-regression-red.txt`. Malformed Link, malformed or expired App
token, installation count mismatch, untrusted error metadata, and independent
scan survival when another token waiter is canceled also have separate
regression cases; these passed during review.

Regression file hash after adding the passing cancellation scenario:

```text
8f35e32ca3f6fb446da8c485d904cc478d0a96643d5c640470bb59a3f70a83cd  internal/github/regression_test.go
```

## Independently approved clock correction

The root and implementation author both noticed a potential flaky assertion
in `TestTransientRetryAndRateLimitDelay`: it supplied a fake Wait but real Now,
then expected a full two-second wait even when scheduler time had elapsed.
The API/test author independently confirmed that the production behavior should
honor the remaining cooldown. The correction injects a fixed Now alongside the
existing fake Wait. No assertions, fixtures, retry count, or minimum cooldown
requirements changed. Production must not compensate for a faulty test clock.

```text
Before: 6c5d85895bc547f99ba0b44eaa8a5c4bf2fd99698b51943f827d8c0f2c900513
After:  fc75bc17cddb90a5d1867af840c259ce8dfac27840a8e38a970566d1ac522312
File:   internal/github/client_test.go
```

`TestTransientRetryAndRateLimitDelay` and the cancellation regression were
rerun successfully after this harness-only correction. The other two original
acceptance test files retain their original hashes.

## Independent GREEN verification

After the implementation author corrected the regression failures, the separate
test author reread the changes and executed:

```sh
mise exec -- go test -race ./internal/github -count=1
```

Exit 0. The original acceptance suite, independently approved fixed-clock
correction, and all regression cases passed under the race detector. Their
hashes match the recorded handoff versions. This verifies the local adapter
contract against in-memory HTTP fixtures, not live GitHub or Flux integration.

## Final two cross-scan integrity cases

Root review approved two remaining cases from the existing requirements. Both
were written by the independent API/test author in
`internal/github/final_regression_test.go` before corrective implementation:

1. One credential observes an active ID while another sees the same ID archived
   or forked. Skipping the second observation must not hide the conflict.
   Participating source catalogs must fail; an unrelated source remains healthy.
   Skipped repositories still trigger no topics/metadata requests.
2. A token exchange leader is canceled while another scan with a live context
   waits for that exchange. The live waiter must retry using its own context,
   rather than inherit a different scan's cancellation. The test first returns
   503, then holds the retry outside the HTTP gate so the waiter can join the
   exchange. `synctest.Wait` establishes durable blocking before cancellation.

```sh
mise exec -- go test ./internal/github -run 'Test(CrossCredentialSkipped|CanceledTokenRefreshLeader)' -count=1
```

Exit 1 for both behavior gaps; output is in `docs/tdd-github-final-red.txt`.
The test file is frozen for the independent implementation author:

```text
d0b96bade22fcdbd763b5c5d4be6796ba8f660492abdd8bac05912be1f9011c1  internal/github/final_regression_test.go
```

After the implementation author corrected both final cases, the independent
test author verified all five test file hashes and reran
`mise exec -- go test -race ./internal/github -count=1`: exit 0. The final
cross-credential reconciliation and canceled-leader retry code were reviewed.
This completes the bounded GitHub adapter review.
