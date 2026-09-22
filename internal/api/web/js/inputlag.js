// ── opt-in: how long a keystroke takes to come back ─────
//
// OFF UNLESS ASKED FOR, and when off every hook below is one boolean test.
// Switched on from settings ("log terminal input lag") or in the console:
//
//     localStorage.setItem("atrium.debug.inputlag", "1")
//
// What it prints, all under `[inputlag]`, is enough to tell the three causes
// apart without anybody describing the lag:
//
//   the WIRE      the echo came back late but the page was idle
//   the BOARD     a long task or a frame gap overlapped the keystroke
//   a STORM       the fetch cap was full or queued when the key was slow
//
// A keystroke is timed from xterm's `onData` to the frame after its echo is
// parsed. Only the FIRST unanswered keystroke is timed, the same rule the hub
// and room use, so the three logs describe the same wait. Keys typed while one
// is pending are counted, not timed. See docs/input-lag-logging.md.

const LAG_KEY = "atrium.debug.inputlag";
// A keystroke slower than this is logged as a warning with the fetch counts,
// rather than as a debug line.
const LAG_SLOW_MS = 100;
// A main-thread block at least this long is logged. 50ms is the long-task
// definition, and it is the point typing starts to feel sticky.
const LAG_STALL_MS = 50;
// How often the rolling summary prints, and how many samples it keeps.
const LAG_SUMMARY_EVERY = 10000;
const LAG_WINDOW = 300;
// Past this with no output at all, a keystroke is dropped from timing.
const LAG_NO_ECHO_MS = 5000;

let lagOn = false;
try { lagOn = localStorage.getItem(LAG_KEY) === "1"; } catch (e) {}

let lagPending = null;
let lagSamples = [];
let lagFresh = 0;
let lagStalls = [];
let lagTimer = 0;
let lagObserver = null;
let lagRaf = 0;

// Wall time with milliseconds, so a line here lines up with the hub and room
// logs, which print the same shape.
function lagClock() {
  return new Date().toISOString().slice(11, 23);
}

function lagFetches() {
  return `fetches ${apiInflight}/${API_MAX_INFLIGHT} in flight, ${apiQueue.length} queued`;
}

// Called by `onData` before the keystroke is sent. Returns the start time, or
// zero when nothing is being timed.
function lagKeyDown(report) {
  if (!lagOn || report) return 0;
  const now = performance.now();
  // A key that draws nothing, a modifier chord a TUI ignores, never gets an
  // echo. Without this the one pending sample would block timing for good.
  if (lagPending && !lagPending.echo && now - lagPending.t0 > LAG_NO_ECHO_MS) {
    console.debug(`[inputlag] ${lagClock()} no output within ${LAG_NO_ECHO_MS}ms of a key, not timed`);
    lagPending = null;
  }
  if (lagPending) { lagPending.coalesced++; return 0; }
  return now;
}

// Called after the frame has been handed to the socket.
function lagKeySent(t0, bytes) {
  if (!t0) return;
  lagPending = {
    t0, sent: performance.now(), bytes, coalesced: 0,
    buffered: termSock ? termSock.bufferedAmount : 0,
    fetches: lagFetches(),
  };
}

// Called with the write callback `onmessage` was going to use. When a
// keystroke is waiting, the echo is stamped now and the callback is wrapped to
// stamp the parse and the paint. Otherwise it is returned untouched.
function lagOnOutput(done) {
  if (!lagOn || !lagPending || lagPending.echo) return done;
  const s = lagPending;
  s.echo = performance.now();
  return function () {
    s.parsed = performance.now();
    if (done) done.apply(this, arguments);
    requestAnimationFrame(() => { s.paint = performance.now(); lagFinish(s); });
  };
}

