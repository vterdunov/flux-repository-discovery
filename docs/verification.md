# V1 verification record

Completed on 2026-09-09. Runtime toolchain: Go 1.27.1 and golangci-lint 2.13.2, pinned by mise. The module has one external dependency: go.yaml.in/yaml/v3 v3.0.5.

## Final checks

| Check | Result |
| --- | --- |
| `mise run check` | PASS: all package tests, race detector, golangci-lint and native executable build |
| `golangci-lint run ./...` | 0 issues |
| `go mod tidy` | PASS; only YAML remains in the root module |
| Linux amd64 build with `CGO_ENABLED=0` | PASS |
| Config parser fuzz, 5 seconds, 2 workers | PASS, 185891 executions |
| Filter invariant fuzz, 5 seconds, 2 workers | PASS, 245679 executions |
| Actual binary HTTP/CLI/SIGTERM smoke | PASS |
| Actual service + Flux Operator v0.59.0 + Kubernetes envtest 1.36.2 | PASS |

The final full check completed after the GitHub integrity and cancellation fixes. The final independent GitHub race run also used `-count=1`. Native build output is `bin/flux-repository-discovery`; the cross-build is an ignored check artifact under `.cache`.

The two fuzz targets are:

```sh
mise exec -- go test ./internal/config -run '^$' \
  -fuzz '^FuzzDecodeDoesNotPanic$' -fuzztime=5s -parallel=2
mise exec -- go test ./internal/discovery -run '^$' \
  -fuzz '^FuzzEvaluateNeverPublishesArchivedOrForked$' -fuzztime=5s -parallel=2
```

These are bounded smoke fuzz runs, not a claim of exhaustive input coverage.

## Independent TDD

API/acceptance authors wrote public-behavior tests and minimal compiling stubs before implementation. RED failures and frozen SHA256 values were recorded before implementation handoffs:

- [Configuration and discovery](tdd-config-discovery.md), followed by [independent regressions](tdd-config-discovery-review.md).
- [GitHub API](tdd-github.md), including two independently authored integrity/isolation regression rounds and [GREEN evidence](tdd-github-green.md).
- [Service and HTTP](tdd-service-http.md), followed by [runtime regressions](tdd-service-review.md).
- [CLI](tdd-cli.md), followed by [partial-report regressions](tdd-cli-review.md).
- [Runtime startup and shutdown](tdd-app.md).
- [Complete CLI/HTTP/GitHub flow](tdd-integration.md).
- [Structured logging](tdd-logging.md).

The implementation authors did not edit acceptance expectations. Seventeen frozen test files were verified against their recorded hashes at completion. Three test harness corrections were independently approved and documented: two equivalent context-aware httptest constructors in [test maintenance](tdd-test-maintenance.md), and a fixed clock paired with a fake wait in the GitHub Retry-After test. The latter removes scheduler-dependent flakiness without changing delay or retry assertions.

The workspace had no Git repository, so handoffs were recorded with file hashes and separate evidence documents rather than commits.

## Integration boundaries

The component integration uses real CLI, HTTP handlers, service and GitHub client against an in-memory GitHub transport. It covers 120 repositories across pages, full mass-removal preview, rename drift, immutability after successful/failed dry-run, failed second-page publication, and successful empty recovery.

GitHub authentication tests use synthetic PATs and generated RSA keys. Real GitHub accounts or credentials were not exercised. Private access scope, organization policy and network behavior must therefore be supplied and verified by the operator's environment.

The [independent final review](independent-final-review.md) records actual executable startup, status/readiness, dry-run and SIGTERM behavior. Its temporary configuration uses zero GitHub sources and a dummy credential, so it makes no GitHub calls.

The [Flux compatibility record](flux-compatibility.md) uses the actual service HTTP handler, real upstream reconcilers and an isolated Kubernetes API server. It verifies resource creation, preservation on 503, stable UID on rename, deletion after topic removal and successful empty results. Source-controller cloning, artifact readiness and deployment are outside that integration test.

