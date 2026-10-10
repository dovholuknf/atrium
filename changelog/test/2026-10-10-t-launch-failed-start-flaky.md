A launch whose runner dies the instant it starts is now always reported as a failed start. Before, under load, the
runner could be gone before the launch looked for it, and the launch said it was running. Two flaky integration tests
(the failed-start ledger test and the scoped everywhere stream test) now wait on events with bounded, explained
deadlines. Item t-launch-failed-start-flaky.
