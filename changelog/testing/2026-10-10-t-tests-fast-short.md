- **`make test-short` is a fast land set.** It runs the integration build with `-short`, and 778 tests that take real
  work (git subprocesses, loopback rigs, ptys, sqlite on disk) now skip themselves there with a `slow:` reason. A
  full run took 4m29s and the short set takes about 55s. CI still runs everything through `make test-all`. Tests that
  guard the halt, hooks and shutdown are never skipped. The table of slow tests is in
  `docs/changes/t-tests-fast-short.md`. (t-tests-fast)
