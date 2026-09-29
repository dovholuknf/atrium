# sa74 handoff: backlog-2 item 74, a long reply loses lines in the middle

Read BRIEF.md first. Then the item 74 section of `docs/backlog-2.md`, which holds the full diagnosis and the design,
committed on this branch.

## Who you report to, and the rules now in force

- Report to `@terminal` (handle `terminal-director-of-pty-to-xterm-and-sc`), not atrium-87300.
- Do NOT edit `CHANGELOG.md` or `docs/test-plan.md`. The change note is `docs/changes/74.md` (written, and it
  passes `pwsh scripts/fold-changes.ps1 -DryRun -Item 74`).
- Targeted tests only (`-run`). Clear ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG first. One-line commits, no trailer.
  No merge to claude/main, no deploy, never the live room. Before reporting done: merge claude/main in and pass the
  targeted checks (done once, at claude/main `e45fc2c`, and vet plus the targeted tests passed).
- Do not build any workaround until @terminal says so. It runs a Mercurius review on the design and takes the choice
  to clint.

## Proven facts

- The orchestrator card is `claude-sg4~01a06dc7-0879-7068-b1eb-20f5c1979845` (pty 206x50). Its ring was fetched
  read-only from the hub on :7778 via `/v1/tasks/<id>/scrollback/raw?collapse=0`. Copy:
  `build.claude/lost-lines/raw0.bin` (2,723,702 bytes).
- The reply is complete in the bytes: 2414400 to 2423200, with "69 details" at 2419061, "Merging:" at 2420109 and
  "Running:" at 2420567.
- At **2460305** comes `\e[46;3H\e[?25h\e[?2026h\e[?2026l\e[?25l\e[H` and then all 50 rows, each `\e[K\r\n`, with
  no LF before it. Row 1 ("Next top 10") had been row 11, so 10 rows are overwritten in place. The board's xterm.js
  at a fixed 206x50 loses exactly those rows (bisect.js), so atrium's ring, replay and board are clean.
- About 70 bare-home repaints in the ring, and 9 shifted content:
  - 204389 (2 rows), 789004 (2), 806332 (25), 812144 (26)
  - 1140221 (3), 1185822 (3), 2218971 (2), 2387653 (3)
  - 2460305 (10)

  heights.js shows the live pty painting at 47, 48, 50 and 51 rows at different points, so its height moved.
- atrium resizes the pty only when the agreed size really changes (`setViewport` and `dropViewport` guards). The
  agreed height is the SHORTEST viewer's. Resizes are not logged, and a mark laid and undone with nothing written
  between merges away, so the resize at 21:19:59 is not recorded anywhere.

## ConPTY tests: inbox conhost 10.0.26100, via the harness, each with a no-ConPTY control that never lost a line

| Test | Result |
| --- | --- |
| Plain and Claude-shaped scrolling: park, lead, full-width, bare LF, sync split, DECSTBM, clear+reprint | 0 lost |
| CSI S | loses in xterm directly too, so not ConPTY-specific |
| A child on the console (git bash, cmd, pwsh -NoProfile) | no repaint, 0 lost |
| Focus reports `\e[O`/`\e[I` | no repaint, 0 lost |
| Same-size resize (`RESIZE=same`) | 1 bare `\e[H` repaint each, 0 lost |
| Column alternation 120/121 (`altc`) | 0 lost |
| Rows 50/51 or 50/49 alternating, single steps (`alt`) | 2-4 of 300 (stream), 18 of 300 (Claude-shaped loop) |
| 50 to 40 to 50 flips (`flip`, FLIP_ROWS=40) | 49-98 lost (loop), 100 lost (stream) |

The live signature is reproduced: fast flips are often painted once, as a single 50-row repaint with 50 rows before
and after and `wasAtRow=11`. That is 10 lost, exactly 2460305.

## Claude versus ConPTY: answered as ConPTY

- **OpenConsole NuGet test: DONE.** `Microsoft.Windows.Console.ConPTY` 1.24.260710001 is unpacked at
  `build.claude/conpty/pkg`, and the x64 conpty.dll plus OpenConsole.exe are staged together at
  `build.claude/conpty/x64/`. With the same knobs it lost 0 in every mode (30 to 62 resizes) and emitted zero `\e[H`
  repaints: it is passthrough. At start it queries `\e[c` and `\e[1t`.
- Real Claude 2.1.284 (haiku, `--strict-mcp-config`, `--settings {"disableAllHooks":true}`) under the inbox ConPTY
  never produced a full repaint. That covers 3 turns, one tall turn with a Read tool, and messages typed mid-turn.
