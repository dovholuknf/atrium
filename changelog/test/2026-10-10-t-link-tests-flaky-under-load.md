# The link tests run beside each other and finish under load

The integration tests of `internal/link` went from 407s to 238s, and finish in 344s with six busy cores where they did
not finish in 550s. The git rigs and the announce tests run in parallel, four rigs at most. Item
t-link-tests-flaky-under-load.
