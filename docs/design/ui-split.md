# Splitting the board into pages

Status: plan only. No board code moves with this note. Numbers come from `grep`, `wc` and small node scripts run on
`claude/ui-plan` at `d29937977`, and each claim a later step depends on names its file and line. The model for the shape
is `docs/design/go-modules.md`.

## What this is for

Three gains, in the order they matter:

1. **One page can be loaded, run and tested alone against a fake API.** Today the smallest thing a test can load is the
   whole board.
2. **A red names a page, and a flake has one place to live.** 201 rows of `scripts/flaky-jobs.jsonl` say the suite is
   red often and for reasons that live outside the page that failed (section 2).
3. **Less context for an agent.** One page directory, one list of the globals it may use, one fake API to read.

Four facts shape everything below, so they come first.

**Fact 1: the board has no modules, only a shared global scope.** 100 classic scripts in `internal/api/web/js/` declare
3,284 top-level names (2,394 functions, 890 `let`/`const`) and none is redeclared, because the load order in
`index.html` is the only namespace. `check-board.sh` says so in its header. Every name is visible to every file.

**Fact 2: the production board is served as files, the test board is not.** `internal/api/web.go:21` embeds the
directory and serves each file. `scripts/lib/mock.js:384` builds `HTML = wholeBoard()` instead, which
`scripts/board-source.js` makes by pasting every script into one string in load order. Any file that uses `import`
cannot be pasted, so the test server has to change before the first module can exist (cut 3).

**Fact 3: half the cut is already made, on the test side.** `scripts/test-buckets.json` names 26 buckets (19 for the
 desktop board, 7 for the phone) with the
source files, units and checks each owns, and `scripts/check-test-buckets.js` lints it. `scripts/board-tests/` holds
15 bucket files of 13,661 lines. What is left is the code, and the 30,501 line harness those buckets still run under.
This plan reuses the bucket names as the page names and does not invent a second map.

**Fact 4: the repo's no-bundler stance holds.** Native ES modules need no build step. The stance in
`internal/api/web/AGENTS.md` is "no build step", and the vendored xterm stays a UMD classic script, which a module can
read as `window.Terminal`. Section 3 argues it, and names the one cost.

## 1. What the board is today

### The shell

`index.html` is 3,998 lines with 723 `id=` attributes, 30 `<dialog>`s, 112 `<script>` tags (5 vendored, 5 for the
phone code that `/m` shares, the rest the board) and 237 inline `on...=` handlers. Two inline `<script>` blocks sit in
the head (`index.html:108` redirects a phone to `/m`, `:156` sets the `switching` class). The phone board is a second
page, `m/index.html` (208 lines) with 18 files and 7,409 lines under `m/js/`, and is out of scope (section 6) apart from
its tests.

The 14 views are `const VIEWS` at `js/board.js:391`, one container each in `index.html`:

| View | Container line | Tab |
| --- | --- | --- |
| board, stack, perms | 293, 296, 308 | board, stack, perms |
| terms | 402 (354 lines, to 755) | terminals |
| history, usage | 756, 782 | history, usage |
| pulls, hubrepos, items, backlog | 818, 847, 866, 872 | pulls, repos, items, backlog |
| audit, chat, chatter, runners | 880, 902, 922, 963 | audit, chat, messages between agents, rooms |

Documents and files have tabs (`data-view="docs"`, `"files"`) but are not in `VIEWS`, which is itself a coupling fact:
`switchView` (`board.js:478`) is a 40 line function that knows each page by name. It hides containers by id, calls
`chatView`, `fdReset`, `paintBoardShow`, `renderHistory`, `loadUsageTab`, `sizeCommands`, `placeTabBridge`, `lrReturn`
and more, each guarded by `typeof X === "function"` because the callee may not be loaded yet.

### Which files each page owns

From `scripts/test-buckets.json`, with the sizes and the cross-file references measured. "Out" is references from the
page's files to names declared in a file of ANOTHER page, "shared" is references to `core.js`, `boot.js`, `cards.js` and
`index.html`, "own" is references to a different file of the same page.

