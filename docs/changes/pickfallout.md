# pickfallout

`internal/cardroot` gained an `Env` (OS name and home folder lookup) with `StateDir`, `Resolve` and `Check` methods. The
package functions call the real machine's `Env`, so behavior is unchanged, and the tests build their own `Env`.

The flat waits added by the picks in `chat.js`, `pulls.js` and `test-board-headless.js` now wait on a condition under
one deadline. Three stay as flat waits with a `// wait:` mark: the injected latency in two chat routes and the idle
probe window in `internal/roomstats/drift_test.go`.

## Test plan

### @LETTER@1. The checks

1. Run the two check scripts, check-test-globals and check-test-waits.
2. Run the cardroot and roomstats Go tests with `-count=3`.

**Expected:** both scripts pass and the tests are green.

### @LETTER@2. The board units

1. Run the chat, pulls, hears and back-to-hub headless units where playwright is installed.

**Expected:** the units pass with no added flat wait.
