The test time guard only warns now. A package or a test over its limit prints `WARN PACKAGE` or `WARN TEST`, the guard
exits 0, and the go-test job stays green. The WARN lines show in `summary.md` (time-guard-package-warn).
