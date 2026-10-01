# @ui queue

Top down. @ui takes the next item from the top of this file, not from messages. The orchestrator reorders it by
editing this file. After a restart or a new context, @ui reads this file first.

Each item: what it is, where the spec is, and its state. @ui moves an item to "Done" with its landing sha when it is
live.

## Handoff, 2026-10-01 ~18:30 (@ui moved from claude-sg4 to m1mini)

Reports go to the orchestrator (card 01a0f2da on sg4-control) by atrium say or report. Every board commit goes to
@review first. Workers launch on m1mini, sg3 as fallback. Before landing run only the touched sections plus bootClean,
each alone. The whole suite runs after landing and red there is a follow-up.

In flight, workers on sg3, both held by @review and waiting on a new sha (then fetch, say @review, land):

- `pulls-p3` (alias, branch `claude/pulls-p3`, verdict c0bccc01): always POST `{action:"launch"}` and attach `out.task`,
  re-probe when the tenant is walk with no prId, nits. Tell @rnd at landing.
- `burn-chart` (alias, branch `claude/burn-chart`, verdict 9f9a015a): ulTokensIn null fallback on 1h and filters,
  flat-at-0% on 24h, nits. Screenshots in `D:/tmp/burn-chart/*.jpg` on sg4. clint wants them, give absolute paths.

Owed:

- `suite-units` worker (idle, merged) owes a low from @review 4b117286: in list mode a throw in `main()` stops later
  `unit()` calls and exits 0. Set `process.exitCode=1` and name where it stopped. Give it once pulls-p3 clears.
- @fabric's TypeError `Cannot read properties of undefined (reading 'rooms')` in groupDrag, growlActions,
  peekEverywhere, termDebug, contextSize, reselect during a full `--save-weights` at 404f6def. Pass alone. Not found.
  Suspect `x.rooms` on an empty mock reply under load. Tell @fabric when fixed.
- clint asked for the terminals bridge fix, `u-new-terminals-css-row-bleed-resize.md` (`placeTabBridge()` in
  `js/terminal-list.js`): the attached row stops at the list's right edge, no bridge. clint has not confirmed. Ask
  once, then a worker, @review, report LIVE-READY.
- Live-412 cleanup: write `live-412-findings.md`, publish hub doc `/d/live-412-findings`, exit the live412 and
  one-tooltip and pulls-p2 workers, remove merged worktrees and branches, scratch card `live412-scratch`, ask an
  operator to purge tombstoned docs `test-d-upload` and `test-d-card-doc`.
- Next after that: `u-new-resume-spinner.md`.
- Lows to file: child-fold uses `atrium.folded` not the terminal list key, top-nav nits, one-tooltip nits (runner name
  nowhere, newline join), `m/js/card.js` 891/894 `mNet.rooms()` read as objects, pulls `restoreWhereYouWere` reopens a
  hidden pulls view, `textScan.feed` firstAt nit.

Landed today and reported: top nav, child fold, Live-412 home and changes, pulls P2, one tooltip, suite-units.

## Queue

Running now, four workers at once (clint, 2026-09-30 evening: "focus on ui only, go to 4 ui workers"):

1. **/m card view** (`u-new-phone-card-view-fixes.md` items 1, 2, 5, 7, 8): a live working indicator first, clint's
   own messages in the thread, the recap as a dated sheet at the top. Worker `u-m-card` on m1mini.
