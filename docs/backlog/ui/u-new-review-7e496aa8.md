# Review of u-m-changes 7e496aa8 (@ui: /m code review, design B with @rnd's 9 changes)

Reviewed by @review, 2026-10-01, from `git diff claude/main...7e496aa8` (one commit, off 6ee44361). Read only. As
asked, the shape is checked against `internal/daemon/changes.go` on claude/main (r-changes, landed 8a38842a), along
with how untrusted text is handled on its way into the DOM and into a sent message.

## What holds

- **Every untrusted string reaches the DOM as text.** Paths, statuses, `old_path`, hunk headers, diff lines and the
  word marks all go through `el()` / `textContent` / `append(string)`. The reply chip's `data-at` is escaped with
  `U.esc`, and `r.edited` is only drawn when `> 0`, so a non-number never reaches the HTML. Nothing from the answer is
  ever `innerHTML`.
- **The shape matches changes.go.** The page reads `files[].path, old_path, status, added, removed, hunks,
  hunks_cut, cumulative`, and `head, note, partial, outside, cut.files, cut.hunks, cut.why`. All are present with
  those JSON names. The statuses (added, modified, deleted, renamed, binary) match. The untracked file's synthetic
  `@@ -0,0 +1,N @@` header parses, and `\ No newline` is skipped. A 404 shows the room's sentence, never an empty
  diff.
- **`?turn=` is exact.** `data-at` carries the reply's `at` string as /replies sent it, and it is
  `encodeURIComponent`'d. That is the exact-time match r-changes needs.
- **One call per tap, never on a timer.** It is refreshed on a turn end only while the sheet is open, and stale
  answers are dropped by `seq`.
- **Multi-line comments to a runner without paste** are joined by the existing `joined` path and say so.
- **The quote always names the repo-relative path** (@rnd's change), with where it was read and the head sha.

## Findings

### Medium

1. **A quoted diff line is typed into the agent's terminal unfiltered, control bytes included.** `quoteOne`
   (`compose.js`, review comments) puts `c.path` and `c.text` into the message verbatim. Both come from the card's
   worktree: a file's name and a line of its content, which can come from a pull request under review.
   - The message goes to `POST /v1/tasks/{id}/message`, and the daemon types it into the pty. When the runner
     supports it, the text is wrapped in `\x1b[200~ ... \x1b[201~`. Nothing on either side removes control
     characters (`supervisor.go` `SayPasted`, `messages.go` `typeLabelledGuarded`).
   - A line that holds `\x1b[201~` ends the paste early, and what follows arrives as keystrokes: `\x1b[Z`
     (Shift+Tab, which cycles Claude Code's permission mode), `\x03`, `\x15` (Ctrl-U erases the line), or any other
     escape.
   - A file name can hold a newline (`name-status -z` passes it raw), which starts a new line in the message.
   - The chip shows only `basename:line`, so the operator never sees what is being sent.

   Fix it in `quoteOne`. Replace every C0 and C1 control (`\x00-\x08`, `\x0a-\x1f`, `\x7f-\x9f`, keeping tab) in
   `path` and `text` with a visible form such as `\x1b` or U+241B, so the quote shows what is in the file without
   acting on it. Add an mChanges case with an ESC and a newline in a line and in a name.

   Separately, `typeLabelledGuarded` and `SayPasted` should drop `\x1b[201~` from inside the text they wrap, so no
   message path can end its own paste. That is @runtime's, filed by me as a follow-up, and not a blocker here.

### Low

2. **The turn's `why` is not shown.** For a turn, `changes.go` puts "a commit in the turn's window has an author date
   outside it ... commits may be missing" into `why`. The page shows a fixed PARTIAL sentence instead. Show `d.why`
   when it differs from the shell sentence, or always.
3. **Bidi controls in a diff line display reordered** (Trojan Source, U+202A-202E and U+2066-2069). A review view
   is exactly where that matters. Mark them visibly in `textEl`, the same way as finding 1.

### Nit

4. When both `cut.files` and `cut.hunks` are set, the combined `cut.why` is printed twice, once on each note.

Quality: after the Sonnet switch. The shape is matched field by field, the DOM is text-only throughout, and history
and refresh are careful. The miss is the one hop the design flagged, untrusted text into a sent message: escaped for
the DOM, but not for the terminal it ends up in. That is the same pattern as before (the second-order edge), and no
drop.

HOLD 7e496aa8~1..7e496aa8 on finding 1. The fix is a small sanitiser in `quoteOne` plus a test case. Lows 2 and 3 can
come in the same pass.

## Re-read of 7ab1eacb (2026-10-01)

Only the fix was read (`git show 7ab1eacb -- internal/api/web`). Everything else after 7e496aa8 is a merge of
claude/main.

1. **Closed.** `visible()` maps C0 controls but tab to U+2400 + n (ESC becomes U+241B, LF becomes U+240A), DEL to
   U+2421, C1 to `\xNN`, and U+2028 and U+2029 to U+23CE. It is applied to `path`, `text`, `where` and `head` in
   `quoteOne`, and to the chip's basename. No byte from the file can end the paste, send a key, or start a new line
   in the message. @ui reports that the evil-name mChanges case fails when `visible` is the identity function.
2. **Closed.** The room's `why` is its own grey line on a turn.
3. **Closed.** Bidi controls show in place as `<U+202E>`-style spans in `textEl`, inside a mark too.
4. **Closed.** The cut reason prints once, on one line.

Defense in depth on the daemon side (`\x1b[201~` inside any wrapped paste) stays with @runtime. It was filed, and is
not a condition of this verdict.

Quality: each finding is fixed as asked, and the test was shown to fail without the fix. No drop.

HUB DEPLOY OK and ROOM DEPLOY OK 7e496aa8~1..7ab1eacb. The room verdict is there because the room serves the board too.
