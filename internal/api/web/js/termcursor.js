// THE CURSOR, HELD UNTIL THE RUNNER'S OUTPUT SETTLES, for a runner whose
// profile asks for it.
//
// Codex on Windows reaches the board through ConPTY, and ConPTY splits one of
// codex's frames into two writes. The first ends with the cursor shown wherever
// the last cell was drawn, which while codex is working is one of the dots it
// animates across its input box. The second, a couple of milliseconds later,
// moves it back to the prompt. The board paints between the two, so the cursor
// jumps across the input box several times a second. Measured on a live codex
// card: 43% of its cursor shows land somewhere other than the prompt, and the
// next write corrects them in 2ms at the median and 13ms at the 90th
// percentile.
//
// Claude never does this, because it moves the cursor to its prompt right
// before every show. That is claude's own habit, not a rule here, so a runner
// whose profile says nothing is written straight through.
//
// So: the runner's own show and hide are tracked, not obeyed. After each write
// the cursor is hidden, and shown again once no output has arrived for the
// profile's settle time, if the runner's last word was to show it. The bytes
// the runner sent are written unchanged. See `runnerprofile` in Go for which
// runners set this and why.

// What the runner last asked for, and the pending reveal.
let cursorWanted = true;
let cursorTimer = 0;

// cursorSettleMs is this socket's settle time, 0 when the runner writes its
// cursor where it means it.
function cursorSettleMs() {
  const n = +(termCaps && termCaps.cursor_settle_ms);
  return n > 0 ? n : 0;
}

// cursorReset forgets the last socket's state. Called when a socket opens.
function cursorReset() {
  cursorWanted = true;
  clearTimeout(cursorTimer);
  cursorTimer = 0;
}

// lastCursorMode scans a chunk for the last DECTCEM, `ESC [ ? 25 h` or `l`,
// and answers true, false, or null when the chunk has neither. A sequence
// split across two chunks is missed, and the next frame's own show or hide
// corrects it.
function lastCursorMode(bytes) {
  for (let i = bytes.length - 6; i >= 0; i--) {
    if (bytes[i] === 0x1b && bytes[i + 1] === 0x5b && bytes[i + 2] === 0x3f &&
        bytes[i + 3] === 0x32 && bytes[i + 4] === 0x35) {
      if (bytes[i + 5] === 0x68) return true;
      if (bytes[i + 5] === 0x6c) return false;
    }
  }
  return null;
}

// writeRunnerOutput writes one chunk of runner output, holding the cursor for
// a runner that needs it. `done` is the write callback, as `term.write` takes.
function writeRunnerOutput(t, data, done) {
  const settle = cursorSettleMs();
  if (!settle) {
    t.write(data, done);
    return;
  }
  const bytes = typeof data === "string" ? null : data;
  const mode = bytes ? lastCursorMode(bytes) : null;
  if (mode !== null) cursorWanted = mode;
  // In the same write queue as the chunk, so no frame is painted between the
  // runner's show and this hide.
  t.write(data);
  t.write("\x1b[?25l", done);
  clearTimeout(cursorTimer);
  // Its own id only: a hidden terminal's timer fires with the showing terminal's globals, and must not lose its id.
  const mine = setTimeout(() => {
    if (cursorTimer === mine) cursorTimer = 0;
    if (cursorWanted && t === term) t.write("\x1b[?25h");
  }, settle);
  cursorTimer = mine;
}
