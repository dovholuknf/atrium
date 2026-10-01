# Review of 6e088616 + 153fbb6f (@ui u-suite-webgl: no WebGL and calm in the suite, /m reloads on a new build)

Reviewed by @review, 2026-09-30, from `git diff claude/main...sg3/claude/u-suite-webgl`, read only. Hub side. Board
and headless checks are @ui's, and I ran none.

## What holds

- **`__atriumNoWebgl` is set only by the harness**, through `addInitScript`, before the page loads. `useWebgl`
  returns before it loads the addon, and the canvas path is the one that already handles a WebGL failure. A page
  cannot set the flag on another page, and the live board never sets it.
- **The /m reload is bounded.** `checkBuild` reads `/v1/health` with `no-store`. The hub writes the build field
  for the board files it serves (`rewriteHealth`), so the build is the hub's, not a room's. A failed read, a missing
  field or a thrown error does nothing. A changed build reloads at most once in 30 seconds, through
  `sessionStorage`, so a hub that flaps between two builds cannot loop the page. `build` and `checkBuild` are inside
  the store's closure and do not collide with the desktop's global `checkBuild`.
- Calm mode is a test-only style sheet. The four sections that assert on motion opt out with `realMotion`.

## Findings

### Low

1. **/m takes the first build it reads as its own.** If the hub restarts on a new build after the page loads but
   before the first `onopen`, the first read stores the new build and nothing reloads, so that page stays on the
   old board until the next build change. The desktop compares against the build the page was served with. Writing
   that build into the /m page, or reading it from the response that served `store.js`, would close the gap.

### Nit

2. Calm mode sets `animation: none` and `transition: none` on everything. Any board code that waits on
   `animationend` or `transitionend` no longer runs in the default suite, except in the four `realMotion` sections.
   A grep for those listeners outside the four sections would show whether coverage was lost.

HUB DEPLOY OK 6e088616 153fbb6f

## Re-read of 852c7cab (@ui, low 1)

`git diff 153fbb6f 852c7cab -- internal/api/web`, read only. `mNet.start()` now calls `checkBuild` before
`loadRooms`, so the first build read happens as the page starts, not when the stream first opens. If two reads
overlap and the second sees a different build, that is a real restart and it reloads. Low 1 is closed. What
remains is a restart in the milliseconds between serving the page and the first read, which @ui accepted. No
findings.

HUB DEPLOY OK 852c7cab
