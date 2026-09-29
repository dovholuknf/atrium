# sa74 handoff: backlog-2 item 74, a long reply loses lines in the middle

Read BRIEF.md first. This is where the investigation stands.

## Verdict so far

NOT atrium's ring, replay or board. The missing lines are in the ring's raw bytes. Later in the same stream a
full-viewport repaint arrives that overwrites them in place, with no line feeds to push them into history. The
board's own xterm.js, fed the uncollapsed raw bytes at a fixed 206x50 with no resize, loses exactly the same 10
lines. The repaint came out of the pseudo console, and that is as far as the bytes go: ConPTY, which is inbox
`C:\Windows\System32\conhost.exe` 10.0.26100.1, or Claude Code v2.1.284 (classic renderer) feeding it. NOT YET
SPLIT between those two.

## Evidence

Card: orchestrator `atrium-87300` = `claude-sg4~01a06dc7-0879-7068-b1eb-20f5c1979845`. The live room is
`atrium.exe room ... --http 127.0.0.1:7781`, reached through the hub on :7778. The only thing touched was a read-only
GET of `/v1/tasks/<id>/scrollback/raw?collapse=0` (and `/raw`, `/text`) at 21:25. Headers: cols 206, rows 50,
one width, not wrapped. The persisted files under ~/.atrium/scrollback date from 20:20 and are older than the
reply, so they were not used.

Copies, all in `build.claude/lost-lines/` (gitignored, not committed):
- `raw0.bin`: the uncollapsed live ring (2,723,702 bytes), with `raw0.hdr`. `raw1.bin` is `/raw` without collapse=0.
  `text.txt`/`text0.txt` are the daemon's `/text` rendering. `xt0.txt` is xterm.js's full buffer over raw0.bin.
- Scripts (node, no DOM, they load `internal/api/web/vendor/xterm.js` directly, and no playwright is needed):
  - `xt.js <bin> cols rows [start] [end]` dumps xterm's whole buffer.
  - `vp.js <bin> cols rows end` prints the viewport after `end` bytes.
  - `vis.js <bin> start end` shows the bytes with readable escapes.
  - `esc.js <bin> start end` counts escape kinds.
  - `bisect.js <bin> cols rows lo hi line needle` finds the first byte at which a buffer line gets the needle.
  - `homes.js <bin> cols rows` lists every bare `\e[H` repaint and how far row 1's text had shifted.
  - `check.js`/`check2.js` score the repro captures, and `run.ps1`/`claude.ps1` drive the repro tests.

Byte offsets in raw0.bin:
- 2414400 to 2423200 is the reply ("Five workers are done..." table, the "69 details" row at 2419061, "Merging:"
  at 2420109, "Running:" at 2420567). It is complete and clean, drawn with CUP to rows 42 to 50 and then `\r\n`
  scrolling. Up to 2423200 xterm has it intact (buffer lines 1886 to 1897).
- 2460305 is `\e[H` followed by a 50-row repaint, every row ending in `\e[K\r\n`, with no LF emitted before it. The
  bytes just before it are `...\e[46;3H\e[?25h\e[?2026h\e[?2026l\e[?25l`. bisect.js confirms this is the byte that
  puts "Next top 10" on the row that held "popover".
- The viewport just before it (vp.js at 2460305) has rows 1 to 10 = "│ popover" .. "Running:", row 11 = "Next top 10",
  row 40 = "Calling atrium-control…". The repaint's row 1 = "Next top 10". Its content is the CORRECT new screen: the
  old one shifted up exactly 10, with "Called atrium-control" plus the next reply ("● sa67 is merged...", 12 rows)
  where "Calling…"+blank were. So whoever made the screen scrolled 10 correctly. Only the downstream stream skipped
  the scroll, and the 10 rows that should have gone to history were overwritten.
