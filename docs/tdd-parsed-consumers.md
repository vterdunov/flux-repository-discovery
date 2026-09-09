# Prepared configuration: independent consumer tests

These tests and API adaptations were authored by the independent consumer API/test agent before the prepared configuration implementation. Root owns production code. Existing fixtures retain their assertions except the explicitly approved HTTP duplicate-field classification and checks relocated to the new config boundary.

## Public API exercised

- Mutable fixtures are `config.Document` and `config.FilterSpec`; `config.Parse(Document)` returns prepared `config.Config`.
- `config.Prepare(Config, env, credentialNames)` returns prepared `config.Runtime` before service construction.
- `service.New(Runtime, Scanner, Options)` accepts prepared state. Service options no longer carry environment or credential references.
- Dry-run accepts a prepared `Config`. Static document errors fail in `config.Parse`; candidate credential references are resolved using the active runtime context.
- CLI and app test fixtures use prepared values and accessors. The real Flux bridge constructs Parse -> Prepare -> New; its scenario definitions and the upstream controller contract test are unchanged.

## Existing expectation relocations

1. `TestTypedConfigCannotLoseInvalidEmptyConditionDuringNormalization`: an explicitly present empty `topics.all` must fail at `config.Parse`, with that field in the error, before a prepared value can enter either service construction or preview. The old duplicate service-level checks are replaced by the owning parser check. Parsing has no Scanner parameter.
2. `TestDryRunWithoutGenerationAndValidationBeforeNetwork`: an unknown filter source fails in `config.Parse` and scanner calls remain unchanged. Unknown candidate credentials still produce service `invalid_config` before any new scan, because reference resolution needs active runtime context.
3. `TestConstructorFreezesConfigAndRejectsUnknownCredentials`: startup credential references fail in `config.Prepare` before service construction; the existing document/environment alias-mutation and revision assertions remain.
4. `TestDryRunHTTPValidationAndCompleteReport`: duplicate candidate JSON fields now return HTTP 400 `invalid_request`, as explicitly approved. All other status codes, report contents, secret handling, and no-scan checks remain.

## New acceptance coverage

- A zero Runtime cannot construct a service and cannot scan.
- A zero Config cannot preview, produce a report, scan, or change the published generation.
- Original documents, returned document/source/filter copies, environment maps, credential-name slices, and returned override maps cannot change service sources, filter selection, revisions, report overrides, or published data.
- Root, deeply nested, and escaped duplicate JSON keys return HTTP 400 `invalid_request`, with no inputs, `no-store`, and no scans.
- Invalid regexp, source reference, credential reference, and duration remain HTTP 422 `invalid_config` before scans.
- A valid server environment override cannot hide an invalid declared candidate duration.

## RED evidence

Before any consumer test adaptation or HTTP implementation change:

```text
MISE_TRUSTED_CONFIG_PATHS=$PWD mise exec -- go test ./internal/httpapi -run TestParsedConfig -count=1
--- FAIL: TestParsedConfigJSONDuplicatesAreRequestErrorsBeforeScanning
    candidate_root: HTTP 422, want 400: invalid_config (version: duplicate key)
    deep_duplicate: HTTP 422, want 400: invalid_config (filters.apps.include[0].nameRegex: duplicate key)
    escaped_deep_duplicate: HTTP 422, want 400: invalid_config (filters.apps.include[0].nameRegex: duplicate key)
FAIL
```

The accompanying content-error cases passed with their required HTTP 422 outcomes. Root was informed of this behavioral RED before replacing the manual envelope tokenizer.

After all public API adaptations and new consumer acceptance tests were written, the unimplemented prepared API produced compilation RED:

```text
MISE_TRUSTED_CONFIG_PATHS=$PWD mise exec -- go test ./internal/service ./internal/httpapi ./internal/cli ./internal/app ./internal/integration
undefined: config.Document
undefined: config.FilterSpec
undefined: config.Runtime
undefined: config.Prepare
invalid operation: cannot call cfg.Scan
invalid operation: cannot call cfg.Server
FAIL
```

This compile RED is supplementary, not represented as behavioral RED. Independent config/discovery behavioral RED using an overlay API stub is recorded in `tdd-parsed-config.md`.

## Frozen SHA-256

The following final API-adapted test files and bridge fixture were frozen before prepared API implementation:

