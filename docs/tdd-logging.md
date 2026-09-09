# Structured scan logging acceptance

Independent final review found a remaining PLAN section 9 observability gap: aggregate scan logs did not identify failed sources, GitHub request IDs or individual filter counts.

A small independent JSON-slog contract test was added before the fix. A scan containing one wrapped typed GitHub error and one successful source must log:

- Source identity, generation and duration for each source; successful catalog count.
- Typed GitHub error_code, github_status and github_request_id for a source failure.
- Filter identity and generation with either count or error_code.
- No arbitrary wrapper error text, which can contain credentials.

The duration can describe the scan attempt at this existing Scanner boundary; it does not claim per-request tracing or a new metrics subsystem.

RED:

```sh
mise exec -- go test ./internal/service -run '^TestScanLogsSafeSourceDiagnosticsAndFilterCounts$'
```

Exit 1. Only the aggregate warning appears, so all four source/filter diagnostic checks fail. Secret exclusion already passes. Frozen SHA256:

```text
33e670b273ed1725a1b0705ef30e9ba708b09ec074ba12cd5ec42315fde78ce5  internal/service/logging_test.go
```
