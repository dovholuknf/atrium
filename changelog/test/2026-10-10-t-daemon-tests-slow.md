The internal/daemon tests run in parallel where they share nothing, cutting the package from 511s to about 205s on sg3 (t-daemon-tests-slow).
No test assertion changed and no production code changed.