| Page | Files | Lines | Own | Shared | Out | Heaviest outgoing |
| --- | --- | --- | --- | --- | --- | --- |
| terminals | 28 | 17,223 | 601 | 382 | 761 | boot 241, cards 155, growls 154 |
| rooms (hub, restart, transports, down) | 8 | 5,058 | 55 | 234 | 176 | boot 65, terminals 35, settings 27 |
| boot (`board.js`, the shell) | 4 | 4,913 | 45 | 203 | 285 | terminals 98, cards 66, rooms 48 |
| settings, themes, providers | 8 | 4,501 | 48 | 178 | 334 | boot 100, terminals 68, rooms 60 |
| growls (notify, toasts, keepalive) | 10 | 4,085 | 86 | 74 | 139 | terminals 37, boot 29, settings 22 |
| cards (groups, lanes, filter) | 12 | 3,932 | 30 | 107 | 227 | terminals 82, boot 69, growls 28 |
| usage | 8 | 2,287 | 100 | 128 | 76 | rooms 29, terminals 16 |
| docs-files | 1 | 2,044 | 0 | 53 | 86 | rooms 29, terminals 22 |
| items | 3 | 1,206 | 19 | 91 | 43 | boot 20, usage 12 |
| pulls | 1 | 820 | 0 | 49 | 48 | rooms 14, terminals 12, cards 11 |
| hubrepos, changereq | 1, 3 | 675, 612 | 0 | 80, 67 | 12, 50 | changereq 7, hubrepos 46 (each other) |
| launch, chat, backlog, merge, homes, route | 9 | 1,912 | 6 | 124 | 109 | rooms, terminals |

Across the board there are **5,132 references between files** (766 of the 9,900 possible file pairs are connected, and
24
files each reach 10 or more other files, the worst `settings-spine.js` with 54). Of them about 47 percent cross a page
boundary, 34 percent go to the shared core and 19 percent stay inside a page. So the page is not where the code
clusters. The boundary that exists in `test-buckets.json` is a label, not a fact of the code.

The five names with the widest reach, by distinct files that use them: `esc` (55, `core.js`), `api` (51, `core.js`),
`lastTasks` (24, `board.js:1829`), `bareId` (23, `notify.js`), `setHTML` (21, `board.js`). `lastTasks`, the card list,
is a bare `let` that 24 files read and `board.js` writes. There is no store, so the card list is a module variable by
convention only.

Names that tell the truth about the shape: `board.js` is filed under the bucket `boot` and is 2,515 lines of list
painting, view switching and the card poll. It is the shell and the cards page in one file. `settings-spine.js` (2,212
lines) holds the settings spine, the stream, the task poll, health and the rooms counter. **The one `EventSource` on the
board is in it** (`settings-spine.js:1881`).

### How the pages talk to each other

Four channels, none declared.

1. **Bare calls across files.** The 5,132 above.
2. **Optional calls.** 348 `typeof X === "function"` guards in `js/`. Most are a page calling another page that may
   have loaded after it. This is the load-order dependency written as a runtime check, and it hides a missing page as a
   silent no-op.
3. **The stream.** `connect()` at `settings-spine.js:1878` opens `/v1/events` and registers 32 listeners
   (`es.addEventListener`, lines 1946 to 2185). Every page that wants an event has a line there, and `es.onopen` calls
   a dozen other pages by name to re-read (`settings-spine.js:1896` to `:1945`: `growlReseed`, `onAuditEvent`,
   `onUsageStreamOpen`, `onPullsStreamOpen`, `loadHubHosts`, `loadHubTransports`, ...).
4. **The DOM.** 1,279 `getElementById` calls and 329 `querySelector(All)` calls against 723 ids in one document. 117 of
   the ids are named in more than one JS file, 36 are in no JS file (CSS and labels only), and `fixtures.js`,
   `runners.js`, `notes-files.js` and `settings-spine.js` each read 61 to 88 distinct ids. 45 ids the JS asks for are
   in no markup because the JS builds them.

