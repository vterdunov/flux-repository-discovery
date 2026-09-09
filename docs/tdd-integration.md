# Component integration acceptance

Independent test authored before the CLI and HTTP implementations on 2026-09-09. It connects the real CLI, HTTP handler, service, GitHub client, config decoder and credentials registry. Both HTTP boundaries use in-memory transports, with no real sockets, GitHub credentials, or external services.

The manually defined workflow covers 120 repositories over two pages; startup 503; complete initial output; candidate reducing results to 3 with all 117 removals; a rename tracked as drift by stable ID; one shared fresh catalog for current/candidate; unmatched explicit names; complete pure JSON and detailed exit code; successful and failed previews preserving active state; page-two failure changing published output to 503; recovery to successful empty `inputs: []`; and credentials absent from operator requests/reports.

RED:

```sh
mise exec -- go test ./internal/integration
```

Exit 1, compile succeeds. The first HTTP check returns stub 501 instead of startup 503. Frozen SHA256:

```text
fda8b54a5cd359ced9d0b9a85b1bb5771c794e7a245da15ab55a5a46e2faeeaa  internal/integration/flow_test.go
```

This test does not claim live Flux Operator integration. The live controller's prune behavior still requires the separately specified isolated external integration check.
