Docs: none, a test-only change. Nothing a reader names changed.

## What and why

`pasteBig` failed on macos CI run 39000703278 (shard 4, 79 units on 4 cpus at 97% load), and passed alone on the
retry. Two gestures failed: "a big paste then a small one" with `{first:2,firstFrame:0,span:160}` and "popped-out
ctrl-v 1MB" with `{first:9,firstFrame:0,span:290}`. The test sampled the `#t-pasting` box on every animation frame
for 800 ms, and took the span as the last frame that saw the box minus the first.

The trace is through the product. A 1 MB paste holds the box for 1024 ms (`pasteHoldFor` in
`internal/api/web/js/terminal-links.js`). Every caller of `pasteEnd` was read: the socket closing (the mock never
closes it), the cap (20 s), the terminal changing (the `moved` gesture, later in the unit), output (only once
`lastOut >= holdUntil`), the room's word (the mock never sends `in-done`), and the paste box, upload and history
paths the unit never takes. Nothing can take the box down before 1024 ms. So a span of 160 or 290 means the
renderer produced no frame between that moment and the window's end: the box was up, and nobody looked. The
locally recorded baseline is 49 frames per gesture at 16 ms with the box on all of them.

The fix makes the test observe the product's state rather than the renderer's:

- a `MutationObserver` on the terminal pane records the moment the box's `hidden` attribute flips, delivered right
  after the task that flipped it. The span is first paint to the first moment the box was seen down, or the window's
  end when it is still up. A second paste hiding and showing the box in one task is no flip, because the observer
  sees a task's end state.
- the first paint is still read on frames, because that claim is about a frame.
- the failure carries `frames`, `gap` (the longest wait between two frames) and `flips`, so the next failure says
  whether the box went down or the renderer stopped.

The 300 ms was not widened. Locally the unit passes 3 of 3 plain and under load, with the span at the window's
end and no down flip, as the trace predicts.

## Test plan

## @LETTER@. The paste box's hold is read from the DOM

### @LETTER@1. The unit passes under load

1. Start a dozen busy `node -e 'for(;;){}'` processes.
2. Run `HEADLESS_UNITS=pasteBig node scripts/test-board-headless.js`.

**Expected:** the unit passes. A failure, if any, prints `frames`, `gap` and `flips` beside `span`.

### @LETTER@2. A box that goes down early still fails

1. In a scratch copy of `internal/api/web/js/terminal-links.js`, make `pasteHoldFor` return 100.
2. Run the unit.

**Expected:** every big gesture fails with a `flips` entry showing the box down near 100 ms and a span under 300.
