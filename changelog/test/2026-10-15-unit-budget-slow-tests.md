Eight tests that generate certificates, wait out a linger or run a real command over a socket moved to
`*_integration_test.go` under `//go:build integration` in `internal/link` and `internal/cli`, so the 10ms unit budget in
`scripts/test-touched.sh` passes on Windows. The assertions are unchanged. unit-budget-slow-tests.