Also measured: 399 `addEventListener`, 202 `setTimeout` and 20 `setInterval` in `js/`, 216 `localStorage` uses, and 271
`api(` calls against about 108 distinct literal paths (67 `api("...")` call-site prefixes, the rest built in strings).
The API is the one place the coupling is already cheap, because every call goes through `api` in `core.js`.

## 2. Where it breaks

### What CI records

`scripts/flaky-jobs.jsonl` holds 201 rows from 2026-10-22 to 2026-10-24, 120 of them failed jobs and 72 of those board
jobs. `scripts/flaky-history.md` holds 34 ADD moves in the same two days. The most frequent names:

| Name | Rows | What it is |
| --- | --- | --- |
| `(step failed, no test named in the log)` | 20 | a step died before any unit reported |
| `check: titles` | 19 | a check script, one cause across three operating systems at once |
| `TestEveryExecStartIsHiddenOrListed` | 12 | Go |
| `cardViewBarInline`, `cacheChip`, `land` | 6 each | board units: layout, chart, deploy |
| the seven `phone*` units (`phoneFocus`, `phoneFollow`, `phoneKeyboard`, `phonePan`, `phoneShare`, `phoneTap`, `phoneView`) | 6 each, all at the same runs | the `/m` page, failing together |

Ten names went on the flaky list, were graduated by hand on 2026-10-22, and went back on within two days
(`termFitHeight`,
`atriumDown`, `heldLine` and the phone units among them). A fix that does not hold is a sign that the cause is shared,
not in the unit. Seven units failing in the same runs says the cause is the page and the harness they share, not seven
bugs.

### What in the code causes it

The tests are not the only cause, and the board's own structure puts three of them in every unit's way.

- **Timing is the assertion.** The headless harness and its bucket files hold 660 `waitForTimeout`, 1,500 `slow(`
  scaled deadlines, 135 fake-clock calls (`fakeWait`, `fakeTick`) and 2,722 `p.evaluate` calls that read page globals
  (`VIEWS.find(isViewing)`, `cr.tab`, `hubReposView()`: `scripts/board-tests/route.js:11`).
  `docs/troubleshooting-tests.md`
  part 3 and part 5 agree on the pattern: a unit measures the renderer and not the product, and the fix each time was
  "wait
  on the state the step asserts". That works one unit at a time, and 278 weighted units (1,532 s serial, mean 5.5 s) is
  too many to do it for.
- **Every unit boots the whole board.** `bootClean` weighs 19 s, `core2` 26 s, `changeReq` 32 s. A unit for the backlog
  page pays for the terminal pane, 30 dialogs and the hub poll, and a stray paint from any of them lands in its
  steps. Part 5 of the troubleshooting guide places a stray paint "from the page's first poll pass" in a unit that
  was about a list of thirty two lists.
- **One global state, reset by hand.** `scripts/lib/mock.js` holds 54 URL branches and 52 module `let`s that
  `mockSet` resets between units, plus `const`s mutated in place that nothing resets (`docs/rnd/test-layout-design.md`
  lists `qDismissed`, `LAND`, `walkDirs`, `gateCalls`, `HON`, `FON`). A unit that leaves one behind fails the next
  unit on the shard, which is why a failure reproduces alone and not in the run (the `LOAD` class of
  `docs/troubleshooting-tests.md` part 5).
- **The stream is one shared writer.** One `EventSource` with 32 listeners means a test that wants one event opens the
  whole stream, and a reconnect (`es.onopen`) re-reads a dozen pages. `eventDriven`, `atriumDown` and `hubReposFeed` all
  need
  the stream, and the stream is the thing no unit can fake in part.
- **The TDZ landmine.** `boot.js` has to be the last script (`web/AGENTS.md`) because a `let` below a call that reads
  it throws into a promise nobody awaits. With 890 top-level `let`/`const` in one scope the order is the only
  protection.
- **Weight and CI shape.** `test-board-sharded.js` balances from `board-suite-weights.json`, `ci.sh` runs two board
  shards (`board-1`, `board-2`) and a `board-visual` group (46 units on `scripts/board-visual-units.txt`) that cannot
  redden the run. A unit moved there "is one nobody will fix" in the guide's words. Retry hides a first-try failure
  behind `LOAD` and `PASSED ON RETRY`.

