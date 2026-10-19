# Troubleshooting tests

Worked cases, each written down so the next one is a procedure and not a night. Part 1 is a board unit that flakes
locally. Part 2 is a Go test that is slow only on a hosted runner. Part 3 is a board unit whose timing assertion
fails only under a loaded shard. Part 4 is two Go shapes: a test that fails only beside others, and a goroutine
that logs after its test ended. Part 5 is the sharded board run on CI: reading its artifacts, reproducing a
Linux or macos red on a Linux box, placing a stray paint between a test's own steps, and proving a change to the
harness by forcing the failure it reports. The rule under all of them: **a fix comes from a trace of the failing
run, never from reading the code and guessing**, and a timeout is never widened to make a number go away.

# Part 1: a board unit that flakes locally

How the `resumeSpinner` headless flake was found. The
rule under all of it: **a fix comes from a trace of the failing run, never from reading the code and guessing.** A
guess that cuts a flake from 6 in 12 to 3 in 12 has not found the cause, it has narrowed one window and left the
other. The first attempt on this one did exactly that.

## 1. Reproduce before anything else

Run the one unit in a loop and count. Six to twelve runs is enough to know the rate.

```powershell
$env:HEADLESS_UNITS='resumeSpinner'
for ($i=1; $i -le 12; $i++) { node scripts/test-board-headless.js | Out-Null; if ($LASTEXITCODE) { "run $i FAIL" } else { "run $i ok" } }
```

To reproduce a flake that only shows on a slow CI runner, add `HEADLESS_CPU_THROTTLE=4` (any rate above 1). Every
page and popup then runs that many times slower, through the browser's own CPU throttle, and the run prints a line
saying so. Waits are not scaled, so a throttled run still hits the same deadlines. With the variable set a missing
playwright fails the run instead of skipping it.

Note the failure text and the line in `scripts/test-board-headless.js` that produced it. The assertion tells you WHAT
was wrong (the pane said nothing). It never tells you WHO did it. That is what the trace is for.

## 2. Instrument the test, not the product

Everything below goes in the test file behind an environment variable (`SPIN_DEBUG=1` here) and is removed before the
commit. The product is never edited to find a bug, because a product edit changes the timing you are trying to see.

**Make the mock say who touched it.** The test's fake WebSocket is a plain object. Its `close()` records a timestamp
and a stack:

```js
close() { this.readyState = 3; (window.__trace = window.__trace || []).push({ t: performance.now(), what: "ws.close", stack: new Error().stack }); }
```

Do the same for `new` and `open`. `performance.now()` rather than `Date.now()`, so the entries sort within a
millisecond.

**Wrap the board's own functions after the page loads.** The board's scripts are plain `function` declarations, so
each one is a property of `window` and can be replaced from `p.evaluate` without touching the source. The wrapper
records what it was called with, then calls the original:

```js
await p.evaluate(() => {
  const tr = (what, extra) => window.__trace.push(Object.assign({ t: performance.now(), what }, extra || {}));
  const orig = reconcileAttached;
  window.reconcileAttached = function (tasks) {
    tr("reconcileAttached", { tasks: tasks.map(t => t.id + ":" + (t.supervised ? "sup" : "un")),
      termTask: termTask && termTask.id, inFlight: attachInFlight, stack: new Error().stack });
    return orig(tasks);
  };
});
```

Start with the function the first trace points at and its callers (`clearTermPane`, `openTerm`, `loadCards`). When a
trace shows no close at all and the symptom is still there, widen to the function that paints the symptom
(`termWait` here: the line went blank because something called it with an empty string, not because the pane went).
Record the ARGUMENTS that decide the branch: the task list with each card's `supervised` flag, the attached card, the
in-flight mark, the opening state. A stack says who called. The arguments say why it took the branch it took.

Wrap after the page booted, not in `addInitScript`: the functions do not exist until their script has run.

**Log the network from outside the page.** `p.on("response")` prints each answer that matters with the one field that
decides the branch:

```js
p.on("response", async r => {
  const u = new URL(r.url()).pathname;
  if (u !== "/v1/tasks" && u !== "/v1/launch") return;
  const j = JSON.parse(await r.text()); const l = (j.tasks || [j]).find(t => t.id === "land-res");
  console.log("  RESP " + Date.now() % 100000 + " " + u + " land-res=" + (l ? (l.supervised ? "sup" : "un") : "absent"));
});
```

This lines up what the daemon said against what the page was holding when it decided.

**Dump only on failure.** At the assertion that fails, when the env var is set, print `window.__trace` in time order
with the first dozen frames of each stack. Then loop until a run fails and keep that run's output in a file:

