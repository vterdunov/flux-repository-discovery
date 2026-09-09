# Permanent actual-binary CLI integration test

The independent API/test author added `integration/cli/cli_test.go` to replace the ignored Python smoke harness. No application implementation changes were required. Existing frozen unit/acceptance tests and the old Python harness were not modified by this author.

## Test boundary

Build constraint: `integration && (darwin || linux)`. The test uses only the Go standard library, including `encoding/json/v2` and `encoding/json/jsontext`; it imports no project runtime packages and declares independent wire DTOs.

One scenario builds the current command source with `go build -race` into a private `t.TempDir`. It never executes `bin/flux-repository-discovery`. A test-specific version is injected at build time and verified through the actual `version` command, so an unrelated stale workspace binary cannot satisfy the scenario. The Go executable is resolved from the mise-provided PATH. Each `-count` iteration gets its own fresh build and private fixture directory.

The scenario verifies:

- actual `version` and example `validate` successful stdout/exit codes;
- private mode-0600 config, candidate, and credentials files, a fake token, zero GitHub sources and filters;
- inherited `FRD_*` and proxy environment variables removed; HTTP transport also disables proxies;
- dynamic IPv4 localhost listener, bounded actual HTTP readiness and first published generation;
- health/readiness/status HTTP 200, JSON and no-store headers, valid revision, completed scan time and empty non-null source/filter maps;
- missing filter HTTP 404 with `not_found` and no inputs member;
- actual CLI dry-run exit 3, pure versioned JSON, complete declared and effective `scan.interval` replacement from `10m0s` to `5m0s`, correct base/candidate revisions and generation, empty filter/drift/override maps;
- status, scan timestamp, revision and generation unchanged after preview;
- SIGTERM exits successfully within the deadline, emits the shutdown log, leaves server stdout empty and exposes no fake token.

No live GitHub integration is claimed. All runtime HTTP communication is local to the temporary server.

## Bounded child lifecycle and diagnostics

`os/exec` continuously drains stdout and stderr into synchronized writers. JSON log records are parsed incrementally across write boundaries. The startup waiter uses captured state and notifications, followed by actual readiness/status checks; it never mixes descriptor readiness with a separately buffered line reader.

Each output stream retains at most 64 KiB. Oversized output, invalid/incomplete JSON logs and the fake token cause failures while draining continues. Error diagnostics are bounded and redact the fake token. Requests have a one-second client deadline and bounded bodies. Build, commands, startup, server lifetime and graceful shutdown have explicit deadlines. Every successfully started child is waited on. Cleanup requests SIGTERM, then uses Kill if needed and waits for the existing Wait goroutine; `WaitDelay` also bounds pipe-draining completion. Temporary fixtures are owned by `t.TempDir`.

## Sensitivity and verification

This is an independently authored regression harness for already implemented behavior, not a production change requiring a manufactured implementation RED.

A first run in the filesystem/network sandbox failed with `bind: operation not permitted`. Its captured stderr correctly explained the failure; it was not counted as an application defect. The authorized localhost run then passed.

A read-only Go test overlay changed only the candidate fixture from `5m` to `7m`, leaving the expected diff untouched. The test failed at the exact semantic assertion, printed bounded server diagnostics and completed child cleanup:

```text
--- FAIL: TestBinaryHTTPDryRunAndGracefulShutdown
preview must contain complete declared/effective interval diff:
Operation:"replace", Path:"scan.interval", Before:"10m0s", After:"7m0s"
FAIL
```

The overlay lived under `.cache/cli-binary-sensitivity`; no checked-in test or production file was modified for this negative control.

Normal checks:

```sh
MISE_TRUSTED_CONFIG_PATHS=$PWD mise exec -- golangci-lint run --build-tags=integration ./integration/cli/...
# 0 issues

MISE_TRUSTED_CONFIG_PATHS=$PWD mise exec -- go test -race -tags=integration -count=3 -timeout=2m ./integration/cli
# PASS, all 3 repetitions; both harness and child binary use the race detector
```

Root owns the permanent mise task wiring and broader final checks. Frozen new test SHA-256:

| File | SHA-256 |
| --- | --- |
| `integration/cli/cli_test.go` | `0a0562710e08ac50306f2a99511c08437c8fffdacfb64863fa21eac112388992` |
