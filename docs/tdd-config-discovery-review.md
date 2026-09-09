# Independent config/discovery review regressions

Reviewed after the initial implementation passed frozen acceptance tests. Baseline test files are unchanged. Two requirement gaps have separately authored regressions before fixes:

1. Accepted `exclude: []` does not remain normalized across the CLI JSON boundary: the optional empty slice serializes away and becomes nil. The existing fuzz contract already requires accepted configurations to round trip. The new deterministic test accepts either rejection of an explicit empty optional list or stable normalization, without prescribing implementation.
2. Malformed nonempty required owner/name metadata (spaces, newlines, owner `-`, repository `..`) reaches published inputs. PLAN section 6 requires malformed mandatory data to fail the whole dependent result. The accepted GitHub owner/name syntax should be consistent with explicit names in configuration.

Additional passing checks cover nulls, aliases, duration overflow, empty listen and parser size bounds.

RED command:

```sh
mise exec -- go test ./internal/config ./internal/discovery
```

Exit 1. `TestAcceptedEmptyOptionalListKeepsNormalizedJSONRoundTrip` fails on empty versus nil exclusions. `TestMalformedRequiredIdentityFailsWholeFilter` shows malformed identities returned successfully alongside a valid repository. Existing acceptance tests remain unchanged.

Frozen SHA256 before implementation fixes:

```text
a88f745a0f445ecbf02a925019a2a88e4373d60c5b87fea7de8456580cd517ba  internal/config/regression_test.go
5b7060c59c637a7f6da06eb4a60e7db465565fbc19779100f8f100e87c20aa0d  internal/discovery/regression_test.go
```