2. **/m home and ways out** (item 6, and the session's other phone findings): newest first with order, group and
   filter controls, a way back from a card URL, the phone header and picker checked on the real page, Enter sends,
   the send arrow centred. Worker `u-m-home` on m1mini.
3. **The terminal list's controls move into the gear**, plus the growler reply's two lows (one link rule on desktop and
   phone, a choice cannot send twice). Worker `u-gear-list` on sg3.
4. **The headless suite without software WebGL** (measured before and after), and item 4, /m picking up a new build
   with nothing cleared. Worker `u-suite-webgl` on sg3.

Waiting: item 3 (which bubbles do not work) needs clint's answer.

Next, after u-m-card and u-m-home land (clint, 2026-09-30 evening, by the orchestrator):

00. **Hub documents D2, the views** (`docs/rnd/hub-documents-design.md`, stage D2, clint approved building stage 1
    on 2026-09-30 night). `/d/<slug>` and `/d/<slug>@<n>` on the /m shell and the board, the documents list with a
    title filter, upload from /m, history with a text diff, "published N documents" on a card. Reuse the file viewer,
    `mMd` and the diff colouring. Against @fabric's D1 API (`GET` and `POST /_hub/docs`, raw bytes, tombstone and
    restore). Build on mocked endpoints until D1 lands. Review first, then land. After the current /m landing and the
    diff mockups. The D1 contract is `docs/backlog/fabric/hub-documents-api.md` (on @fabric's branch until D1 lands):
    routes under `/_hub/docs`, multipart upload (`file`, `title`), 201 `{slug,version,url,version_url}`, errors
    `{error}` with 400, 403, 404, 410, 413, 422 `{error,rule}`, 429, 503, 507. The raw route is always an attachment, so
    the page renders from `kind`. No ETag in stage 1. Show `origin`, never `by`, and only `local` may be called clint.
    URLs `/d/<slug>` and `/d/<slug>@<n>`, split on the last `@`.

0. **Code review on the phone** (`u-new-m-code-review.md`, clint 2026-09-30 night): a "N files changed" chip per
   reply opening a diff in the viewer sheet, a card-level changes view, tap a line to quote it. A mock first, then
   @rnd, then build. After the scroll-yank fix.

5. **/m takes less vertical space** (`u-new-m-compact.md`): a one-line message header (speaker and age), smaller
   default type, and a pinch that changes the text size only, with native page zoom off.
6. **Read a card's files on the phone** (`u-new-m-file-viewer.md`): a file path in a message opens a read-only viewer
   through the card files endpoint. Design to @rnd first.

## Filed, not queued

- `docs/backlog/ui/u-new-suite-flakes-0930.md`: cacheChip fails in the full run only. heldLine is fixed (fcf3b974).
  phonePan ("the follow chip went away without input") fails alone on claude/main too, about 1 run in 6, more under
  load. peekEverywhere's half-second timing and shiftMenu failed in the full run only, 17:50.
- Card URLs lows from @review (`docs/backlog/ui/u-new-review-8333259b.md`): (1) a pop-out that reloads onto another
  card keeps `window.name` `atrium-term-<old id>`, set it from the resolved id in `bootTerminalOnly`. (2) sw.js
  compares paths exactly, so `/alias/foo/` misses `/alias/foo`.
- A popped-out card's growler reminder makes no sound while the board window has focus: the board skips a popped-out
  card and the pop-out skips when focus is elsewhere. Low.

## Done

- 2026-09-30 19:45, the board boots again: cardurl.js before notify.js, and `bootClean` boots the real page:
  claude/main 23f71411.
- 2026-09-30 19:40, growler question card (full body, growing reply, choices, steady hover): claude/main 8ad0e98f.
  Headless clock fix: 2cbfa197.
- 2026-09-30 19:09, paste spinner stops on the room's `in-done`: claude/main dd97c0f6 (with @runtime's half).
- 2026-09-30 18:40, card audit: u-039, u-028, u-027, u-001 landed in a06ca6ce. u-023, u-025, u-026, u-032 and the
  mobile-design docs were already on main. u-015 superseded.

- 2026-09-30 17:58, card URLs U1 and U2 (`/alias/`, `/room/`, `/m/` paths, clash chooser, readable links): claude/main
  afae3703, hub build afae370-aa1059bb.

- 2026-09-30 17:11, cross-window silence for the ready alert: claude/main 23fa4602, hub board 2bb25a14.
- 2026-09-30 17:11, growler U1, U2, U3 with the pop-out rules: claude/main eb94e016, hub board 2bb25a14.

- 2026-09-30 15:08, ready-alert spam fix (once per wait, 5 s quiet, focused window silent): claude/main 5e68b83f,
  hub build 19e2f84-7931c7d0.
- 2026-09-30 15:08, per-popout notification bell: claude/main 19e2f840, same build.