None of this says the units are badly written. They are written against a page that cannot be loaded in part.

## 3. The target

### Decision: native ES modules, no build step

The evidence for it:

- A module needs no bundler. `<script type="module" src="/js/boot.js">` in `index.html`, `import` between files, and the
  embed FS serves them as it serves classic scripts today.
- The cost is request count, and it does not rise. The board already makes 112 script requests. A module graph of the
  same files makes the same number, and `<link rel="modulepreload">` lines remove the waterfall if one shows.
- It ends the two landmines that cost the most. Imports are explicit, so a file reaches only what it names, and a module
  graph runs in dependency order, so the TDZ rule ("`boot.js` is last") becomes "`boot.js` is the entry".
- It makes the files loadable in node. A pure module (`route.js` parsing, `notify` rules, `seen`) is `import`ed by a
  test. Today the 18 `scripts/test-*.js` build the code under test by `new Function(...)` with hand-written stub globals
  (`scripts/test-seen.js:17` stubs eight names before `seen.js` can run) and a stub that drifts passes while the board
  fails.

The costs, stated:

- Modules are strict mode. Code that relies on sloppy mode (an implicit global, `arguments.callee`, a duplicate
  parameter) throws on load. Cut 4 finds these on the smallest file first. `node --check` parses the strict form
  with `--input-type=module`.
- A module does not put its `function`s on `window`, and 204 inline handlers in `js/` and 237 in `index.html` call
  them by bare name. A `window` bridge keeps those alive during the move and is deleted in the last cut.
- `p.evaluate(() => VIEWS...)` in tests reads globals. The bridge keeps them working. New tests do not use it (section
  4).
- node reads `.js` as CommonJS unless the nearest `package.json` says otherwise. The plan adds
  `internal/api/web/js/package.json` with `{"type":"module"}`, and it is embedded and served (a few bytes). If that is
  refused, the files take the `.mjs` suffix and `go:embed` is unaffected.

**Alternative rejected: keep classic scripts and namespace each page as `const Backlog = (() => {...})()`.** It needs no
server change and gives no enforcement, because anything can still read `Backlog.x` and every other global, and node
cannot load it without a stub. It is the current design with a longer name.

**Alternative rejected: a bundler.** The stance in `web/AGENTS.md` is firm and the evidence does not break it.

### Layers

The same shape as `check-imports.go`: a table, and a script that fails when a file imports a layer it should not.

| Layer | Directory | Holds | May import |
| --- | --- | --- | --- |
| 0 core | `js/core/` | `api`, `esc`, `setHTML`, the store, the stream, prefs, the route, the clock | layer 0 |
| 1 ui | `js/ui/` | dialogs (`askUser`, `confirmUser`, `tellUser`), toasts, tooltips, `popfit`, `dragclick` | 0, 1 |
| 2 pages | `js/pages/<name>/` | one directory per page, `index.js` is its only public file | 0, 1, and its own files |
| 3 shell | `js/shell/` | `board.js`'s view switching, topnav, `boot.js`, the page registry | 0, 1, and each page's `index.js` |

Two rules carry the whole design:

1. **A page imports no other page.** If terminals needs a card, it reads the store. If growls wants a terminal opened,
   it
   calls `navigate("terms", { card })`, which is the hash route (`js/route.js` already carries the view, and a deep link
   is how `docs/changes/f-board-deep-links.md` says it works). The shell is the only layer that imports a page.
2. **Layer 0 imports nothing above it.** The store has no idea a terminal exists.

The 5,132 references sort into three outcomes. The 34 percent to the shared core become imports of `core/` and `ui/`.
The 19 percent inside a page become imports inside the directory. The 47 percent across pages are what the cuts below
turn into a store read, a stream subscription, or a navigate, one edge at a time, and cut 1 pins how many there are so
the count only falls.

### State

One small store in `core/store.js`, about 60 lines, not a framework:

```js
export const store = createStore({ cards: [], view: "board", hub: null, prefs: {} });
store.get("cards"); store.set("cards", next); const off = store.on("cards", fn);
```

