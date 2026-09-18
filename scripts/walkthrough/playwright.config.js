// Config for the recorded walkthroughs.
//
// IT EXISTS BECAUSE PLAYWRIGHT FINDS NOTHING WITHOUT IT. Run from the repo
// root with no config, `npx playwright test` reports "No tests found" even
// with the file named explicitly, because the positional argument is a regular
// expression matched against paths and the default test directory is not this
// one. Pointing `testDir` at this folder is the whole fix, and it makes the
// command the same from anywhere:
//
//   npx playwright test --config scripts/walkthrough/playwright.config.js --headed
//
// ── the timeout is the load-bearing setting ──
//
// These are recordings, not tests. They are SLOW ON PURPOSE: `slowMo` puts a
// gap between every browser action and the specs pause again wherever a person
// would stop to read. That walks straight through Playwright's 30 second
// default and fails a run that was working perfectly, with a timeout that
// looks like the board hanging.
//
// Five minutes, because the cost of it being too generous is a genuinely stuck
// run taking five minutes to say so, and the cost of it being too tight is a
// recording that cannot be watched.
const { defineConfig } = require("@playwright/test")

// The pace, in milliseconds between actions. The only knob worth turning.
const SLOW = Number(process.env.ATRIUM_SLOW || 600)

module.exports = defineConfig({
  testDir: __dirname,
  // One at a time and in order. These drive ONE board and make directories on
  // a real disk, so two at once would be two runs fighting over `D:/zztest`
  // and over the same provider row.
  workers: 1,
  fullyParallel: false,
  // A retry would re-record over a perfectly good failure, and the failure is
  // the thing worth watching.
  retries: 0,
  timeout: 5 * 60 * 1000,
  outputDir: "../../test-results",
  reporter: [["list"]],
  use: {
    baseURL: process.env.ATRIUM_BOARD || "http://localhost:7778",
    launchOptions: { slowMo: SLOW },
    video: "on",
    screenshot: "only-on-failure",
    viewport: { width: 1600, height: 1000 },
    // A recording nobody can read is a recording nobody watches.
    deviceScaleFactor: 2,
  },
})