The future Google Workspace/OIDC login remains intentionally tracked in [TODO.md](TODO.md), as agreed for the unauthenticated HTTP API in v1.

## JSON v2 migration

The subsequent [JSON v2 migration](jsonv2-migration.md), also verified on 2026-09-09, adds four independently authored frozen acceptance files. The original 17 files remain unchanged. The migration passed all package tests, race detection, lint, native and Linux amd64 builds, a bounded configuration fuzz run and a fresh real Flux integration run. See the migration record for the changed input validation and preserved output contract; the earlier fuzz counts above belong to the original implementation verification.

## Prepared configuration refactor

The later user-approved prepared configuration refactor was completed on 2026-09-09. Configuration parsing now returns immutable prepared Config/Filter values; runtime binding belongs to config, and service accepts Runtime. HTTP uses direct JSON v2 request decoding. The manual envelope tokenizer, service JSON freeze roundtrip and repeated filter validation/regexp compilation were removed. Duplicate JSON keys inside candidates intentionally changed from HTTP 422 to 400; other configuration errors remain 422.

Independent authors adapted old Go API fixtures, retained behavioral expectations except the explicitly agreed classification/ownership changes, and froze new tests before implementation. Their [config/discovery](tdd-parsed-config.md) and [consumer](tdd-parsed-consumers.md) records explain the adaptations, behavioral RED and final reviews. Two further RED regressions preserved UTF-8 and canonical size guarantees in the owning config boundary. All 19 recorded test/fixture hashes matched after implementation. Historical hashes in earlier records describe their respective handoffs.

| Final refactor check | Result |
| --- | --- |
| `mise run check` | PASS: package tests, race detector, native build; 0 lint issues |
| Linux amd64 with `CGO_ENABLED=0` | PASS |
| Final config fuzz, 5 seconds, 2 workers | PASS, 96730 executions |
| Filter fuzz, 5 seconds, 2 workers | PASS, 244029 executions |
| Actual service + Flux Operator 0.59.0 + envtest 1.36.2 | PASS, controller package 7.680 seconds |
| Flux bridge lint with its build tag | PASS, 0 issues |
| Actual native binary HTTP/CLI/SIGTERM smoke | PASS, 10 consecutive runs after fixing the independent harness's log buffering |

The GitHub adapter and its tests, upstream Flux scenario assertions, dependency files and pinned toolchain stayed unchanged during this refactor. GitHub credentials in tests remain synthetic. The Flux scenario uses isolated local Kubernetes API processes, and the executable smoke uses zero GitHub sources. Neither test accesses an existing cluster or verifies live GitHub permissions.

## Permanent Go binary test

The disposable `.cache/manual-smoke.py` has been replaced by [integration/cli/cli_test.go](../integration/cli/cli_test.go) and removed. An independent test author defined the executable contract; application code and existing tests needed no changes. The new test uses only the standard library, builds current sources into `t.TempDir`, and runs the resulting real server and CLI processes. Both the test and child binary use the race detector.

```sh
mise run test-cli
mise run check
```

`test-cli` selects the `integration` build tag and disables test caching. It supports Linux/macOS and needs a localhost listener. `check` now includes this task, and lint includes its build tag. The fixture uses no GitHub sources, a fake credential and isolated application/proxy environment settings. It verifies version and example validation, HTTP health/readiness/status, the first published generation, missing-filter errors, the complete declared/effective dry-run diff, unchanged publication after preview, and successful SIGTERM shutdown. Output and waits are bounded; child processes and temporary fixtures are cleaned up.

The independent author's three consecutive race runs passed. A controlled candidate change from 5m to 7m with unchanged expectations failed the diff assertion, confirming sensitivity. The final full `mise run check` passed, including a fresh binary test in 4.663 seconds; lint reported 0 issues. All 58 baseline application, existing test, dependency, lint-configuration and Flux-fixture files retained their hashes. The new test also retained its independently frozen SHA-256. See the [test contract and evidence](tdd-cli-binary.md).
