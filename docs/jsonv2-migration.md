# JSON v2 migration

Completed on 2026-09-09 using the pinned Go 1.27.1 toolchain. Runtime JSON imports now use `encoding/json/v2` and `encoding/json/jsontext`. No new dependency or experiment flag was added. Configuration syntax, endpoints and report version are unchanged.

Historical record: the later [prepared configuration refactor](verification.md#prepared-configuration-refactor) replaces the envelope tokenizer described below with direct JSON v2 decoding. Repeated JSON fields inside a dry-run candidate now return 400, matching other JSON decoding errors. Content errors still return 422. The original migration evidence remains below.

## Output compatibility

[internal/jsonwire/marshal.go](../internal/jsonwire/marshal.go) centralizes explicit output options shared by HTTP, CLI reports, configuration revisions, preview source identities and response-size checks:

- Deterministic map ordering preserves configuration hashes across map insertion orders.
- Existing HTML/JavaScript escaping and raw string representation are preserved.
- Nil diagnostic collections remain `null`; successfully evaluated empty input collections remain `[]`. Empty override maps remain `{}`.
- Durations remain strings; zero counts, zero generation and false flags remain explicit.

Raw diff values use `jsontext.Value`. Their `before` and `after` fields use `omitzero`, preserving explicit `{}`, `[]`, `false` and `0` under v2's different `omitempty` semantics. The fixed pre-migration revision fixture, including a regexp with `<` and `&`, still hashes to `4d35272db8411a2084633b82af16f6dd2a004696f5ce1ac95db4cb857ec876f2`.

## Input validation

CLI reports and GitHub data use strict v2 decoding. Duplicate JSON names and invalid UTF-8 are rejected. The GitHub catalog validator retains the existing depth bound and case-alias rejection, including `id` alongside `ID`; unrelated legitimate GitHub fields remain accepted. Missing mandatory metadata still requires successful completion before publication.

The dry-run envelope uses a `jsontext` decoder. It rejects malformed JSON and invalid UTF-8 with HTTP 400 before scanning. To preserve existing error classification, it permits duplicate names only during tokenization: repeated outer `config` names are rejected there with 400, while duplicate candidate fields reach the existing configuration validator and produce 422. No duplicate configuration is accepted. The candidate value is cloned before advancing the decoder because raw decoder values reference reusable storage.

File configuration and candidate semantic validation continue to share the strict YAML/JSON configuration parser. Standard-library JSON v1 imports remain only in independent tests and the Flux test harness, so their consumer expectations are not coupled to the migrated writer.

## Verification

The independent API test author wrote and froze four new acceptance files before production changes. RED exposed malformed reports accepted by the CLI and incorrect HTTP classification of invalid UTF-8. Existing tests were not changed. Requirements, RED output and SHA-256 values are in [tdd-jsonv2.md](tdd-jsonv2.md).

| Check | Result |
| --- | --- |
| `mise run check` | PASS: package tests, race detector, native executable build; golangci-lint reports 0 issues |
| Linux amd64 build, `CGO_ENABLED=0` | PASS |
| Configuration fuzz, 5 seconds, 2 workers | PASS, 23347 executions |
| Real service + Flux Operator 0.59.0 + Kubernetes envtest 1.36.2 | PASS using the unchanged consumer fixture |

The fuzz run is bounded input exploration, not exhaustive coverage. The [Flux integration](flux-compatibility.md) confirms creation, preservation on 503, stable identity on rename, removal after losing a topic and valid empty arrays. It uses local envtest and injected GitHub catalogs; it does not access a working Kubernetes cluster or live GitHub credentials. No performance claim is made by this migration.