```powershell
$env:SPIN_DEBUG='1'
for ($i=1; $i -le 20; $i++) { $out = node scripts/test-board-headless.js 2>&1 | Out-String
  if ($LASTEXITCODE) { $out | Out-File "$env:TEMP\fail.txt"; break } }
```

## 3. Map the stack back to source

Stacks point into the served page (`http://127.0.0.1:PORT/:62207`), which is every board script concatenated. The
mock exports that page, so write it out and read around each frame:

```bash
node -e 'const {HTML}=require("./scripts/lib/mock.js");require("fs").writeFileSync("board.html",HTML);process.exit(0)'
sed -n 62204,62208p board.html
```

An async boundary (`at async renderTerms`) drops the caller. Wrap the function BEFORE the first `await` (here
`renderTermList`, called synchronously by `renderTerms`) to get the frames above it.

## 4. Read the timeline, not the code

Lay the entries out in order and look for the one that changed state against the test's expectation. On this flake:

```
950.9  ws.open                                         in-flight mark cleared
1112.8 loadCards      <- runPass <- runRefresh <- boot.js setTimeout   the 500 ms boot pass
1114.3 reconcileAttached tasks=[] termTask=land-res inFlight=""        the cache still says unsupervised
1114.3 clearTermPane  <- reconcileAttached <- renderTermList <- renderTerms <- repaintLists <- switchView <- restoreWhereYouWere
1114.5 ws.close
1118.7 loadCards.done rows=[land-res:sup]                              the fresh list, 4 ms too late
```

Two facts came out of that which no amount of code reading had produced: the closer was the boot-time restore that
fires half a second after load, and the list it painted from was the card cache, not a polled response. The cache
never learned what `/v1/launch` had answered.

After fixing that, the trace with `termWait` wrapped showed the residual failure as a different thing entirely: no
close, no teardown, just `termWait("")` from `waitLoop`, the restore's own wait loop blanking a line it never wrote.

## 5. Decide: product or test

Ask whether the window exists on a real daemon. Here `/v1/launch` answers before its own `task` event, which is
coalesced for 200 ms (`taskCoalesce` in `internal/api/api.go`), and the socket opens in a few milliseconds on
loopback. So the real board has the same window on every resume, only narrower. Product bug. A test race is when the
mock does something no daemon does, and the fix is to make the test wait on the state it is asserting.

## 6. Fix, then re-run with the trace still armed

Twenty runs clean, then twenty under load (eight busy `node -e 'for(;;){}'` processes, which is what CI feels like).
Keep the env-var instrumentation on during those runs: a failure then explains itself instead of costing another
round. Remove it before the commit.

## What not to do

- Do not add a flat wait. A test waits by spinning on an observable condition in short steps inside ONE overall
  deadline taken from `slow()`. A "nothing happens" check waits for a sentinel (something that must happen after it,
  `watchRequests(...).waitFor(n)`) or moves the fake clock, and a flat wait that has to stay carries a one-line
  `// wait: <why>` comment. `scripts/check-test-waits.go` fails when a file's count rises.
- Do not widen a timeout. The boot restore fires on its own clock, so a longer wait moves the window, it does not
  close it.
- Do not add a guard where the symptom shows (`reconcileAttached`) without a trace saying that is the only closer.
  That was the first attempt, and the second closer was in a different file.
- Do not infer the caller from the assertion. Three different functions can blank one line.

# Part 2: a test that is slow only on CI

How `internal/ptyhost.TestStartRunsADetachedHostFromACopy` went from "11.73 s on the Windows runner, 5 s guard" to
a cause with evidence. The rule here: **a number from CI is a symptom, and the cause is found by splitting the time
into phases until one phase owns it.** Then prove the phase's cause with an experiment that cannot be read two ways.

## 1. Get the numbers from the artefacts, not from memory

Every run uploads `build.claude/ci`, and `timings.sorted.tsv` in it has every test's seconds. Pull three runs, not
one: a test that is 11.7, 12.4 and 7.9 s is slow for a reason, a test that is 11.7 once is noise.

```
atrium_ci action=artifact repo=dovholuknf/atrium run_id=39000703278 name=ci-windows-latest-go-test-attempt-1
```

Look at the neighbours in the same file. Here `internal/detach.TestStartOutsideJobAnswersTheProgramNotCmd` sat at
13.9, 9.9 and 6.7 s in the same runs, and both tests start an executable that did not exist a moment before. Two
slow tests with one thing in common is a lead. One slow test is a guess.

## 2. Phase-log the test, and keep the lines

