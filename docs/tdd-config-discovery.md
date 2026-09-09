# Config and discovery API acceptance contract

Authored independently before implementation on 2026-09-09. Expectations derive from the original configuration, filter, and HTTP requirements and consumer examples, without using implementation output. The completed implementation plan has been removed; current behavior is documented in the [README](../README.md).

## Public package contract

- `config.Decode` strictly loads either YAML or JSON into normalized typed configuration. Defaults are listen `:8080`, interval `10m`, timeout `2m`. Durations serialize as strings for dry-run requests.
- `config.ApplyEnv` applies only documented overrides to a value, preserving the declared configuration for later diff.
- `config.ValidateCredentialReferences` checks named references against the server registry without reading any secrets.
- `discovery.Evaluate` consumes one named filter and complete source catalogs. It returns deterministic Flux inputs plus unmatched explicit full-name references, or an error without partial results.
- Repository IDs are numeric typed values internally, decimal strings in Flux output, sorted numerically. Unmatched names are normalized to lower case, deduplicated, and sorted lexically. Topic ordering does not make duplicate repository metadata conflict.

## RED evidence

Command:

```sh
mise exec -- go test ./internal/config ./internal/discovery
```

Result: exit 1. Both packages compile; positive behavior tests fail with `not implemented`. Strict validation cases fail because the stub does not report the required field path. Examples: `TestDecodeDefaultsAndTypedFields`, `TestJSONRoundTripRetainsCanonicalConfigAndStringDurations`, `TestEvaluateCombinesRulesAndExclusions`, `TestEmptyResultSerializesAsArrayAndMissingSourceFails`. No production behavior was implemented before this run.

Acceptance files frozen at the start of implementation:

```text
a6efcff53b9031ee4fec835f44027b760fd4867c4875d98016658ea4adad9d3b  internal/config/config_test.go
bba31b777c185a17914d89e79beb2d66cb156b492f4395f34108bd1d9b29d24f  internal/discovery/discovery_test.go
```

Changes to expectations require an independently checked requirement or contract correction, documented separately before implementation continues. Tests include strict parsing paths, credential reference checks, env behavior, AND/OR/all/any/exclude, repository identity, output shape, conflict failure, immutability, and fuzz invariants.