function lagFinish(s) {
  if (lagPending === s) lagPending = null;
  const total = s.paint - s.t0;
  const stalled = lagStalls
    .filter(x => x.end > s.t0 && x.start < s.paint)
    .reduce((n, x) => n + Math.min(x.end, s.paint) - Math.max(x.start, s.t0), 0);
  lagSamples.push(total);
  if (lagSamples.length > LAG_WINDOW) lagSamples.shift();
  lagFresh++;
  const f = n => n.toFixed(1);
  const line = `[inputlag] ${lagClock()} key ${f(total)}ms` +
    ` (send ${f(s.sent - s.t0)}, wire ${f(s.echo - s.sent)}, parse ${f(s.parsed - s.echo)},` +
    ` paint ${f(s.paint - s.parsed)})` +
    (stalled ? ` | main thread blocked ${f(stalled)}ms of it` : "") +
    (s.coalesced ? ` | ${s.coalesced} more keys while waiting` : "") +
    (s.buffered ? ` | socket had ${s.buffered} bytes unsent` : "");
  if (total >= LAG_SLOW_MS) {
    console.warn(line + ` | at send: ${s.fetches} | now: ${lagFetches()}`);
  } else {
    console.debug(line);
  }
}

function lagPercentile(sorted, p) {
  return sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * p))];
}

function lagSummary() {
  if (!lagFresh || !lagSamples.length) return;
  lagFresh = 0;
  const s = lagSamples.slice().sort((a, b) => a - b);
  console.info(`[inputlag] ${lagClock()} last ${s.length} keys:` +
    ` p50 ${lagPercentile(s, 0.5).toFixed(1)}ms,` +
    ` p95 ${lagPercentile(s, 0.95).toFixed(1)}ms,` +
    ` max ${s[s.length - 1].toFixed(1)}ms`);
}

function lagStall(start, dur, how) {
  lagStalls.push({ start, end: start + dur });
  const cutoff = performance.now() - 30000;
  while (lagStalls.length && lagStalls[0].end < cutoff) lagStalls.shift();
  console.warn(`[inputlag] ${lagClock()} main thread blocked ${dur.toFixed(0)}ms (${how})` +
    ` | ${lagFetches()}`);
}

// A LONG TASK where the browser reports them, a frame gap where it does not.
// Both at once would log every block twice.
function lagWatchStalls() {
  try {
    if (PerformanceObserver.supportedEntryTypes.includes("longtask")) {
      lagObserver = new PerformanceObserver(list => {
        for (const e of list.getEntries()) {
          if (e.duration >= LAG_STALL_MS) lagStall(e.startTime, e.duration, "long task");
        }
      });
      lagObserver.observe({ type: "longtask", buffered: false });
      return;
    }
  } catch (e) {}
  let last = performance.now();
  const tick = now => {
    // A hidden tab gets no frames at all, which is not a stall.
    if (!document.hidden && now - last >= LAG_STALL_MS + 17) {
      lagStall(last, now - last, "frame gap");
    }
    last = now;
    lagRaf = requestAnimationFrame(tick);
  };
  lagRaf = requestAnimationFrame(tick);
}

function lagStart() {
  lagWatchStalls();
  lagTimer = setInterval(lagSummary, LAG_SUMMARY_EVERY);
  console.info(`[inputlag] ${lagClock()} on. keystrokes, stalls and a summary every ` +
    `${LAG_SUMMARY_EVERY / 1000}s print here. switch off in settings.`);
}

function lagStop() {
  if (lagObserver) { lagObserver.disconnect(); lagObserver = null; }
  if (lagRaf) { cancelAnimationFrame(lagRaf); lagRaf = 0; }
  clearInterval(lagTimer);
  lagTimer = 0;
  lagPending = null;
  lagSamples = [];
  lagStalls = [];
}

function toggleInputLag(on) {
  if (!!on === lagOn) return;
  lagOn = !!on;
  try { localStorage.setItem(LAG_KEY, lagOn ? "1" : "0"); } catch (e) {}
  if (lagOn) lagStart(); else { lagStop(); console.info("[inputlag] off"); }
}

if (lagOn) lagStart();
