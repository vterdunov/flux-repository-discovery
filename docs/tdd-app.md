# Runtime lifecycle acceptance

Independent API and acceptance tests completed before application wiring on 2026-09-09.

`app.Run(context, cli.ServeOptions, app.Options)` owns credential loading, GitHub and service construction, listener binding, background scanning, HTTP serving and shutdown. OS file/env reads, GitHub transport, listener and shutdown timeout are injectable at their actual I/O boundaries. Normal cancellation returns nil after the background loop and in-flight handlers terminate.

Tests use a real standard-library HTTP server over an injected `net.Pipe` listener. No sockets, real credentials, GitHub network or wall-clock sleeps are involved. `testing/synctest` makes startup and shutdown timing deterministic.

Covered contracts: server-only credential resolution; effective listen env; listener binding before scans; immediate scan with initial inputs 503 and running readiness; successful empty response; active dry-run GitHub context canceled during shutdown before the shutdown deadline; clean cancellation; bind failure; invalid credentials failing before bind without leaking inlined secrets.

RED:

```sh
mise exec -- go test ./internal/app
```

Exit 1. Package compiles. Runtime stub returns `not implemented` before the first scan and does not preserve the injected listener error. Frozen SHA256:

```text
1a512215e788e8a2af29340314e7f3a06bc7c7da7c51f6d58ab7597af08eaf59  internal/app/app_test.go
```
