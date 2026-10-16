Docs: none, test tooling only. The design is `docs/rnd/test-layout-design.md`, stage B1.

What moved: the top of `scripts/test-board-headless.js` is now `scripts/lib/browser.js` (the shifted clock, `slow`, the
fake-clock helpers, the Playwright import) and `scripts/lib/mock.js` (the mocked daemon's server, its fixtures and its
state). The harness still holds every unit and every assertion.

The mock's state is one object, `mock`. The 52 loose `let`s became fields (`mock.tasksMode`), and so did the objects
sections mutate in place (`PIN`, `LAND`, `HON`, `HOFF`, `FON`, `FOFF`, `LOOSE`, `ALPHA`, `SGG`, `qDismissed`,
`setupFixed`, `filesDeleted`, `gateCalls`, `walkDirs`), plus `stuckStep` and `peekReads`. `mock.snapshot()` deep-copies
every field and `mock.reset(snapshot)` puts them back, copying again so one snapshot can be reset to many times. The
harness resets before every unit, serial run included, except between the members of a pin group.

## Test plan

## @LETTER@. The headless harness library

### @LETTER@1. The same units pass, serial and sharded

1. Run `FULL_SUITE=1 node scripts/test-board-headless.js` with `HEADLESS_RESULTS=serial.json` set.
2. Run `FULL_SUITE=1 node scripts/test-board-sharded.js --local`.

**Expected:** both pass the same units as before the change. Every unit name is listed as passing in each run.

### @LETTER@2. A unit cannot leak mock state into the next

1. Run `HEADLESS_LEAKS=1 HEADLESS_UNITS=core,history,core2,skinScope node scripts/test-board-headless.js`.

**Expected:** `core2` reports `LEAK core2 soloMode: "ok" -> "gone"`, and the `skinScope` lines that follow start from a
reset mock (`settingsReads: 0 -> ...`, not carried on from `core2`).

### @LETTER@3. A new module-level `let` fails the listing

1. Add `let leaky = 0;` at the top level of `scripts/lib/mock.js`.
2. Run `HEADLESS_LIST=1 node scripts/test-board-headless.js`.

**Expected:** it exits 1 and says a module-level `let` holds `leaky` and to put it on the mock object.

### @LETTER@4. Touching the library plans bootClean

1. Touch `scripts/lib/mock.js` and run `bash scripts/test-touched.sh --plan`.

**Expected:** the plan runs `bootClean` and warns that the file is the library every unit runs on.
