# Independent JSON v2 migration acceptance tests

Historical contract: the subsequent user-approved [prepared configuration refactor](tdd-parsed-consumers.md) changes duplicate candidate JSON fields from HTTP 422 to 400 and removes the manual tokenizer. Its independent tests also adapt configuration construction to the new Go API. The hashes and review below describe the earlier migration handoff.

The independent API test author defined these expectations before production migration began. The 17 existing frozen test files remain unchanged. Four new blackbox test files exercise public package APIs and HTTP/CLI JSON; they do not inspect imports, serialization helper internals, or source layout.

The intended migration preserves the existing external contract while rejecting ambiguous or invalid JSON at the input boundary:

- A successful CLI report with duplicate keys, including escaped names and nested duplicates, is an error with no successful stdout. Invalid UTF-8 is rejected rather than repaired.
- Invalid UTF-8 anywhere in the dry-run JSON document is HTTP 400 `invalid_request` before scanning. Repeated outer `config` names are HTTP 400, including escaped aliases. The existing frozen test continues to require HTTP 422 for a duplicate field within a syntactically valid candidate configuration.
- A valid HTTP report still distinguishes an unavailable drift comparison's `null` slices from a successful empty filter result's `[]`. Zero generation/counts and false flags remain explicit. Empty override maps remain `{}`.
- CLI JSON output retains the supplied complete report, including explicit raw `{}`, `[]`, `false`, and `0` values in before/after changes. None of those values mean an absent change field.
- Existing configuration revisions are stable across the migration and map insertion orders. A fixed pre-migration fixture includes regexp characters `<` and `&`, so changing JSON escaping cannot silently change its revision. Duration values remain strings through HTTP request decoding, preview computation, and report serialization.
- Missing or null mandatory repository metadata cannot become a successful catalog. Metadata completion still has to provide the missing information. Unrelated, legitimate GitHub fields are accepted. Existing independent GitHub tests continue to reject `id`/`ID` ambiguity.

The fixture revision is `4d35272db8411a2084633b82af16f6dd2a004696f5ce1ac95db4cb857ec876f2`, the SHA-256 of this pre-migration canonical JSON:

```json
{"version":1,"server":{"listen":":8080"},"scan":{"interval":"10m0s","timeout":"2m0s"},"sources":{"company":{"owners":["acme"],"credential":"token"},"personal":{"owners":["bob"],"credential":"token2"}},"filters":{"apps":{"sources":["company"],"include":[{"nameRegex":"^[\u003c\u0026]"}]},"platform":{"sources":["personal"],"include":[{"nameRegex":"^infra-"}]}}}
```

## RED evidence

Before any production migration edits, on the pinned Go 1.27.1 toolchain:

```sh
mise exec -- go test -count=1 ./internal/cli ./internal/httpapi ./internal/service ./internal/github -run '^TestJSONMigration'
```

Exit 1. All four malformed successful CLI report cases incorrectly return exit 0 with a report on stdout. Both invalid UTF-8 HTTP cases return 422 rather than 400. All preservation tests, the escaped duplicate envelope case, and GitHub metadata/unknown-field cases already pass. Complete output is retained in [tdd-jsonv2-red.txt](tdd-jsonv2-red.txt).

The revision fixture was calculated independently from the displayed canonical JSON and then verified through public status/report APIs before freezing. No existing failure expectation was relaxed.

Frozen SHA-256 values:

```text
bbd64455a69597d31fb86d2ed427e59f28ba16b1af17ba2169864234f480700f  internal/cli/jsonv2_test.go
2748fbcc8f5a698f4600013d1f82c18307d5c02fbf5373a532e25b911102575d  internal/httpapi/jsonv2_test.go
9ed9e906625bd81ae50012043ce5d9093f90d2df9d28fdafc4e44136d418f8d1  internal/service/jsonv2_test.go
d9bc5841074a2854bd0747f45935cfed16b8d5d76ff06a2220387f1ea6ac5430  internal/github/jsonv2_test.go
```

## GREEN and independent implementation review

After the migration, the independent test author reran the exact acceptance command above with `-count=1`. All four packages passed. The six previously failing cases now meet their expectations without changing any test.

The review compared all production changes against `.cache/jsonv2-before`, inspected the shared output policy and each JSON input boundary, and found no material migration defect:

- Runtime code uses `encoding/json/v2` and `encoding/json/jsontext` directly. The shared output helper explicitly retains deterministic ordering, existing HTML/JavaScript escaping, raw string escapes, and null diagnostic collections.
- Configuration hashes and response-size checks use the same encoding policy as the external output. The fixed revision fixture passes for both source/filter map insertion orders.
- Raw change fields use `jsontext.Value` with `omitzero`, preserving explicit `{}`, `[]`, `false`, and `0`. In Go 1.27, the old `json.RawMessage` name aliases the same type, so existing callers and frozen fixtures remain source-compatible.
- The HTTP envelope clones its raw candidate before the tokenizer advances. The narrow duplicate-name exception preserves candidate validation's HTTP 422 boundary; both the envelope parser and candidate parser still reject duplicates at their respective levels. Invalid UTF-8 is rejected by the JSON tokenizer before candidate decoding.
- GitHub decoding retains the independent depth bound and case-alias rejection while the standard tokenizer rejects malformed JSON, duplicate names, and invalid UTF-8. Typed repository metadata checks and acceptance of unrelated response fields remain intact.
- CLI success decoding uses strict v2 defaults and preserves the existing completeness checks before writing stdout.

The review independently verified all 18 pre-existing test files against the pre-migration snapshot byte-for-byte: 17 internal test files plus the Flux consumer contract test. All four new acceptance file hashes also match their frozen values above. No production or test files were changed during this review.