| File | SHA-256 |
| --- | --- |
| `internal/service/service_test.go` | `b656412ffbb23e28ac058b04179146602238a4e69c43ff11d64f9efdfbcb783c` |
| `internal/service/regression_test.go` | `cc2b6150a766ded0ad81a827778bcba4e6a13748e950713e6705011c59480f95` |
| `internal/service/logging_test.go` | `55a291ceb81577e4bc5bf9c4634caa34c84f6cb7464dea00c5f344cc3d2e84ef` |
| `internal/service/jsonv2_test.go` | `79a553697123fad309ca6871b3f8ba60f7825fe0f9479b9724e3b05aa89699dc` |
| `internal/service/parsed_config_test.go` | `cf79360caa955b3cc950677b23b32cd36a8ffbcaaffadb3dae2fbd9fcee5e3a4` |
| `internal/httpapi/httpapi_test.go` | `e832e2931666541c1e268969e434e1877c2df74956922d7a3637d3f0f72ffbb5` |
| `internal/httpapi/parsed_config_test.go` | `02a517bd6a34eb253adde663753045f8bad1f2bcb2c66caa87572fbb0fd96e7d` |
| `internal/cli/cli_test.go` | `de788cacd370972482021752a890254b78026dea5284925b6c32331b91c3d971` |
| `internal/app/app_test.go` | `30e1342acb5c6ae597a9dd9aa8f219925c184cf7b174ce2f301a944a87e42214` |
| `internal/integration/flow_test.go` | `b2055748faaed2873a30a33dff232907e30415c9f15f3274fe96503c0c7c7d25` |
| `integration/flux/bridge/main.go` | `c3c3990d8dec3819bd99f114a0b4fa14045640fae82389d321b2adb353639efe` |

## Independent GREEN and source review

After the production implementation, the independent consumer test author ran:

```text
MISE_TRUSTED_CONFIG_PATHS=$PWD mise exec -- go test ./internal/service ./internal/httpapi ./internal/cli ./internal/app ./internal/integration -count=1
ok internal/service
ok internal/httpapi
ok internal/cli
ok internal/app
ok internal/integration
```

All 11 frozen test/bridge SHA-256 values still match the table above. No test expectations or assertions were changed after freezing.

Source review confirmed that service construction checks only the prepared runtime sentinel plus service-owned runtime limits, preview delegates candidate binding to `config`, and static invalid documents cannot cross the prepared `Config` boundary. Unknown candidate credentials remain `invalid_config` before scanning. App prepares the runtime once and uses its effective settings for listener/timeouts; CLI serializes `Document()` without applying local server overrides. HTTP delegates JSON syntax to `encoding/json/v2` and configuration meaning to `config.Decode`. No consumer implementation defect was found. Prepared document JSON-encodability is an invariant of the config package; its additional boundary review is recorded separately.

## Actual-binary smoke harness flakiness review

The final actual-binary smoke initially timed out waiting for the startup log. An independent deterministic pipe reproduction confirmed a harness bug: `selectors.select` checked OS descriptor readiness while `TextIOWrapper.readline` could read two JSON lines into its own buffer. After returning the first line, the OS pipe was empty. The selector then timed out although the startup line was already buffered in Python:

```text
first readline: {"msg":"scan completed"}
select sees second line: False
second line already buffered: {"msg":"discovery server started","address":"127.0.0.1:1234"}
```

A single rerun of the original actual-binary smoke passed, consistent with its dependence on log ordering/coalescing. No production change was needed. Only ignored `.cache/manual-smoke.py` was corrected to read binary stderr with `os.read`, split complete lines from an explicit buffer, retain incomplete records, and capture all service stderr on failure. It also waits within the original bounded startup deadline for both the listener address and the initial completed scan before asserting readiness and generation; the server's startup log alone does not promise that its asynchronous scan has finished. Child cleanup remains bounded and always reaps the process.

The corrected raw-descriptor reproduction consumed both coalesced records. The final harness then passed 10 consecutive actual-binary runs, each preserving all original assertions:

- example validation exit 0 and binary version output;
- health, readiness and status HTTP 200 with no-store, completed generation and empty source/filter maps;
- missing filter HTTP 404 with structured error and no inputs;
- actual CLI dry-run exit 3, pure versioned JSON and scan.interval diff;
- SIGTERM exit 0, shutdown log and no fake credential in logs.

The fixture used only a temporary localhost listener, a non-real token and zero GitHub sources. No live GitHub integration is claimed. Frozen tests and the Flux bridge retained all 11 recorded SHA-256 values. Final ignored harness SHA-256: `5779b3c263b0ab91121dd0671d31112d966fc401369ec614b3858530483aea8a`.

This paragraph records the historical Python check. That harness was subsequently removed after replacement by the permanent [Go binary test](tdd-cli-binary.md), now included in `mise run check`.
