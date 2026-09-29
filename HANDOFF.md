# HANDOFF: @terminal, director of the terminal path (claude/terminal)

Written 2026-09-29 about 05:40 UTC at the 150k context line. Re-read `BRIEF.md` and `DIRECTOR.md` in this worktree
first, then this file. My handle is `terminal-director-of-pty-to-xterm-and-sc`. The orchestrator is `atrium-87300`
(alias `orchestrator`) and the merger is `merge` (card 01a0eb20).

## Rules that changed since DIRECTOR.md was written

- ASK the orchestrator before EVERY worker launch, in one line: the item, a one-line reason, and the room.
- A design goes into backlog-2, then a Mercurius round (`mercurius_open_session` with `working_dir` this worktree,
  then `mercurius_start_review_round`) before anything is built. Artifact names must be plain file names. Wait on a
  round with `pwsh -File <scratchpad>/waitmerc.ps1 -Status <.mercurius/<session>/status.json> -Round N` in the
  background. The session `state` stays `active`, so the script keys on `round_count` and `active_round`.
- Exit a worker BY CARD ID once it is merged into claude/terminal. Remove its worktree and branch after @merge
  lands it.
- Directors do not write feature code. Test and comment fixes after a merge are fine, and so are backlog-2 edits.
- `docs/changes/<item>.md` follows `docs/changes/README.md`. Nobody edits `CHANGELOG.md` or `docs/test-plan.md`.
- No gcc here, so `go test -race` cannot run.
- Hooks: no `;` chaining, no `>` (use `tee`), no `cd x && y`, no `git -C`, no `git checkout`. `go build` must write
  to a `build.claude/` (`go build -C <dir> -o build.claude/ .` from PowerShell works for a scratch tool).
- Backlog numbers are shared across directors. Ask the orchestrator for the next free one.
- DO NOT touch `D:/worktrees/claude/atrium/sa89`. It is @runtime's item 89 worker (card 01a0eb97), not item 33.
- Reading the live room is fine (logs, a COPY of the DB). Never write, restart or call anything on it.

## This context's work, all on claude/terminal, docs only, nothing handed to @merge yet

Head is `940a970`. Commits since the last handoff: `d93b6db`, `d536987`, `c7b6511`, `054af15`, `8e42a86`,
`f3303ec`, `061bd1a`, `940a970`, all `docs/backlog-2.md`.

- **Item 33** is fully built on claude/main: `118d6e5` (typedline, keystrokes only), `d713e5c` (Esc Esc port),
  `2f15ace` (readout), and `TestASayWhenDoneWaitsForTheTurnToEnd` covers `when: done`. All in the deployed room
  binary `66717c5`. The sa85 incident is confirmed from a DB copy plus `~/.atrium2/room.err.20260928-111944`: three
  messages (09:03 `when: done`, 09:45:50, 09:50:59) were all typed at 09:55:21, 2s after clint's keystrokes, and the
  09:45 turn end released nothing. That is the old counter, and the build predates `118d6e5`. Status is in item 33.
- **Item 8** is narrowed to the `[inputlag]` prefix naming only. The hop split is `1f49694` (runner side), and the
  unsent-bytes fix is `6bb14e4`. The table row had a stale pre-resign SHA `5d9ba72`, now corrected.
- **Item 61** table row is marked DONE `88fc53d`.
- **Item 74 option 3** (replay-only repair) is designed in item 74 beside option 2, with a cost table ("2 or 3, for
  clint"). The status line now says the height hold is on claude/main (`387ccd5`). Mercurius `s_ijoTH04DNGvl`,
  CLOSED: rounds 1 to 4 were each needs_changes with one major, all fixed. Round 5 is **ready_to_build** and its
  advisory is adopted. Nothing is left to collect.
- **The recommendation for clint** (accepted by the orchestrator, who carries it to him): build option 3's
  diagnostic report alone first (`/scrollback/text?repair=report`, a day at most) and read it on the live room for a
  few days. Almost nothing means the height hold was enough. Real losses mean option 2 (OpenConsole ConPTY) is the
  fix. Option 3's repair only if 2 is refused, or as a stopgap, since it cannot fix the pane that was watching.

## Next

1. Hand claude/terminal to @merge: `git rm HANDOFF.md` in a commit first, then one `atrium_say` to `merge` with the
   branch, the head sha, and "docs only (backlog-2 items 8, 33, 61, 74), no suite needed, or run it if you want".
2. One `atrium_report` to atrium-87300 for this batch.
3. Waiting on clint: item 74, option 3's report versus option 2. Also a C toolchain for `-race`.

## FYI from @fabric

`TestRealSessionsKeepTheirText` fails on claude/main `bb65176` on this machine. It replays the live
`~/.atrium/scrollback`, and session `01a0eac2-73a` keeps only 26% of sampled words. That card is sa74, whose
scrollback is full of the lost-lines flip repros, so a low score there is expected. Known noise per DIRECTOR.md, but
it will keep flipping as the corpus grows. A floor or file-selection change is a candidate item. Not filed.

Scratchpad for this context: `C:/Users/claude/AppData/Local/Temp/claude/D--worktrees-claude-atrium-terminal/47c4cb02-0c4f-4de1-8b5e-636cc3ac6187/scratchpad`
(`waitmerc.ps1`, `q/` is a read-only sqlite query tool with a DB copy in `q/db`, which can be deleted).