- It is systematic. homes.js finds about 70 bare-home repaints in this ring, and several shifted content: at 204389
  (2 rows lost), 789004 (2), 806332 (25), 812144 (26), 1140221/1185822/2387653 (3 each), 2218971 (2), and 2460305
  (10). So a reply is fairly often short some rows. Nearly all follow an empty `\e[?2026h\e[?2026l` pair.

## Ruled out

- Ring and capture: the text is in the bytes, and nothing in atrium removes it.
- `collapseRedraws` and the replay: raw0 is uncollapsed, and xterm alone reproduces the loss.
- Board xterm resize or refit: reproduced at a fixed size with no resize. The ring has one width mark (206).
- xterm scrollback limit: the loss is in the middle, and earlier lines are kept.
- Claude clearing the screen (`\e[2J\e[3J`): this ConPTY passes those through verbatim (repro `c23`), which
  would have wiped xterm's whole history, and it did not.

## Repro attempts (all through the inbox conhost via go-pty, all KEPT the lines)

Uncommitted-quality scratch tests, committed only so the next session has them. Both are skipped unless their env
var is set:
- `internal/daemon/conpty_scroll_repro_test.go`: a child writes 80 base lines plus a Claude-like bottom block, then
  one frame that erases the block and inserts N lines. Knobs: ATRIUM_CONPTY_PARK (cursor on the prompt row),
  _LEAD (a "Calling…" row that turns into "Called"), _NEW, _CLEAR, _WIDE (full-width rows), _LF, _SPLIT=sync|nosync
  (BSU, frame and ESU as separate writes), _REGION=stbm|su, _FULLSEP (full-width `─` separators), and _DIRECT (also
  saves the child's own bytes, which is the no-ConPTY control). ConPTY did the right thing every time. It even
  turned a DECSTBM region scroll into `\e[50;1H\n` plus a repaint. `CSI S` loses lines in xterm directly, so it is
  not ConPTY-specific.
- `internal/daemon/conpty_claude_repro_test.go`: real `claude --model haiku --strict-mcp-config --settings
  {"disableAllHooks":true}` in this worktree, 206x50, with ATRIUM_* and CLAUDE_CODE_* stripped and the classic
  renderer on. It printed 70 then 12 then 12 numbered lines, and everything survived (cl1.bin). No shifted repaints.

## What is left

1. Split ConPTY from Claude Code. The best lever is a NEWER ConPTY: the `Microsoft.Windows.Console.ConPTY` NuGet
   package (conpty.dll plus OpenConsole.exe, what VS Code and WezTerm bundle). Point a repro at it and see whether
   the full repaints go away. That needs a real trigger first, though.
2. Get a trigger. The live pattern is a long-running session where a reply or tool line ("Calling X…" turning into
   "Called X") lands while a peer message typed by atrium sits in the prompt, and the new block goes ABOVE a
   9-row bottom region. Try the claude repro with a tool call (an MCP or Bash call) plus a reply, a long
   transcript, and typed input in the prompt while it streams. Measure with homes.js (wasAtRow>1 means lines lost).
3. Then write it up, since it is not atrium's: the byte evidence above, plus what atrium could do. Options: ship or
   load a newer conpty.dll (go-pty calls kernel32 CreatePseudoConsole with flags 0, in `pty_windows.go`, so this
   needs our own CreatePseudoConsole via conpty.dll). A taller pty makes a shift less likely to reach history.
   Claude's fullscreen renderer is the alternative. No fix without asking.

## Exact next step

Extend `conpty_claude_repro_test.go` so the prompt makes Claude run one Bash tool call and then reply with 40
numbered lines. Type into the prompt while it streams (the way atrium injects peer messages). Run
`build.claude/lost-lines/claude.ps1` with new prompts, then `node build.claude/lost-lines/homes.js <bin> 206 50` and
`check2.js`. When a shifted repaint shows up, rerun the same scenario under conpty.dll from the NuGet package, and
ask atrium-87300 before downloading it.
