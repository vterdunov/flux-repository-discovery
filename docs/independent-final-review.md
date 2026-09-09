# Independent final review

Reviewed on 2026-09-09 by the API/acceptance-test author without modifying production code.

## Reviewed behavior

- App startup loads and validates server credentials before binding, applies documented listen overrides, starts the HTTP server and scan loop, and gives requests the runtime context.
- Cancellation stops active GitHub work, background scanning and HTTP handlers; normal SIGTERM is successful. Bind errors remain visible. Credentials and file reads are bounded.
- CLI rejects incomplete successful reports before stdout: observation time, supported diff kind, nonnegative counts, complete proposed inputs and count arithmetic are checked. Unavailable drift is explicitly exempt from claiming a complete diff.
- README and example configs match the implemented flags, env precedence, filter semantics, limits, error behavior and separate server credentials. The Flux example explicitly selects a consumer-owned Git branch and keeps clone credentials separate from discovery credentials.
- Structured logging initially lacked source/filter details. The independently authored RED in `tdd-logging.md` was fixed: source/generation/duration/count, safe typed GitHub error/status/request ID, and filter/generation/count or error are now logged. Wrapped arbitrary error text is excluded. Source event duration describes the complete scan attempt.

No remaining contract violation was found in these reviewed paths.

## Rechecked acceptance and race

```sh
mise exec -- go test -race ./internal/app ./internal/cli ./internal/service ./internal/httpapi ./internal/integration
```

All five packages passed after the logging fix. The baseline acceptance hashes were audited: config, discovery, service, CLI and app match their original freezes. HTTP and component integration match the independently documented noctx-only changes in `tdd-test-maintenance.md`. The logging contract still matches its original freeze.

## Actual executable smoke

The existing `bin/flux-repository-discovery` executable, version `dev`, was exercised separately from package tests. Temporary configuration used an OS-assigned loopback port, zero GitHub sources/filters, and an explicitly fake token. No GitHub request was needed.

The default sandbox denied loopback binding with `operation not permitted`. The same bounded smoke then ran with approved execution escalation and passed:

```text
example validation: exit 0
binary version: dev
/healthz: HTTP 200
/readyz: HTTP 200
/api/v1/status: HTTP 200
/inputs/missing: HTTP 404, structured error without inputs
CLI dry-run: exit 3, pure versioned JSON, scan.interval diff, zero GitHub sources
SIGTERM: exit 0, server stopped, no credential in logs
temporary smoke files removed; child process reaped
```

All HTTP checks also required `Cache-Control: no-store`. The dry-run changed the candidate interval to 5m and reported the structural change through the actual CLI/HTTP path. Temporary config and credential-reference files were deleted by the smoke harness, and its server process was waited for after SIGTERM. The original disposable Python harness has since been removed and replaced by the permanent [Go binary test](../integration/cli/cli_test.go), run with `mise run test-cli` and included in `mise run check`. Its independent verification is recorded in [tdd-cli-binary.md](tdd-cli-binary.md).

This executable smoke checks thin main/flag/signal wiring. GitHub pagination and source failures are checked by independent component tests; real Flux pruning behavior has its separate evidence and limits in `flux-compatibility.md`.
