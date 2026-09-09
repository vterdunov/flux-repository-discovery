# CLI acceptance contract

Independent API and tests completed on 2026-09-09 before CLI implementation.

`cli.Run(context, args, Options)` returns an exit code and writes to explicit stdout/stderr. The executable only supplies runtime dependencies and OS signal cancellation. `ServeOptions` passes the normalized declared config, credentials path, and documented server env values to the runtime constructor; it preserves the declared/effective distinction.

Contracts covered:

- `version`, `validate`, `dry-run`, `serve`; unknown flags and positionals are argument errors.
- Explicit config and credentials path flags override env; missing required arguments or invalid local config return 2.
- Validate is local. CLI dry-run only calls the service, preserves caller context/deadline, sends `{"config":...}` with no local server env or secrets, and never calls GitHub.
- Complete JSON reports survive without lost fields or stdout decoration. Human reports expose config values, renamed objects, added/removed repositories, endpoint removal/404, and active server overrides.
- Successful checks return 0; `--detailed-exitcode` returns 3 when `hasChanges` is true. HTTP/server failures, cancellation, malformed or unsupported reports return 1 with stderr and no success stdout.
- Serve delegates runtime startup and forwards context, credentials selection and supported overrides.

RED:

```sh
mise exec -- go test ./internal/cli
```

Exit 1. Package compiles against the stub. All positive cases fail because `Run` unconditionally returns 1 and produces no output or request. Invalid argument cases fail because exit code is not 2 and stderr is empty. Cancellation case fails because no request was made.

Frozen SHA256:

```text
9a9ac149698ae5d9374bc35481754e27770a65a8904ccddcdd31cf6da5b94e6e  internal/cli/cli_test.go
```

Expectations were constructed manually from the original requirements and the previously agreed service report schema. The completed implementation plan has since been removed; current behavior is documented in the [README](../README.md). At this handoff, implementation was authorized to begin; expectation changes require an independently documented correction.