- **Echo-statusline test: NOT RUN, on purpose.** `disableAllHooks` also turns the statusline off. The other way
  (a throwaway CLAUDE_CONFIG_DIR) needs a copy of the OAuth credentials, and a token refresh there could rotate the
  live refresh token and log out every session. It is not needed now that resizes reproduce the signature. Tell
  @terminal if it asks.

## Keep-the-rows design state

The design is written in `docs/backlog-2.md` item 74: strict-match rule, false-positive risks, costs, replay-only
versus live, and the recommendation. In short:
- **Strict match.** A bare `\e[H` full-height repaint at a byte with no height mark. Buffer it, then find the smallest
  k where repaint rows 1..M equal screen rows k+1..k+M, with M at least max(6, rows/4) and 4 or more rows non-blank
  and distinct. The match must be unique, or do nothing. Rows 1..k go to history.
- **Risks.** The rule only ever adds to history, so a wrong call is a duplicate or stale line. The causes are
  content that legitimately moved, repeated rows aligning at the wrong k, and repaints split across reads (needs a
  cap).
- **Replay-only (screen.go).** Cheap, but it does not fix a pane that was watching live.
- **Live (fan-out).** A per-runner parse of every byte, held repaints, and wrong calls shown to everyone. It becomes
  dead code after a ConPTY swap.
- **Recommendation.** (1) Debounce agreed HEIGHT changes about 0.5 s in setViewport and dropViewport, which kills
  the coalesced flips. (2) OpenConsole ConPTY as the real fix, behind a setting, with the item 81 prerequisite.
  (3) Replay-only repair only if 2 is refused.

## Files

In the repo, committed:
- `internal/daemon/conpty_harness_repro_test.go` opens a pseudo console through kernel32 or a conpty.dll (env
  `ATRIUM_CONPTY_DLL`).
- `internal/daemon/conpty_scroll_repro_test.go` is the synthetic child plus parent.
  - Child knobs, as `ATRIUM_CONPTY_*`: PARK, LEAD, NEW, CLEAR, WIDE, LF, SPLIT, REGION, FULLSEP, SPAWN, SPAWN_MS,
    SPAWN_AFTER, STREAM, LOOP and DIRECT.
  - Parent knobs, also `ATRIUM_CONPTY_*`: RESIZE (same, flip, alt, altc, focus), FLIP_ROWS, FLIP_HOLD and OUT. The
    parent also writes `<OUT>.sizes` with the byte offsets of its resizes.
- `internal/daemon/conpty_claude_repro_test.go` runs a real Claude. Knobs: `ATRIUM_CLAUDE_OUT`, `_DIR`, `_PROMPTS`
  ("prompt=>marker|..."), `_TYPE`, `_TYPE_AFTER`, `_TYPE_EVERY` and `_TYPE_TIMES`.
- `docs/changes/74.md` and `docs/backlog-2.md` item 74.

In `build.claude/lost-lines/`, gitignored:
- Captures:
  - `raw0.bin`, `raw0.hdr`, `raw1.bin`, `text.txt`, `text0.txt` and `xt0.txt` come from the live ring.
  - `cl1.bin` is real Claude.
- Scripts:
  - `xt.js`, `vp.js`, `vis.js`, `esc.js` and `bisect.js`.
  - `homes.js` lists every bare-home repaint with its `wasAtRow`.
  - `heights.js` gives the painted height per repaint.
  - `check.js` (with `--follow`, which replays `.sizes`) and `check2.js` score captures.
  - `run.ps1 <name> [park] [lead] [new] [clear]` reads the knobs from env: STREAM, LOOP, RESIZE, FLIP_ROWS,
    FLIP_HOLD, DLL, SPAWN, WIDE, LF, FULLSEP, REGION and SPLIT.
  - `claude.ps1 <name> turn|multi` reads DLL and TYPE*.

  The scripts write captures to the session scratchpad path at their top, so change `$S` to a folder that exists.

## Exact next step

atrium_say @terminal that the diagnosis and design are committed, with the head sha. Then wait. Build nothing until
@terminal or clint picks an option from item 74's recommendation. If asked to build option 1 (the height debounce),
it goes in `setViewport` and `dropViewport` in `internal/daemon/supervisor.go`, with a test that flips rows back and
forth fast and asserts one or zero `Resize` calls. Check it with the harness `flip` runs above.
