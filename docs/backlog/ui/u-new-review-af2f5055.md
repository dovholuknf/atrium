# Re-read of pulls-p3 af2f5055: the walk drawer over /v1/prs, after the HOLD at c0bccc01

Reviewer: @review, 2026-10-04. Range d33bd3b5..af2f5055, which is 450269c4 plus the fix, merged with claude/main
(e655d544). The fix delta is `git diff af2f5055^2 af2f5055 -- internal/api/web scripts`. Read against
internal/api/prsdrawer.go, and the touched headless units were run here.

**Verdict: OK d33bd3b5..af2f5055.** One merge conflict against claude/main today, in the test file only, noted below.

## 1. The MEDIUM is closed, and the case bites

`pullsWalk` (js/pulls.js) now always posts `{action:"launch"}`, takes `out.task` (falling back to the row's
`walker_task`), and attaches it. The server decides whether the old walker is still alive, as the earlier review asked.

The case is in `pullsDrawer`: row `pr_d` has `walker_task: "land-pwdead"`, and the mock answers 201 with a new task for
that name and 200 with the live one otherwise. The case clicks walk, expects `land-pw3` attached with the drawer open,
clicks again, and expects exactly two launch posts and that `land-pw3` was not replaced.

Mutant, done by hand and reverted: `pullsWalk` seeded `task` from `row.walker_task` and posted only when it was empty,
which is the old behaviour. `pullsDrawer` fails three ways: "walk on a row whose walker is done did not reach a new
walker", "walk did not ask the daemon for the walker every time", "a live walker was replaced". The case covers it.

## 2. The LOW is closed

`pullsChanged` now calls `walkProbe(termTask)` when the card has a row and the drawer is not yet on the pulls source
(`!dock.tenant || !walkTenant.prId`). It is not a loop: `walkProbe` sets `prId`, `loadPr` calls `pullsChanged` again,
and the condition is then false. The fix is slightly broader than the one suggested, which only covered the walk
tenant, and the extra case is a drawer on no tenant, which is also right. `pullsWalk` re-probes after the attach for
the case where the card was already attached.

A case was added: the page loads with the rows gated, attaches `land-pw4`, sees `prId` empty, releases the rows, and
expects `prId === "pr_x"`. Mutant, reverted: the `pullsChanged` re-probe line deleted. `pullsDrawer` fails with "a
walker attached before the rows loaded stayed on the files source". Covered.

## 3. The NITS

- The undo toast now says "this finding is still open", which is right with three states. Fixed.
- The CRLF note is not addressed. `walkPrItem` still parses with `"\n"`, and a first save of a CRLF finding rewrites it
  with LF. Still harmless for a finding, and still unsaid anywhere. Left open, nit.

## 4. What the fix brought in

- `pulls.open`, `pulls.findings`, `pullsToggleFindings`, `pullsMark`, `pullsWalker`, `pullFindingsHTML` and the
  `.pull-finding` family of CSS rules are gone. A grep over internal/ and scripts/ finds no caller and no use of the
  CSS. The only hit is a negative assertion in `pulls` ("findings are still drawn inline"). The Go hits are unrelated
  names in the store and link tests.
- So the drawer is now the only place findings are read. The pulls row keeps its counts line, and the walk button is
  the way in. That is the intended design and the row's tooltip says so. A ready row with no walker still reaches the
  findings in one click, because the click launches one.
- `onPrEvent` now kicks the drawer when the event's row is the one it walks, so a mark made elsewhere shows. It guards
  with `typeof`, as the rest of the file does.
- A 409 on save now carries the server's sentence into the compare view (`said`). The `d` key exists only in pulls
  mode. Neither affects the file source.

## 5. Headless run

Playwright is not installed in this worktree, so it was taken from a sibling's node_modules through `NODE_PATH`, and
nothing was installed. `HEADLESS_UNITS=pulls,pullsAbsent,pullsDrawer,bootClean node scripts/test-board-headless.js`:
all four ok. Nothing failed, so there was no base run to compare. The rest of the suite was not run, as asked.

## 6. Merge onto claude/main 6b407bbd

`git merge-tree --write-tree 6b407bbd af2f5055` is not clean. One file, scripts/test-board-headless.js, three hunks,
all of the same kind: both sides appended to the same place. Main added the hubRepos, burn and keepAlive sections and
this branch added `pullsDrawer`. (1) the section functions at the end of `trayHeadSection`, (2) the section table
line, which also loses `pullsDrawer` or the main sections if taken one-sided, and (3) the `unit(...)` list. The
resolution is to keep both sides in each hunk. Whoever merges owns it, and `HEADLESS_LIST=1` shows the unit guard if one
is dropped. The product files merge without conflict.

Verdict: OK d33bd3b5..af2f5055. Hub-ok and room-ok, as the change is web files and a test.

Quality: the fix took the server's answer rather than adding a client rule, and both cases fail when the fix is
removed.

Atrium-Verdict: ok d33bd3b5..af2f5055
