# Independent runtime review regressions

Baseline service and HTTP acceptance tests independently passed before this review. Existing frozen files are unchanged.

Two requirement gaps were reproduced before fixes:

1. A Go caller can construct `topics.all: []` with a valid `topics.any`. Service normalization serializes optional fields before semantic validation, erasing the empty condition. Both constructor and dry-run accept a configuration that the file/HTTP parser correctly rejects; dry-run even scans GitHub. Validate typed input before optional serialization can remove evidence of an invalid condition.
2. When source B completes and source A exhausts the whole-cycle deadline, the service discards B's completed result after observing the canceled shared context. Source isolation in PLAN section 6 applies to timeout failures too. The Scanner contract already requires complete results or per-source errors and cancellation support, so a completed successful source can still publish independently.

A passing check also confirms that arbitrary server environment values cannot enter the report's overrides.

RED:

```sh
mise exec -- go test ./internal/service ./internal/httpapi
```

Exit 1. `TestTypedConfigCannotLoseInvalidEmptyConditionDuringNormalization` accepts both invalid constructor and dry-run inputs and records one network scan. `TestSourceTimeoutDoesNotDiscardAnotherCompletedSource` returns source_unavailable for B although B completed before timeout. HTTP baseline remains GREEN.

Frozen new regression SHA256:

```text
01327c5f129810c0e133fec7ef3632c097a2393a9f1e4c3e21962a1960c65651  internal/service/regression_test.go
```

No HTTP-specific requirement violation was found in this review of strict envelopes, error status/body separation, middleware routing, no-store headers and response size enforcement. Live Flux compatibility remains a separate check.
