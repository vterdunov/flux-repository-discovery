# Prepared configuration API: independent TDD contract

The independent API/test author defined this contract before production changes to config and discovery. The user approved immutable prepared configuration, configuration-owned preparation, and removal of repeated validation. Existing config/discovery assertions were preserved while their fixtures and accessors were adapted to this contract.

## Frozen public API

- `Document` is the former serializable `Config` DTO; `FilterSpec` is the former mutable filter DTO. Source, Rule, Topics, Server and Scan keep their wire shape.
- `Decode([]byte) (Config, error)` accepts the existing strict YAML/JSON document and defaults omitted settings. `Parse(Document) (Config, error)` prepares a complete typed document; explicit zero intervals/timeouts and empty listen remain errors.
- `Config` has private state. `IsZero`, `Document`, `Server`, `Scan`, `Sources`, and `Filters` expose copies or immutable prepared values. Document preserves user array order and casing.
- `Filter` has private prepared state. `IsZero`, `Sources() []SourceName`, `Repositories() []string`, and `Matches(owner, name string, topics []string) bool` expose safe operations. Repositories returns explicit include references followed by exclude references in declaration order, retaining duplicates/case; discovery owns normalized unmatched diagnostics.
- `Prepare(Config, map[string]string, []CredentialName) (Runtime, error)` binds supported environment settings and credential names with no secret I/O. Unknown credentials fail here, not in document parsing.
- `Runtime` has private state. `IsZero`, `Declared() Config`, `Effective() Config`, `Overrides() map[string]string`, and `PrepareCandidate(Config) (Runtime, error)` preserve frozen environment and credential context. Candidate preparation never mutates the active runtime.
- Zero Config/Runtime values fail preparation. Zero Filter fails `discovery.Evaluate` without successful empty or partial output. Zero Runtime must fail `service.New` (independent consumer tests).
- Service accepts Runtime. Discovery accepts Filter. Neither repeats config validation or regexp compilation.

## Acceptance coverage

New blackbox tests mutate inbound/outbound source maps, owner slices, filter maps, source slices, rule slices, repository slices, Topics pointers and their all/any slices. They verify matching still honors the original rules and exclusions, document array order/case remain intact, runtime bindings are frozen, unsupported environment values are not retained, failed preparations yield zero values, and concurrent snapshot consumers cannot mutate shared prepared state. Existing discovery tests retain the full rule truth table, exclusions, catalog integrity, ordering, genuine empty result, and fuzz invariants.

## RED evidence

Production behavior was unchanged when RED was captured. Since the approved API intentionally cannot compile against the old public DTO type, `.cache/parsed-config-red/overlay.json` supplied minimal compile-only declarations and methods returning explicit not-implemented errors; no parsing, cloning, matching, env application or credential-resolution behavior was implemented in those stubs. The test packages compiled, then failed behavioral assertions. `docs/tdd-parsed-config-red.txt` contains the actual output.

Command:

```sh
MISE_TRUSTED_CONFIG_PATHS=$PWD mise exec -- go test -overlay=.cache/parsed-config-red/overlay.json ./internal/config ./internal/discovery -run 'Test(Parse|Prepared|Runtime|ZeroPrepared|EvaluatePrepared|EvaluateRejectsUninitialized)' -count=1
```

The stub implementation must not be used for GREEN. Run normal `go test` after production implementation. New tests and mechanically adapted baseline config/discovery tests are frozen at this point:

| Test file | SHA-256 |
| --- | --- |
| `internal/config/prepared_test.go` | `c0d76ac3c8c0a867342fe019a13cbbbf7045953f70e4229e096e3ce732c47fbf` |
| `internal/discovery/prepared_test.go` | `c99a8ffb257afe30b2f12985f83a02e0793ff2f44a74d5b105e2cec4e291629a` |
| `internal/config/config_test.go` | `bcc77129314df042f7f22642e5d254f3c3293df8a48ded0e56b2b2bcc0cd3657` |
| `internal/config/regression_test.go` | `ba98e41c338cceabe31314617aa3afcc15d5009bba9ab61d39ec4588ca50e015` |
| `internal/discovery/discovery_test.go` | `360c90f1f0c19047748816ad1d5430f56fa4293ac2800972d29b978f1e97f425` |
| `internal/discovery/regression_test.go` | `6cd1ea923224debbe2fd4898d73bc839b04da91cd7eebdb5ec891db16d6c96af` |

## Independent review regressions

Review compared config/discovery production with the pre-refactor snapshot. Two additional invariants were independently reproduced before fixes, without changing any frozen tests:

1. A typed listen address containing byte `0xff` passed `net.SplitHostPort`, creating a Config whose JSON serialization failed. The same byte in `FRD_LISTEN` created an invalid effective configuration. `prepared_regression_test.go` requires both config-owned boundaries to reject with their field/env path and return zero values. Actual behavioral RED: `docs/tdd-parsed-config-regression-red.txt`.
2. Removing the old service freeze/decode roundtrip accidentally removed its normalized JSON size bound. A valid typed document serialized to 1,092,150 bytes, exceeding MaxBytes 1,048,576. A compact YAML document of 1,040,077 bytes could similarly expand beyond the supported JSON transport limit. After root approved preserving this existing invariant in config.Parse, `prepared_size_test.go` froze under-limit acceptance plus oversize typed/canonical expansion rejection. Actual behavioral RED: `docs/tdd-parsed-config-size-red.txt`. This does not introduce an effective-env size limit that the old runtime did not enforce.

Pre-fix probe output: `docs/tdd-parsed-config-review-probes.txt`. Probes also confirmed a valid document with zero sources/filters remains initialized, binds to an initialized Runtime, and serializes with `{}` maps.

| Additional test file | SHA-256 |
| --- | --- |
| `internal/config/prepared_regression_test.go` | `b6b50253a0f9894c0ef7f612c7170ae45d61f26caf09009b1df42c30a038e76f` |
| `internal/config/prepared_size_test.go` | `4d765bec597091bb48e1115b4737ea643a089a4739877a4df66d4cbc5fd5b2d9` |

## Independent final review and GREEN

After production fixes, the independent test author ran:

```sh
MISE_TRUSTED_CONFIG_PATHS=$PWD mise exec -- go test -race ./internal/config ./internal/discovery
```

Both packages passed, including all original assertions, prepared API acceptance, UTF-8 regressions, and canonical size regressions. All eight SHA-256 entries above were checked against the final test files and matched.

Read-only review against `.cache/parsed-config-before` confirmed:

- All DTO maps, source owners, filter source/rule/repository slices, and nested Topics slices are snapshotted. No mutable regexp pointer crosses the prepared Filter API.
- Effective configuration copies the scalar container and safely shares private immutable filter state. Runtime bindings copy supported env values and credential names, and candidate preparation cannot mutate active values.
- Zero Config/Runtime/Filter boundaries fail closed. Valid empty configuration remains distinct from an uninitialized value and preserves `{}` maps.
- Rule matching, exclusion priority, source metadata integrity, ordering and unmatched diagnostics preserve the existing behavior.
- User regexp compilation exists only in config.prepareRule during Parse. Runtime binding, candidate binding, service and discovery do not parse config or compile regexp again.
- Canonical size protection is a single encoding/length check in config.Parse; the former service JSON encode/decode freeze is gone. No new effective-environment size restriction was introduced.

No unresolved config/discovery findings remain in the reviewed scope.
