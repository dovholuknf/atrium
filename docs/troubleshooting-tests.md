# Troubleshooting a flaky board test

How the `resumeSpinner` headless flake was found, written down so the next flake is a procedure and not a night. The
rule under all of it: **a fix comes from a trace of the failing run, never from reading the code and guessing.** A
guess that cuts a flake from 6 in 12 to 3 in 12 has not found the cause, it has narrowed one window and left the
other. The first attempt on this one did exactly that.

## 1. Reproduce before anything else

Run the one unit in a loop and count. Six to twelve runs is enough to know the rate.

```powershell
$env:HEADLESS_UNITS='resumeSpinner'
for ($i=1; $i -le 12; $i++) { node scripts/test-board-headless.js | Out-Null; if ($LASTEXITCODE) { "run $i FAIL" } else { "run $i ok" } }
```

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

- Do not widen a timeout. The boot restore fires on its own clock, so a longer wait moves the window, it does not
  close it.
- Do not add a guard where the symptom shows (`reconcileAttached`) without a trace saying that is the only closer.
  That was the first attempt, and the second closer was in a different file.
- Do not infer the caller from the assertion. Three different functions can blank one line.
