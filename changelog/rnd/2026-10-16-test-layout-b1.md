- The headless board suite's mock server and browser helpers moved to `scripts/lib/mock.js` and `scripts/lib/browser.js`.
  All the mock's state is now one object with `snapshot` and `reset`, put back before every unit in the serial run as
  well as in a shard, so a unit that leaves state changed cannot reach the next. No assertion changed. rd-test-layout,
  stage B1.
