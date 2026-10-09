// ── a cleared context leaves a line ──────────────────────────────────────────
//
// The room writes OSC 7777;atrium-clear into the stream when `/clear` is typed (see `clearMarkOSC`, internal/daemon/
// screen.go), and the OSC handler in openTerm pushes the visible page into scrollback. This draws "context cleared" and
// the local time at that seam, between the kept page and the new screen.
//
// NOTHING IS WRITTEN INTO THE BUFFER. Like the new-since divider (js/lastread.js) it is an xterm marker with a
// decoration over it, so the live view and a replay hold the same rows, the cursor rows the runner relies on do not
// move, and copy and select return the same text around it. The marker follows its line through scrolling and
// resizes, and is disposed when scrollback trimming takes the line.
//
// ONE PER CLEAR. A reattach resets the terminal and replays the same bytes, so the handler draws them again: reset
// disposes what is there first, and a second mark at a row that already has one is ignored.
//
// THE TIME is the moment the mark was parsed, but only for a mark that arrived live. A replay's marks are old and the
// bytes do not carry when, so a mark parsed while a replay is landing says "context cleared" and nothing else. A
// terminal counts as live once its writes have been quiet for CL_QUIET after it was built or reset.

const CL_QUIET = 300;

function clTime(at) {
  try { return new Date(at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }); } catch (e) { return ""; }
}

function clState(t) { return t._cl || (t._cl = { live: false, divs: [], timer: 0 }); }

function clDropAll(t) {
  const C = clState(t);
  for (const d of C.divs.slice()) {
    try { d.dec.dispose(); } catch (e) {}
    try { d.marker.dispose(); } catch (e) {}
  }
  C.divs = [];
}

// Per terminal, once it is built.
function clInit(t) {
  const C = clState(t);
  t.onWriteParsed(() => {
    if (C.live) return;
    clearTimeout(C.timer);
    C.timer = setTimeout(() => { C.live = true; }, CL_QUIET);
  });
  const reset = t.reset.bind(t);
  t.reset = () => {
    clDropAll(t);
    clearTimeout(C.timer);
    C.live = false;
    return reset();
  };
}

// The first row of the new screen, now that the page above it has been kept.
function clMark(t) {
  const b = t.buffer.active;
  if (b.type !== "normal") return;
  const C = clState(t);
  const marker = t.registerMarker(-b.cursorY);
  if (!marker) return;
  if (C.divs.some(d => !d.marker.isDisposed && d.marker.line === marker.line)) { marker.dispose(); return; }
  const label = C.live ? "context cleared · " + clTime(Date.now()) : "context cleared";
  let dec;
  try { dec = t.registerDecoration({ marker, x: 0, width: t.cols, height: 1 }); } catch (e) {}
  if (!dec) { marker.dispose(); return; }
  dec.onRender(el => {
    el.classList.add("atrium-clearline");
    el.style.width = "100%";
    el.dataset.label = label;
  });
  const d = { marker, dec };
  C.divs.push(d);
  marker.onDispose(() => { C.divs = C.divs.filter(x => x !== d); });
}