`t.Logf` with `time.Since(start)` at every boundary the test crosses: after `Start`, after the dial, after the probe.
These are not instrumentation to remove. They cost nothing, and the next time the guard fires the log says which
phase grew instead of only the total. The ptyhost test now prints:

```
started after 1.77s (a fresh copy's first start is the virus scan)
dialed after 1.82s
probed after 1.82s, in a job: true
```

Run it locally. If the phases add up to the local total and one of them is most of it, that phase is the subject.
Here `Start` was 2.2 to 3.4 s of a 6 s test, and the dial landed 50 ms after it.

## 3. Isolate the phase with a throwaway experiment

Write a test file you will delete (`zz_trace_integration_test.go`) that does only the suspect phase, in
variations that tell the candidates apart:

- the original executable: 11 ms to start
- a fresh copy, first start: 3.39 s. The same copy again: 20 ms
- a second fresh copy: 2.8 s. Again: 19 ms
- a hard link to the original: 1.23 s

"First start of a never-seen file is slow, the same file again is fast" is one thing: something inspects a new
executable before CreateProcess returns, and it is content-keyed. Defender's real-time protection is the only thing
on a Windows box that does that. A hard link paying too says it is not the copy.

## 4. Prove it where it fails, in the log

Local evidence says what THIS machine does. The runner is another machine, and "GitHub's Windows runner has Defender
on" is a belief until a run prints it. So the fix prints the evidence into the machine step of every Windows job
(`scripts/ci-defender.ps1`): real-time protection on or off, the exclusions already there, and the first and second
start of a fresh copy of go.exe. Then it excludes the folders the tests write binaries to, measures again, and prints
before and after. One run after the change answers the question for good, and every later run re-answers it.

The same script on the developer's box, as a sanity check of the probe itself: first start 4276 ms, second 30 ms.

## 5. Do not widen the test

