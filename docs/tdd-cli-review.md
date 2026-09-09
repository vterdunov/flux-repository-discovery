# Independent CLI review regressions

After baseline CLI and component integration were GREEN, the independent review found that syntactically valid but incomplete success reports could be printed with exit 0. The consumer must not present a partial dry-run as completed. Reproduced cases: missing observation time, positive afterCount with missing complete proposed inputs, and negative counts. These expectations refine the existing incomplete-success rejection contract without changing frozen baseline tests.

A separate passing check confirms candidate configuration is not forwarded to a redirect target.

RED command:

```sh
mise exec -- go test ./internal/cli
```

Exit 1. All three partial-report cases return 0, write the malformed report to stdout and leave stderr empty. Frozen regression SHA256:

```text
047b8bad9fd0a75efabb69ef201aa5d1923bb94de93b49a808ea9288bf084639  internal/cli/regression_test.go
```

The fix should validate the supported report's mandatory observation and count/output consistency before any success output. Unavailable drift may retain its empty diff value; it does not claim a completed comparison.
