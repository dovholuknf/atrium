The headless board suite runs sharded: `node scripts/test-board-sharded.js` splits its 166 units over several processes,
each with its own mock server and browser, and prints one merged report with a time per unit, the failures named and an
exit code. 22 minutes serial on sg3 became about 3 minutes on 9 shards. Failures are retried once alone, and
`scripts/board-suite-flaky.json` labels the known flaky units. The plain run and `HEADLESS_ONLY` are unchanged.
(u-new-board-suite-faster, part 1)