`lastTasks` (24 files read it) becomes `store.get("cards")`, and the card poll in `board.js` is its only writer. Pages
subscribe in `mount` and unsubscribe in `unmount`, so an unmounted page paints nothing, which is the "stray paint" cause
of section 2 removed at the source. Page-local state is a module `let` inside the page and nothing outside can read it.
`localStorage` keys (216 uses) move behind `core/prefs.js`, which already has a seed in `js/prefs-live.js` (32 lines).

### The stream

`core/stream.js` owns the one `EventSource` (moved out of `settings-spine.js:1881`) and offers `on(event, fn)` and
`onOpen(fn)`:

```js
const off = stream.on("pr", onPullsEvent);   // returns an unsubscribe
stream.onOpen(reread);                       // runs on every (re)connect, in registration order
```

A page declares which events it wants in its manifest (below) and registers them in `mount`, so the dozen name calls in
`es.onopen` and the 32 listeners at `settings-spine.js:1946` to `:2185` become 45 lines of registration spread over the
pages that own them, and each `typeof onXEvent === "function"` guard (348 of them) goes with its caller. Reconnect
order is kept by registration order, and the first migration writes a test for it before moving anything (cut 6).

### How a page declares what API it uses

Each page exports a manifest and two functions from `pages/<name>/index.js`:

```js
export const page = {
  name: "backlog",
  view: "backlog",                           // the VIEWS id, and the hash route
  api: ["GET /v1/backlog", "POST /v1/backlog/{id}/priority"],
  events: ["backlog"],                       // stream events it listens for
  reads: ["cards"],                          // store keys it reads
  mount(ctx) {},                             // ctx = { root, store, stream, api, navigate }
  unmount() {},
};
```

Three uses, each a check or a test and none a runtime framework:

- The fake API (section 4) answers exactly `api` and the harness fails a test that calls anything else, with the path in
  the message. Today a missing mock answer is a hung fetch and a timeout.
- `check-board-pages.js` greps each page's files for `api("...")` and `fetch("...")` literals and fails when a path is
  not
  in the manifest, or when the manifest names a path nothing calls. A path built from a variable is written in the
  manifest as the pattern and the check accepts a literal prefix.
- The shell builds the nav, the route table and `VIEWS` from the registry, so adding a page is one directory and one
  line in `shell/pages.js`.

### Markup

