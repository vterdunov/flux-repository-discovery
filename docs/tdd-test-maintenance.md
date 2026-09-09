# Independently approved mechanical test maintenance

2026-09-09: golangci-lint `noctx` reported `httptest.NewRequest` in two frozen helper functions. The independent test author replaced each with `httptest.NewRequestWithContext(context.Background(), ...)`. This preserves the previous background context and every request/response expectation. No test was weakened or aligned to implementation behavior.

Validation: `mise exec -- go test ./internal/httpapi ./internal/integration` passes after the mechanical correction.

SHA256 before and after:

```text
internal/httpapi/httpapi_test.go
before 4fadab5c9c883bbb93ab43efb318ac5e07e22fa281f05aca6fb263872e5699fc
after  3962a07d934721f32692aca9a96355445ffeb76e258dfa7f104e565dbafaedd1

internal/integration/flow_test.go
before fda8b54a5cd359ced9d0b9a85b1bb5771c794e7a245da15ab55a5a46e2faeeaa
after  66585fb98f4ef359ec99e8e6ce0f9229bcd0550016ab47fb309d8b28759a2341
```

Historical RED evidence retains the original freeze hashes. This document records the exact authorized transition for final audit.
