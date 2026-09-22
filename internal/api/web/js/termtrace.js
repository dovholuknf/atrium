// ── the last few seconds of a terminal, on request ──────
//
// A garble that shows up one time in twenty cannot be reproduced on demand, and
// by the time somebody describes it the bytes that caused it are gone. So every
// terminal keeps a rolling window of its raw attach traffic in BOTH directions,
// and "save terminal trace" in the cog downloads it. Click it the moment the
// pane looks wrong and the file holds the exact bytes that drew it.
//
// What goes in, each with a timestamp:
//
//   out   a binary frame from the daemon, exactly as xterm was handed it
//   in    a control frame the board sent: keystrokes, resize, echo, signal
//   ctl   something local that moves the cursor maths: the socket opening or
//         closing, a reset, and xterm's own grid changing size
//
// CHEAP WHEN NOBODY SAVES. An output frame is kept by reference, not copied,
// and the only work per frame is a push and, once the window is full, a shift.
// Nothing is encoded until the save.
//
// The window is bytes, not frames, so a burst of one-byte echoes does not push
// out the repaint that preceded it. See TRACE_BYTES.
//
// Replay a saved file with `node scripts/replay-term-trace.js <file>`.

// About a screenful of repaints plus the typing that led to them. A claude
// frame is a few kilobytes, so this holds the last dozen or so redraws.
const TRACE_BYTES = 64 * 1024;

let traceLog = [];
let traceSize = 0;
let traceT0 = 0;

// Starts a fresh window for a new terminal, and watches its grid. Called from
// `openTerm` once per xterm instance, so the listener dies with the terminal.
function traceTerm(t) {
  traceLog = [];
  traceSize = 0;
  traceT0 = performance.now();
  traceCtl({ ev: "open-term", cols: t.cols, rows: t.rows });
  t.onResize(s => traceCtl({ ev: "xterm-resize", cols: s.cols, rows: s.rows }));
}

function tracePush(d, data, size) {
  traceLog.push({ t: performance.now() - traceT0, d, data });
  traceSize += size;
  while (traceSize > TRACE_BYTES && traceLog.length > 1) {
    const old = traceLog.shift();
    traceSize -= old.data.byteLength != null ? old.data.byteLength : old.data.length;
  }
}

// A daemon frame. Binary arrives as an ArrayBuffer and text as a string.
function traceOut(data) {
  tracePush("out", data, data.byteLength != null ? data.byteLength : data.length);
}

// A frame the board sent, already serialised.
function traceIn(json) {
  tracePush("in", json, json.length);
}

function traceCtl(obj) {
  const s = JSON.stringify(obj);
  tracePush("ctl", s, s.length);
}

function traceB64(buf) {
  const u = new Uint8Array(buf);
  let s = "";
  for (let i = 0; i < u.length; i += 0x8000) {
    s += String.fromCharCode.apply(null, u.subarray(i, i + 0x8000));
  }
  return btoa(s);
}

// The file. The screen and cursor as xterm holds them NOW go in the header, so
// a replay can be checked against what the operator was looking at.
function traceSnapshot() {
  const b = term ? term.buffer.active : null;
  const screen = [];
  if (b) {
    for (let y = 0; y < term.rows; y++) {
      const line = b.getLine(b.viewportY + y);
      screen.push(line ? line.translateToString(true) : "");
    }
  }
  return {
    v: 1,
    task: termTask ? termTask.id : "",
    title: termTask ? (termTask.display_title || termTask.title || "") : "",
    kind: termKind,
    saved: new Date().toISOString(),
    agent: navigator.userAgent,
    cols: term ? term.cols : 0,
    rows: term ? term.rows : 0,
    cursor: b ? { x: b.cursorX, y: b.cursorY, baseY: b.baseY, viewportY: b.viewportY } : null,
    screen,
    frames: traceLog.map(e => typeof e.data === "string"
      ? { t: +e.t.toFixed(1), d: e.d, s: e.data }
      : { t: +e.t.toFixed(1), d: e.d, b64: traceB64(e.data) }),
  };
}

function saveTermTrace() {
  const snap = traceSnapshot();
  const blob = new Blob([JSON.stringify(snap)], { type: "application/json" });
  const a = document.createElement("a");
  const stamp = snap.saved.replace(/[-:]/g, "").replace(/\..*/, "");
  a.href = URL.createObjectURL(blob);
  a.download = `atrium-trace-${(snap.task || "term").slice(-8)}-${stamp}.json`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  toast("terminal trace saved", `${snap.frames.length} frames, ${a.download}`);
}
