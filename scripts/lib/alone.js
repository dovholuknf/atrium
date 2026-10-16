// Runs a bucket file by itself: `node scripts/board-tests/chat.js`. The units it holds are handed to the harness as
// HEADLESS_UNITS, so they run through the same unit() as in the full run (the mock put back between them, the results
// file, the exit code) and nothing else runs. Anything already in HEADLESS_UNITS is left alone, to name a few of them.

module.exports = function alone(units) {
  if (!process.env.HEADLESS_UNITS) process.env.HEADLESS_UNITS = units.map(u => u.name).join(",");
  require("../test-board-headless.js");
};