The idle floor was tried at 1 s during the trace and put back to 3 s: the trace showed the scan does not count
against the idle clock (the host's clock starts in its own process, after the scan), so the floor was never the
problem. No allowlist line. The guard stays, the scan goes.

## What not to do

- Do not write a script that carries the exclusion cmdlet through a shell command line. Defender's behaviour
  signatures flag the command line itself (`Trojan:Win32/PowExcEnv.H!MTB`, 2026-10-17) and kill the shell, which
  looks like a random `EPERM` from the tool. Write the file with a file tool. The same text on disk is fine.
- Do not take one run's number. Take three and the neighbours.

# Part 3: a timing assertion that fails only under load

How `pasteBig` failed on macos CI (shard 4, 79 units on 4 cpus at 97% load) with `{first:2,firstFrame:0,span:160}`
and passed alone on the retry. The rule: **decide whether the test observes the product or the renderer, and when
it is the renderer, make it observe the product.** Never widen the number.

## 1. Read the failure as a measurement

The test sampled the paste box on every animation frame for 800 ms and took the span as the last frame that saw the
box minus the first. `span:160` says the last frame with the box up was at 162 ms. It does not say the box went
down at 162 ms. Those are two different claims, and the assertion conflates them.

## 2. Trace the product for every path to the symptom

The symptom is "the box was hidden". Find every caller of the function that hides it (`pasteEnd`) and ask of each
whether the test's mock can take that path before the product's hold (1024 ms for 1 MB) runs out:

| caller | can it fire before 1024 ms here |
| --- | --- |
| the socket closing | no, the mock never closes |
| the cap | no, 20 s |
| the terminal changing | no, that gesture comes later |
| output | no, only once `lastOut >= holdUntil` |
| the room's word | no, the mock never sends `in-done` |

Every row says no. So the box was up at 162 ms and stayed up, and the renderer produced no frame between 162 ms and
the window's end: the loaded machine starved `requestAnimationFrame`. The local baseline agrees: 49 frames per
gesture at 16 ms with the box on all of them.

## 3. Make the test read the state, not the paint

A `MutationObserver` on the pane records the moment the box's `hidden` attribute flips, delivered right after the
task that flipped it, however late the next frame comes. The span is first paint to the first moment the box was
seen down, or the window's end when it is still up. The first-paint claim stays on frames, because that claim is
about a frame.

Check the observer against the product's own coalescing: a second paste that hides and shows the box in one task
must not read as a flip. It does not, because the observer sees a task's end state. The run shows one `up` flip per
gesture and no `down`, including the pair gesture.

## 4. Put the evidence in the failure

The failure now carries `frames`, `gap` (the longest wait between two frames) and `flips`. The next CI failure
says on its own whether the box went down or the renderer stopped, and nobody has to reproduce 97% load on four
cpus to find out.

## 5. Prove the test still fails when the product is wrong

Cut the product's hold to 100 ms in the working tree, run the unit, and read the failure: a `down` flip near 100 ms
and a span under 300. Restore the file. A test that cannot fail has measured nothing.

## What not to do

- Do not reproduce for hours first. Six passes under eight burners and three under twenty said nothing: the load
  that starves a renderer is the shard's, not a loop's. The product trace in step 2 took minutes and settled it.
- Do not sample with a timer instead of frames. A starved main thread starves timers too. Observe the mutation.
- Do not widen 300 ms. The product holds 1024 ms. The test was reading the wrong clock.

# Part 4: two Go test shapes that look like flakes and are not

Both from `internal/ptyhost`, both found by the method above, both short.

## A test that fails only in a full-package run

`TestWrappedRingIsTruncatedWithRebasedCuts` passed 8 of 8 alone and failed once in the package. A test that passes
alone and fails beside others is a test whose premise holds only on a quiet machine. Find the premise: here, that
the runner's 64 KB burst had all landed when a poll first saw the offset pass 20000. Log the quantity the premise
is about (the offset at the resize and at the attach) and run under load until the log shows it false. Then make
the test wait for the event the premise names (the runner prints READY after its burst) instead of a number that
only correlates with it on a quiet box. The product's answer in the "failing" case was right: a cut older than
the ring is gone, and the test was asking for it back.

## "Log in goroutine after TestX has completed"

This panic ends the whole package run, so it reads as a crash and is reported as one. It is one goroutine calling
`t.Logf` after its test finished. Run the test with `-count=3`: the first repeat makes it reliable, because the
goroutine lands on the next copy's clock. The stack names the goroutine (`Host.await`) and the logger it reached
(`testHost`'s `Logf`). The cause is in the product when the thing being closed returns from Close before the
goroutines it started are done, and that is where the fix goes: count them, wait for them in Close, bounded, and
say so when the bound is hit. Muting the test's logger instead hides the same unfinished goroutine from every
caller that is not a test.

# Part 5: a board unit that is red on CI and green here

How eight sharded board reds across runs 39052523653 and 39054604921 were read, reproduced and fixed, one wait at a
time, on a Windows machine that never showed any of them. The rule here: **read the shard's own log, reproduce on
the operating system that failed, and when the fix is to the harness, prove it by forcing the failure it reports.**

## 1. Read the shard logs, not the summary

Every board job uploads its `build.claude/ci`. Pull the run, then read in this order:

```
gh run download <run id> -D <scratch dir>/art1
```

- `board-shards/shard-N.log` and `board-shards/retry-1.log` hold each unit's `FAIL:` lines with the message, the
  Playwright call log and the `(at unitFn (file:line))` trace. This is the only place the selector and the line are.
- `board.log` is the merged report. Under `FAILED` each unit shows its message. Before `sharded-fail-lines` only
  the first line was printed, so a timeout there read as `Timeout 30000ms exceeded` and nothing else.
- `failing.tsv` names units. A check that is not a unit, such as `scripts/check-board-drawer.js`, printed `FAIL`
  lines into `board.log` and never reached the tsv, so two macos jobs were red for two runs with nothing named in
  the summary. When the summary names nothing, grep `board.log` for `^FAIL`.

Then run the unit alone with `HEADLESS_TRACE=1`. The last `TRACE <unit> click(...) at <file:line>` printed before the
timeout is the call that hung, and a timeout's first line now carries the same call and line.

## 2. Reproduce on the operating system that failed

A unit that fails on the Linux or macos runner and passes 15 of 15 here is not reproduced yet. Playwright's own
image on a Linux docker (WSL does) runs the same suite against this worktree, bind-mounted, with the image's
browser and fonts. Six shell burners stand in for the shard load, so run this only on a machine set aside for load
tests, never on a room that other sessions share. Fifteen runs, FAIL and TIMING lines only:

```
docker run --rm -v /path/to/worktree:/work -w /work -e HEADLESS_UNITS=<unit> \
  mcr.microsoft.com/playwright:v1.64.1-noble bash -c '
    for i in $(seq 6); do ( while :; do :; done ) & done
    for i in $(seq 15); do node scripts/test-board-headless.js 2>&1 | grep -E "FAIL|TIMING"; done
    kill $(jobs -p)'
```

`groupBridge` reproduced 2 of 15 this way after 0 of 30 on Windows. Keep the in-page trace of Part 1 armed while it
runs: the run that fails explains itself, and a Linux repro of a macos red is close enough to read the same race.
A unit that still passes 15 of 15 here under load has not been reproduced, and the fix for it is the one Part 3
describes: make the test read the state it asserts, with the evidence in the failure.

## 3. Place a stray paint between the test's own steps

A unit that loops over fixtures inside one `p.evaluate` (`termBox` draws thirty two lists) can be hit by a paint it
did not ask for: a poll pass, a stream event, a boot render still waiting on its fetch. The wrapper from Part 1 says
who painted, but not WHEN relative to the loop, since the loop's steps leave nothing in the trace. Two additions:

- Push a marker from inside the loop, before the render and after the measurement, so each draw in the trace sits
  between two named steps: `window.__trace.push({ mark: "150 none/grouped/someHidden start", t: performance.now() })`.
- Record which function fed the paint. A test that replaces a page global (`boardCards = async () => cards`) and a
  render that captured the original before the swap draw different lists from the same code, so the wrapper records
  `String(boardCards).slice(0, 30)` beside the stack. The original shows as `async function boardCards(sign`, the
  override as the arrow.

On `termBox` this placed every empty-state draw before the loop's first marker, from the page's first poll pass
through the original function, which is harmless. The stale-draw theory was wrong, and no fix was written for it.

## 4. Prove a harness change by forcing the failure it reports

A change to how the suite reports (`printFail` in the sharded runner, `withTrace` and `threwMessage` in the
headless harness) has no unit of its own. Prove it by breaking one selector and reading the output:

1. In the unit, change a click's selector to one the page lacks: `.cr-li[data-id="cr_8"]` to `cr_nope`.
2. Run it alone: `HEADLESS_TRACE=1 HEADLESS_UNITS=changeReq node scripts/test-board-headless.js`.
3. Run it through the shards: `node scripts/test-board-sharded.js --local --no-retry --units changeReq`.
4. Restore the file (`git restore` it, never a stash).

Both outputs must name the selector and the line on the failure's first line, and the sharded one must show the
call log under it. Keep the two logs beside the change: they are its evidence, the way a trace is a fix's.

## 5. Where the LOAD reds come from

The sharded run uses one and a half shards per core, and a unit that fails in the shards and passes alone fails the
run by policy (`LOAD` in the report). Every run therefore reddens on a new handful of such units, two to five, and
the eight fixed so far all held. Each fix is a wait on the state the step asserts (`until()` in the drawer check,
`loaded || loadFailed` before reading the pulls, the hub stream before writing a rooms event). The pattern is in Part
3. What this part adds: the units on `scripts/board-visual-units.txt` do not redden the run, so a unit moved there
is one nobody will fix, and a unit split into single-purpose halves fails on the half that names the race.

## What not to do

- Do not read `failing.tsv` as the list of what failed. Read `board.log` and the shard logs.
- Do not call a unit reproduced because it failed once under load on the wrong operating system. Reproduce where it
  failed, or trace where it fails.
- Do not fix a theory. `termBox`'s empty draw had a tidy story and the trace placed it outside the loop.
- Do not widen the click's 30 seconds. The call that hangs is named now, so find out why it hangs.
- Do not expect the same unit to fail twice. The last ten failed runs on main held 27 distinct units and 22 of them
  failed once, so a green run after a fix says little. Count a fix by the wait it adds on the state the step asserts.
- Do not hunt for the line of a thrown unit. Its `failing.tsv` row now names the call and its line, as in
  `waitForSelector("#uc-body .ucfade") at usage.js:2072:14`. `HEADLESS_TRACE=1` prints every page action before it.

# The flaky suite: how a test enters and leaves

`scripts/flaky-suite.json` lists every test known to fail now and then. CI runs them first and alone
(`bash scripts/ci.sh --group flaky`, one job per platform), with no retry, and every other step skips them, so each
test runs in exactly one place. The other jobs of a platform wait for its flaky job and run even when it is red.

- **Enters** when it failed in at least 2 of the last 20 runs of the flake ledger
  (`node scripts/ci-flake-ledger.js report --window 20`), whether or not a fix names it. Add an entry with its kind
  (`go` with the package, or `board`), its name and a one-line reason. A fix entry is a commit subject that names the
  test, not proof that it held.
- **Leaves** after 30 quiet runs, counted in the ledger with the entry in place. Remove the entry in the same change
  that records the count.
- **A typo fails loudly.** `node scripts/flaky-suite.js check` runs in the `go-build` step and at the start of the
  flaky step, and fails on a name that no longer exists, since an empty `-run` would run everything.
- `scripts/test-touched.sh` runs a touched manifest test ahead of the rest of its plan.
- A manifest board unit that is also on `board-visual-units.txt` runs in the flaky job and fails it. That is the point
  of the move, and the unit is not run in board-visual.