The markup stays in `index.html` until each page is otherwise done. Then each page's container moves into `pages/<name>/
view.js` as an exported string the shell writes into the view's container on first mount, so the harness loads one
page's markup and not 3,998 lines. The template literal is the backtick landmine in `web/AGENTS.md`, so
`check-board.sh` parses each module on its own (`node --check` per file, which also gives a real file name in the
error and not a line in a concatenation). The 30 `<dialog>`s that belong to one page move with it, and the ones that
several use stay in the shell.

## 4. Testing

### Three tiers

| Tier | What it loads | Speed | Replaces |
| --- | --- | --- | --- |
| node unit | one pure module, imported, with `node --test` | ms, no browser | the 18 `test-*.js` that rebuild code with `new Function` and stubs |
| page harness | `harness.html` that mounts ONE page from its manifest, in one browser context, against the fake API | about 1 s a unit | most of the 436 `await unit(` blocks in `test-board-headless.js` |
| board smoke | the real `index.html`, the whole mock, the shell | tens of seconds | what is left: `bootClean`, `route`, nav, a handful of cross-page flows |

The target for smoke is 15 units or fewer, down from 436 `await unit(` blocks in `scripts/test-board-headless.js` plus
the
ones already in `scripts/board-tests/*.js`. The weights say what moves: of 1,532 s, terminals 528 s, phone 421 s, cards
255 s, growls 218 s (`docs/rnd/test-layout-design.md`), and every one of those units loads the full board to test one
page.

### The fake API

`scripts/ui/fake-api.js` is a function `fakeApi(manifest, fixtures)` that returns the handler a test server mounts. It
answers the manifest's paths from fixtures, records every call (so a unit asserts "one POST, with this body" and not "a
paint happened"), 404s anything else with a message naming the path and the page, and has a push-controlled fake
stream: `fake.emit("backlog", {...})` writes an SSE frame, and `fake.drop()` closes it, so a reconnect is one line and
not a race. Fixtures sit with the page, `pages/<name>/fixtures.js` (not `js/fixtures.js`, which is the product's own
"fixtures" feature and a different thing). The 927 lines and 52 module `let`s of `scripts/lib/mock.js` are cut the same
way, one page's endpoints at a time, each moving with its page. State is per fake instance, so nothing leaks between
units,
which removes the reset-by-hand class of failure.

### The harness

`js/harness.js` plus a 20 line `harness.html` (dev only, served by the test server, not linked from the board, and
embedded like any file) read `?page=backlog`, import `pages/backlog/index.js`, build a `ctx` over the real `store` and
`stream` and the fake API, and mount into an empty `<main>`. Because the page imports only layers 0 and 1, this is the
whole dependency set, and a page that needs something else fails to import, which is the point.

Time: tests drive time with the browser's clock (`page.clock`) for anything timed, and wait on the page's own state
attribute for everything else. `route.js` already does it (`data-route="ready"`, "a test waits on that and on nothing
else": `js/route.js:11`). Each page sets `data-ready` on its root when its first read has painted, and the harness
exposes it. The `slow(`-scaled deadlines (1,500 uses) stay for smoke only.

### What shrinks the headless file

A unit moves in three moves, none of which changes the assertion: the unit's body moves from the harness to its bucket
file (15 of the 19 desktop buckets already have one, `scripts/board-tests/index.js`), the bucket file moves next to the
page
(`pages/<name>/<name>.test.js`), and the unit's mock moves into the page's fixtures. The harness file shrinks as each
page lands, and `test-buckets.json` updates in the same commit, which `check-test-buckets.js` already forces. The line
count of `test-board-headless.js` is the measure: 30,501 today.

### The check that stops reaching

`scripts/check-board-imports.js`, the sibling of `scripts/check-imports.go`, parses `import` lines with a regex (no
bundler, no parser dependency, `import` statements are the line start) and enforces the layer table:

- a file under `pages/<a>/` may import `core/`, `ui/` and `pages/<a>/`, never `pages/<b>/` and never `shell/`
- `core/` imports only `core/`, `ui/` imports only `core/` and `ui/`
- only `shell/` imports `pages/<x>/index.js`, and only that file
- a file may not read `window.<name>` or a bare global that is not on the allow list (`document`, `location`,
  `Terminal`, ...). The list is the whole point and starts as today's cross-page names.

Before any module exists (cut 1) it runs on the classic scripts instead: it reads each file's declarations, reads its
references to other files' declarations, maps files to pages through `test-buckets.json`, and compares the page to page
edge counts against a checked-in baseline, `scripts/board-deps.json`. It fails when an edge is new or a count rose. The
baseline is allowed to fall (the check asks for the lower number to be written back, as `flaky-sync` does for its
list), so the number only goes one way.

## 5. The order of cuts

Every step leaves `claude/main` green, does one thing and lands by itself. Steps 1 to 3 change no product behaviour and
no module. Step 5 is the first page and the go/no-go. Page cuts (6) are independent of each other once step 5 is in.

1. **Pin the coupling. No code moves.** Add `scripts/check-board-deps.js` and `scripts/board-deps.json`: the page to
   page
   reference counts of section 1 (the table, per edge), the 348 `typeof` guards, the 17 `window.x =` assignments and the
   per-page `getElementById` ids, with the ratchet above. It runs from `check-board.sh`. Lands green by construction
   because it only describes what is true today, and from here no step can add an edge. Mapping uses
   `test-buckets.json`, which `check-test-buckets.js` already keeps complete.
2. **Write the page manifests. No code moves.** One `scripts/board-pages.json`: per page its name, view id, the API
   paths
   from a grep of its files, and the stream events it uses. Add `check-board-pages.js` that fails when a page's files
   call a
   path the manifest lacks. This is the data the fake API and the route table are built from, and it finds the cases
   where one file calls another page's endpoints (a coupling the first check cannot see).
3. **Serve the board as files in the tests.** Change `scripts/lib/mock.js:384` and `scripts/board-source.js` so the test
   server serves `index.html` and `/js/*` and `/css/*` as the embed FS does, and make `check-board.sh` parse each file
   alone as well as the concatenation. Behaviour is unchanged because the files are the same, but it is the step that
   makes modules possible, and it proves the 278 units do not depend on the inlined form. If a unit does (a timing that
   shifts with 100 requests against one), it is found here and not in a page cut. Run all units twice in CI on this
   step.
4. **`boot.js` becomes the module entry.** Change the last script tag to `type="module"`, keep every other script
   classic, add `js/package.json`. A module sees classic `let`/`const` and `function` names by bare name, and runs
   after every classic script, so `boot.js` still sees the whole board and the TDZ rule now holds by construction.
   `boot.js` is 195 lines, so a strict-mode failure is found on the smallest file. Add the `window` bridge file
   `js/shell/bridge.js` here, empty.
5. **Core as modules, with the bridge.** `core/api.js`, `core/esc.js`, `core/store.js` (with `lastTasks` wrapped, the
   card poll as its writer, and a classic getter so the 24 readers keep working), `core/prefs.js`. Each exports and
   `bridge.js` assigns it to `window`. No page changes yet. The unit tests for `store`, `prefs` and the `esc`/`setHTML`
   pair
   are the first node tier.
6. **Pilot page: `hubrepos` with `changereq`.** Chosen because it is the least tangled and has the most structure: 1,287
   lines, 12 and 50 outgoing references, 46 of which are the two files calling each other, its own sub-routes
   (`#/hubrepos/
   requests/open/<id>`, `route.js:20`), a bucket file already (`scripts/board-tests/hubrepos.js`, `changereq.js`, 900
   lines) and a mock already (`js/changereq-mock.js`). The step: move the files to `pages/hubrepos/`, add
   `index.js` with the manifest, move its stream events out of `settings-spine.js` onto `core/stream.js` (this also
   lands
   `core/stream.js`, scoped to these events), add the harness and the fake API for this page, move its units to run in
   the harness, and add `check-board-imports.js` in its enforcing form for this page alone.

   **Go/no-go after this step.** Go if all four hold: the page's units run in the harness with no full board boot and
   in a median of 3 s or less a unit (today `changeReq` is 32 s), the page-to-page baseline fell and was written back,
   the stream reconnect test passes, and `bootClean` and `route` are unchanged and green. No-go if the harness needs
   more than the page's own imports to mount, or if a module version of the page ships a request waterfall that shows
   in the `bootClean` time: then stop at the bridge and keep what steps 1 to 5 bought, which is the ratchet and the
   manifests.
7. **The small pages, one per step, in the order of fewest outgoing references:** `backlog` (341 lines), `merge`,
   `homes`, `launch`, `chat`, `pulls` (820), `items` (1,206, with `items-mock.js` and `items-data.js`), `usage` (2,287,
   8 files), `docs-files`. Each step is the pilot's recipe: move, manifest, stream registration, fixtures, units,
   tighten
   the baseline. Each can land alone in either order, and each leaves the harness smaller by its units.
8. **The shell.** `switchView` (`board.js:478`) is rewritten to read the registry, `VIEWS` becomes derived, and the 14
   `typeof` calls it makes go. The topnav (`topnav.js`) and `route.js` take the registry. This is done after four or
   five
   pages, because the registry has to hold what the pages are really like.
9. **The big pages, in the order cards, growls, settings, rooms.** Each is 3,900 to 5,100 lines and 139 to 334 outgoing
   references, so each splits in two or three steps along its files (growls: `notify`, `toasts`, `keepalive`, `growl`
   first). `settings-spine.js` is split here, and its stream and poll go to core, so this is where the dozen `es.onopen`
   calls end.
10. **Terminals last, in four steps: `terminal-list`, `terminal-links` and `termfull`, `terminal` itself, the
    popout.** 28 files, 17,223 lines, 761 outgoing references, and the landmines in `web/AGENTS.md` (the paste frame,
    `e.currentTarget`, the scroll guard `fromTerminal`, the popped-out window rule) live here. **This is the riskiest
    cut.** It is last so that everything it leans on is already a module with a store and a manifest, and it has a
    second go/no-go: the terminal units (49 units, 528 s) must be green in the harness for two consecutive main runs
    before
    the next terminal step lands, and any step that moves `terminal.js` is held to the paste and attach units with
    `HEADLESS_CPU_THROTTLE=4` as well.
11. **Retire the bridge.** Delete `bridge.js`, replace the 441 inline handlers with `data-action` delegation per page
    (one
    page per commit), and remove the `typeof` guards that remain. The check from cut 1 reads zero. `new Function` stubs
    in
    the old `test-*.js` are replaced by imports.
12. **Retire the harness.** What is left of `test-board-headless.js` is the smoke set, renamed `scripts/board-smoke.js`.
    `board-suite-weights.json` is cut to it. The 46 units on `board-visual-units.txt` are re-assessed: each moved into
    its page
    where the fake clock makes it deterministic, and the rest stay. CI shards the smoke set across the OSes and runs
    each page
    on one job.

### Which step is the riskiest, and why

Step 10, terminals, for size. Step 3 is the riskiest one that happens early, because it is the only step that changes
what 278 units run against, and its failure mode (a timing unit that passed on one 400 KB string and fails on a hundred
files) is exactly what the flaky list is made of. That is why it runs the suite twice and lands before any page moves.

## 6. Risks and what is out of scope

**Risks**

- **The bridge hides a missed edge.** While `bridge.js` exists a page can still reach `window.x`. The baseline from cut
  1
  catches a new one, and cut 11 removes the cause.
- **The stream moves are the quiet ones.** `es.onopen` ordering is not written down anywhere. Cut 6 writes the
  reconnect test first, and an order change is a separate commit.
- **A load-time change shows only in CI.** Modules defer, and a unit that read the DOM at a fixed point may fire earlier
  or later. Cuts 3 and 4 are small for this reason.
- **The harness can become a second mock.** A fake that drifts from the daemon passes while the board fails, which is
  the same hazard `scripts/test-seen.js` has with its stubs. The answer is the manifest check from cut 2 plus a Go test
  that the fake's paths exist in the room's mux, and one `smoke` unit per page against the whole board.
- **Strict mode.** Cut 4 finds the first throw, and every page cut re-runs `node --check` per module.
- **Service worker.** `sw.js` (350 lines) names no JS file, so a module graph does not change what it caches. Cut 4
  confirms that by test (`scripts/test-sw-openboard.js`).
- **Churn.** The plan moves about 70,000 lines across 12 or more steps. The ratchet means a step cannot be half done
  silently, and the rule "one page per step, files moved with `git mv` and no edit in the same commit" keeps `git blame`
  usable.
- **Parallel work.** Anyone editing `board.js` or `settings-spine.js` during cuts 8 and 9 conflicts with them. Those two
  are
  announced and landed fast, as the Go plan does for its mass edits.

**Out of scope**

- **The phone board, `m/`.** 7,409 lines and its own units (`m-home`, `m-card`, ...), already separate with its own
  scripts. Its tests fail together (the seven `phone*` rows) and would gain from the same harness, but it is a second
  plan
  after cut 12, and it takes this plan's `core/` only if a file is genuinely shared (the five shared tags at
  `index.html:3961` to `:3965`).
- **A framework, a virtual DOM or a reactive library.** The store is 60 lines and is not a framework.
- **CSS.** 30 files already, one per page by name. A page's stylesheet is linked by the shell when the page registers,
  later, and not here.
- **Rewriting any behaviour.** A cut moves code. The landmines in `web/AGENTS.md` are kept as comments on the lines
  they belong to.
- **`read.html`, `share.html`, `down.html` and `sw.js`.** Untouched except as named above.
- **The Go side**, including the room's HTTP surface (`internal/api`). The only Go-side step is the optional test that
  the
  fake's paths exist in the mux.
- **Changing what CI runs.** The groups, the shards and `flaky-suite.json` stay as they are until cut 12.
