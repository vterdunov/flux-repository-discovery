# Service and HTTP acceptance contract

Independent API/test stage completed on 2026-09-09 before service or HTTP implementation. Tests consume public package methods and HTTP responses. Their expectations derive from the original HTTP, publication, and dry-run requirements. The completed implementation plan has been removed; current behavior is documented in the [README](../README.md).

## Contract decisions

- `service.New` freezes declared config and documented server env, validates credential references, and does not perform network I/O.
- `Scan` collects one complete attempt and publishes independent successful filters plus dependent source errors atomically. It returns an error if an attempt contains failures. `Inputs` never returns stale fallback after failure or mutable generation storage.
- `Run` starts immediately, waits interval after completion, cancels on shutdown, and caps a whole scan by its timeout. `Now` controls freshness; `testing/synctest` controls timers without real sleeps.
- `DryRun` validates before network, uses current server limits and overrides, scans distinct owner/credential scopes once, returns complete config/effective/output differences, and separates drift. No active state changes on success, failure or cancellation.
- Error codes are typed values: `not_ready`, `not_found`, `source_unavailable`, `stale`, `result_limit`, `invalid_config`, `busy`. Errors expose sanitized context instead of arbitrary upstream messages.
- HTTP adds `invalid_request` and `request_too_large`; malformed/ambiguous request envelopes return 400, semantic candidate errors including duplicate candidate keys return 422. Dry-run receives `{"config":...}`.
- Shared, inputs-only and operator-only middleware hooks permit separate future authorization policies. Readiness means the runtime loop is running, independent of upstream availability.

## RED evidence

```sh
mise exec -- go test ./internal/service ./internal/httpapi
```

Result: exit 1, both packages compile. Service failures include `not implemented`, missing typed error codes, constructor accepting unknown credentials, and Run exiting before its initial scan. HTTP returns stub 501 instead of required 200/400/404/413/422/503 and does not apply middleware. Tests using channels detect premature stub return explicitly and do not deadlock.

Frozen acceptance SHA256:

```text
46e3ec85fb492195e5b2fa65b68d88026b14419e2472363c4cdc85719744e4b6  internal/service/service_test.go
4fadab5c9c883bbb93ab43efb318ac5e07e22fa281f05aca6fb263872e5699fc  internal/httpapi/httpapi_test.go
```

Implementation may now begin. Changes to these expectations require a separately documented requirement correction approved by the independent API/test author.
