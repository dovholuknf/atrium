# Atrium and Codeman

Codeman (https://github.com/Ark0N/Codeman, v1.40.0, read at commit 3a0cee6) and atrium start from the same sentence:
agents run on a machine you leave on, and you watch and steer them from a browser, including a phone. They split on what
the product is. Codeman is a session host: a dashboard that keeps agents alive and productive while nobody watches.
Atrium is a task board with live agents attached, and it gates every tool call. This page compares them from the code of
both, and ends with what atrium should take.

Codeman paths are relative to a checkout of Codeman. Atrium paths are relative to this repo. Where a claim rests on the
README or wiki alone and not on code, it says so.

## Summary

| Question | Codeman | Atrium |
| --- | --- | --- |
| Unit of work | A session (a tab) | A card (a task with a status column, event log, queue, worktree) |
| Where agents live | A tmux session per agent, `src/tmux-manager.ts` | A pty the room owns, optionally in a separate pty host, `internal/ptyhost`, `internal/daemon` |
| Runners | Claude, OpenCode, Codex, Antigravity, Gemini, Pi, Grok, DeepSeek, OMP, plain shell, `src/types/session.ts` | Rows in a runner table: claude, codex, gemini, opencode, ollama, shell, `internal/runnerprofile/profile.go` |
| Shell beside the agent | A separate Terminal-mode session | A second terminal on the same card, same directory, `internal/daemon/shell.go` |
| Continue after a usage limit | Yes, opt-in per session, Claude only, `src/session-auto-ops.ts` | No. Reads the limits, shows them, never acts on them |
| Local input prediction | Yes, `packages/xterm-zerolag-input` | No. Logging only, `internal/inputlag`, `internal/api/web/js/inputlag.js` |
| Phone | Responsive dashboard, PWA, keyboard accessory, voice | `/m` page, phone composer, push, `internal/api/web/m`, `js/tcompose.js` |
| Always-on box | Tailscale first, installer sets it up, systemd or launchd service | Overlays (zrok, OpenZiti), OIDC login on a published board, service installers |
| Permission control | Skip-permissions by default, Approvals Inbox | Every tool call gated, standing rules, `docs/runtime/auto-mode.md` |
| More than one machine | Remote SSH hosts and Docker cases | A hub with one room per machine over mutual TLS, `docs/fabric/hub-and-rooms.md` |
| Platforms | Linux and macOS, Windows through WSL (needs tmux) | Windows, macOS, Linux, native conpty |
| Stack | Node and TypeScript, vanilla JS front end | Go single binary, vanilla JS front end |

## Architecture

**Codeman** is one Node process, `codeman web`, serving a Fastify dashboard and holding the sessions. Each agent runs
inside `tmux`, and the web server holds a pty that is a `tmux attach-session` client (`src/pane-exit-sweep.ts`,
`src/reboot-restore.ts`). Programmatic input goes through `tmux send-keys -l` (`src/tmux-manager.ts`,
`src/session-submit-verifier.ts`). tmux is the persistence layer: sessions survive a server restart and are rediscovered
on boot ("ghost recovery"). Everything else is built on the stream of cleaned terminal text: idle detection, usage limit
detection, the respawn controller (`src/respawn-controller.ts`, 3,200 lines), Ralph loop tracking, the subagent watcher.
The one-process model is simple and it is the reason most Codeman features are screen scrapers. Claude's hooks
supplement that for Claude only (`src/hooks-config.ts`).

**Atrium** is a hub and rooms. The room owns the database, the ptys and the agents, and runs for days. The hub serves
the board and holds no work, so it restarts while agents are mid-turn. With `pty_host` on, the ptys live in a third
process and survive a room restart (`docs/terminal/ptyhost-protocol.md`), which is the job tmux does for Codeman, done
without a dependency and on Windows too. Atrium learns what an agent is doing from hooks first (twelve events for claude
and codex, `docs/runtime/other-runners.md`) and from the screen second (`internal/daemon/looksidle.go`, `idleframe.go`).
A runner with no hooks (ollama, shell) shows a terminal and nothing else.

The consequence for this comparison: Codeman's features that need the agent's state are screen scraping with a
Claude-only upgrade path. Atrium's equivalents start from a structured signal where the runner gives one. Usage limits
are the clearest case, below.

## Feature by feature

### Continue after a usage limit

This is the feature atrium lacks outright, so it gets the detail.

**What Codeman does** (`src/session-auto-ops.ts`, `src/usage-limit-patterns.ts`, `docs/wiki/Keeping-Agents-Running.md`).
Per session, off by default, Claude only.

1. **Detect.** `processCleanData` runs on the ANSI-stripped output. `detectUsageLimitPause` matches a limit phrase
   (`limit reached`, `you've hit your limit`, `you're out of extra usage`) and then requires a reset time within 160
   characters after it. A limit phrase with no parseable reset is ignored on purpose, so chat text that mentions limits
   cannot arm it. The file lists eleven message shapes seen across Claude Code 1.0 to 2.1, including a weekly reset with
   a weekday, an explicit date, an IANA timezone in parentheses, and the raw API form with epoch seconds.
2. **Parse.** The reset is wall-clock in the named timezone when there is one, server-local otherwise, and anything more
   than about eight days out is rejected. Year rollover and DST are handled loosely, and the file says so: the retry
   loop absorbs the error.
3. **Arm.** The fire time is the reset plus two minutes (`RESUME_BUFFER_MS`), at least five seconds out. A stale reset
   in the past arms a five minute retry instead. While a timer is armed, re-detections are ignored unless they are
   earlier by more than 90 seconds, because the footer redraws constantly.
4. **Fire.** It sends Escape (to dismiss the limit options dialog), waits 600ms, then types `continue` and Enter. If the
   session is already working at fire time, it does nothing.
5. **Retry.** `_limitPaused` is cleared optimistically at fire. If Claude is still limited, the footer comes back, the
   detector re-arms, and the cycle repeats. `notifyWorking` resets the attempt count when the session works again.
6. **Survive.** The reset time is persisted and restored after a Codeman restart (`restoreAutoResume`, minimum five
   second delay).
7. **Do not destroy the conversation.** While a session is limit-paused, respawn cycles are blocked. The respawn
   controller would otherwise send `/clear` into the very conversation the timer is waiting to resume. The wiki calls
   this the most useful part, and it is easy to miss.

Gaps in Codeman's version: Claude only (the Agent-CLIs table in `docs/wiki/Agent-CLIs.md` says so), dependent on exact
message wording, and the single `continue` prompt is fixed.

**What atrium has.** The ingredients, not the feature.

- Every card's statusline posts `five_hour` and `weekly` limits with a percentage and a `ResetsAt` time
  (`internal/daemon/telemetry.go`, `Limit`). That is a structured reset time, with no regex and no timezone parsing.
- The board draws them (`internal/api/web/js/usage-limits.js`, `docs/rnd/usage-tab-design.md`) and says plainly that
  nothing there notifies or acts.
- `docs/rnd/runner-switch-design.md` proposes the opposite remedy: when an account is near its limit, file a decision to
  move the work to another runner. It is a design, and it needs a capture while turns still work.
- Machinery to reuse for the delivery: `internal/daemon/restartwake.go` types a line into a card once its runner is at
  an empty prompt with the keyboard quiet (the `injectPeer` gate), and
  `docs/backlog/runtime/r-new-resume-says-continue.md` is a filed item for typing "continue" at a card a restart
  interrupted.
- Auto compact and context cycling (`internal/daemon/autocompact.go`, `docs/runtime/context-cycle-design.md`) are the
  atrium counterpart of Codeman's respawn `/clear`. Whatever resumes a limited card must hold those, for the same reason
  Codeman holds respawn. I did not trace whether they would fire on a card sitting at a limit.

Nothing in `internal/` detects the limit message on screen or schedules a resume. I searched for the limit phrases and
the `resets` text and found only unrelated zrok name limits and the usage tab.

### Local input prediction ("zerolag")

The author's pitch is "local input prediction so typing feels instant over a slow link". The package is
`packages/xterm-zerolag-input` (MIT, zero dependencies, published as `xterm-zerolag-input`, repo `Ark0N/Codeman`
directory `packages/xterm-zerolag-input`). It is client-side only, with no server or protocol change. It ships two
addons for two kinds of TUI, and Codeman picks per runner in `src/web/public/terminal-ui.js` (`_updateLocalEchoState`,
around line 3970).

**Common mechanism.** An absolutely positioned DOM overlay at z-index 7 inside `.xterm-screen`, one `<span>` per
character at exact cell coordinates from xterm's render dimensions (`src/cell-dimensions.ts`). Writing into xterm's
buffer was tried twice and failed, because Ink repaints the screen and overwrites injected cells
(`docs/local-echo-overlay-plan.md`). The overlay is a layer Ink cannot reach. Fonts are copied from computed style. CJK
and emoji take two cells. It works with the DOM, canvas and WebGL renderers.

**Mode 1, buffer (`ZerolagInputAddon`, `src/zerolag-input-addon.ts`).** Used for Claude, Gemini and similar prompts.

- *How it predicts.* It does not guess. It holds the typed keys locally and paints them. `addChar` appends to
  `_pendingText` and the overlay draws it at the prompt. Nothing reaches the pty until Enter or a control character
  (`terminal-ui.js` around line 1583: "Nothing is sent to the PTY until Enter"). Enter sends the whole line, then a
  second write with `\r` 80ms later so the text lands first.
- *Where the prompt is.* It scans the buffer bottom-up for a prompt glyph (`src/prompt-finder.ts`), for example `❯`, and
  falls back to the cursor cell when the glyph is scrolled out of the viewport. It does not trust `cursorY`, which in
  Ink points at a status bar. OpenCode uses a custom finder for its `┃` border.
- *How it reconciles.* There is little to reconcile, because nothing was sent. On Enter the overlay clears and the real
  echo draws on the same pixels. The harder case is text already in the pty (flushed). The addon tracks a flushed count
  and text, shows it opaque, and `removeChar` returns `pending`, `flushed` or `false` so the caller knows whether a
  backspace must go to the pty (a three-layer cascade). The app keeps per-session flushed offsets for tab switches and
  survives a reload through local storage.
- *What breaks with full-screen TUIs.* Quite a lot, and Codeman's own history shows it. Holding keys until Enter starves
  anything that reacts per keystroke: a live slash command picker, history and cursor arrows, composer rewrap,
  paste-burst detection. Codeman issues #218 to #222 are all this, and the fix for Codex was to stop buffering. For
  Claude it works around the cases it can: arrows, Home, End, Delete and paging flush the pending text and hand the
  session back to plain pty echo until Enter or Ctrl+C (`_echoPassthroughSessions`). A bracketed paste flushes pending
  text first, then sends the paste in its own write 80ms later. Escape and Tab are control characters and flush. What it
  cannot do is show the picker or history reaction before the round trip.
- *Cost of being wrong.* Text on screen may not have reached the agent. The wiki says so: "If a prompt appears to have
  been ignored, press Enter" (`docs/wiki/Input-And-Voice.md`). The README describes 50ms debounced forwarding instead.
  The code on the main path holds until Enter, so I would trust the code and the wiki over the README.
- *Default.* On for touch devices, off on desktop (`localEchoEnabled ?? isTouchDevice()`). Shell sessions never use it,
  since the shell's own echo is the thing being hidden.

**Mode 2, predictive write-through (`PredictiveEchoAddon`, `src/predictive-echo-addon.ts`).** Added for Codex, per
`docs/predictive-echo-plan.md`. This is the mosh-style one.

- *How it predicts.* Every keystroke goes to the pty at once, byte-identical to no overlay. In parallel, `predictChar`
  paints the glyph at the predicted cell. The return value is informational and never gates the send, so a wrong or
  suppressed prediction costs nothing on the wire.
- *When it predicts.* Gated by guards: dimensions known, viewport at the bottom, a single code point of width at most 2,
  at most 32 outstanding (`maxPending`), at least 4 cells from the right edge, and a per-runner predicate. For Codex the
  predicate is that the cursor row starts with `› ` (`CODEX_COMPOSER_ROW_RE`). A modal such as the trust dialog has no
  such row, so nothing is painted while keys still reach the pty. Wrapped composer lines are deliberately not predicted,
  because that was the ghost-text zone of #220.
- *How it reconciles.* This is the useful part. It compares against the parsed terminal buffer after xterm's parser has
  run (`onWriteParsed`, coalesced in a microtask), not against the byte stream. The stream matching approach failed
  against Ink and tmux, which turn a keystroke echo into `e\x1b[K\x1b[20;80H...` and paint spaces with ECH plus cursor
  forward. The buffer converges to the same cells however the bytes arrive. The rules:
- a prefix-only confirm loop: a prediction is confirmed when the cell matches **and** the cursor advanced past it, which
    stops false confirms against placeholder text
- positions fixed at predict time and removed on confirm, with no relayout, so partial confirmation does not jitter
- a two-pass mismatch rule: a cell that is neither the snapshot nor the prediction must persist across two parse passes
    before the run is dropped, since a half-parsed row is redrawn milliseconds later
- a TTL of 1000ms drops a stale suffix, and an off-row cursor is tolerated for 150ms
- an anchor hold: after a wire input whose cursor effect is not displayed yet (a backspace over echoed text, a paste, a
    control key) predictions pause until the next parsed write, which costs at most one unpredicted keystroke and
    removed a "tehh" ghost bug
- everything clears on scroll, resize, tab switch, reconnect and font change
- *What it does not do.* Deleting already echoed text still waits for the round trip. Wide characters work in the
  package but IME input never reaches the hook in Codeman.
- *Measured.* The plan records phase 0 recordings of codex-cli 0.147.0 in tmux at 100x30 as fixtures
  (`packages/xterm-zerolag-input/test/fixtures/codex`), and the package has replay and fuzz tests
  (`predictive-echo-fuzz.test.ts`, `codex-replay.test.ts`). The test count in its README (227 in the badge, 175 in the
  alt text) is inconsistent, so treat it as roughly 200.
- *Claude is not on this mode.* Claude sessions still use buffer mode. The predictive composer gate is written for
  Codex's `› ` row only. Porting it to Claude means a composer gate for Claude's `❯` row, and Claude's composer has its
  own per-key behaviour (slash picker, `@` file picker, vim mode) that would need the same recording work.

**What atrium has.** Nothing that predicts. `internal/inputlag` and `js/inputlag.js` are measurement: they time a
keystroke from xterm's `onData` to the frame after its echo is parsed and log which hop (board, hub, link, room) was
slow (`docs/terminal/input-lag-logging.md`). Atrium's keystroke path has more hops than Codeman's (browser, hub, link,
room, pty), so on a slow overlay it has more to gain, and it already has the measurement needed to prove a gain.

What atrium does for a slow link today is a different answer, on the phone only. The phone terminal types into a real
textarea and sends once on the send button as one bracketed paste then Enter (`js/tcompose.js`, `m/js/compose.js`). That
avoids the lag by never sending per key, the same trade as Codeman's buffer mode, with the OS keyboard in charge of
swipe, dictation and autocorrect. A desktop browser on a slow link has no equivalent.

### Remote shell beside the agent

Codeman: a `Terminal` run mode, a session like any other. It sits in its own tab and is not tied to an agent. In
multi-user mode raw shells need an explicit grant (README, security section). It is the way to put a secret in a `.env`
without it passing through the agent.

Atrium **already does this, and ties it to the card.** `internal/daemon/shell.go`: a card may hold two terminals, the
runner's and one shell in the same directory, deliberately two and not N. The shell has no card of its own and records
no activity, is not resumed, closes after 30 minutes unattended, and does not survive the daemon. The design note
rejects a list of shells as building a multiplexer. Codeman's model is more flexible, atrium's keeps the shell next to
the thing it inspects and out of the event log.

### Many runners

Codeman: nine CLIs plus a shell, each a mode with its own idle and prompt handling (`src/session-cli-builder.ts`,
`src/session-cli-registry-bridge.ts`, `docs/wiki/Agent-CLIs.md`), plus custom model endpoints
(`src/custom-model-hosts.ts`, `docs/wiki/Custom-Model-Endpoints.md`) and a DeepSeek harness route
(`src/deepseek-route-config.ts`), which is how a local GPU model reaches an agent. The wiki is honest that most features
are Claude-only because they need hooks or Claude's output format.

Atrium: runners are rows in a table, with a profile of what each reports (`internal/runnerprofile/profile.go`). Two of
four (claude, codex) have hooks and give atrium everything. Ollama and a plain shell give a terminal and nothing else,
and the board says so. Atrium has fewer runner rows, but the same split exists in both products: hooks or nothing.
Adding a runner is a data change, `docs/runtime/wiring-a-runner.md`. A local GPU model today means the ollama row, with
no state signals.

### Phone and tablet

Codeman: the same dashboard made responsive, a PWA with push, a keyboard accessory bar (`keyboard-accessory.js`), voice
input with a push-to-talk path (`voice-input.js`), an IME preview, a mobile overview, and a gesture package
(`packages/gesture-control`). `docs/wiki/Mobile-Guide.md` covers it.

Atrium: a dedicated `/m` page (`internal/api/web/m`) with cards, bubbles, a code review view and a file viewer, push
notifications through the hub (`js/hubpush.js`, `js/phone-bell.js`), a phone composer, and a key bar. The design
workshop is `docs/backlog/ui/mobile-design.md`. Both ended up building a real textarea for mobile typing (Codeman's IME
preview, atrium's composer) because xterm's hidden textarea mishandles Android composition. No voice input found in
atrium.

### A box that runs 24/7

Codeman: `install.sh` asks three questions, can install Tailscale and run `tailscale serve` for a real certificate,
installs a systemd user unit or a LaunchAgent that bakes in PATH, and prints a QR code. A bare `codeman web` binds
loopback, and a network bind without `CODEMAN_PASSWORD` warns loudly (`docs/wiki/Remote-Access.md`).

Atrium: loopback with no login by default, and an overlay for anything else (`docs/fabric/overlays.md`). The hub shares
the board with zrok or OpenZiti and can require an OIDC sign-in on a published board (`internal/daemon/auth.go`). Rooms
reach the hub by mutual TLS, zrok or Ziti. Service installers exist. Codeman's installer is friendlier for the single
box on Tailscale. Atrium has no Tailscale path today beyond binding where Tailscale can reach.

### Keeping an agent working

Codeman: the respawn controller re-prompts an idle session, cycles `/clear` and a kickstart prompt on a timer, has
adaptive timing, circuit breakers and a Ralph loop for task lists (`src/respawn-controller.ts`, `src/ralph-tracker.ts`,
`docs/wiki/Autonomous-Loops.md`). Cron jobs launch sessions on a schedule. The wiki states every cycle is real spend.

Atrium: wake after a restart (`docs/runtime/restart-wake.md`), a cache keep-alive that refreshes idle cards to avoid
cache rewrites (`internal/daemon/keepalive.go`, `docs/runtime/cache-keepalive-design.md`), idle parking, and an
orchestrator that directs cards. I found no idle re-prompt loop and no scheduler in `internal/daemon`. That is by design
more than gap: atrium has an orchestrator session doing that job with judgment.

### Permissions and review

Codeman defaults to `--dangerously-skip-permissions`, offers Anthropic's `auto` mode and an allowed tools list, and has
an Approvals Inbox (`src/web/approval-inbox.ts`). Atrium gates every tool call through a hook, with standing rules and
per-card auto mode that still records every call and who answered (`docs/runtime/auto-mode.md`).

## Where Codeman is ahead

- **Usage limit auto-resume**, with a reset parser tested against eleven message shapes, retry, restart survival and the
  respawn hold. Atrium has none of it.
- **Local input prediction**, both modes, with recorded fixtures, replay tests and a fuzz test. Atrium measures lag and
  does not hide it.
- **Single box on Tailscale in one command**, with the QR code.
- **More runners with runner specific idle and prompt handling**, and a path to custom model endpoints.
- **Docker cases and SSH hosts** as first class places to run a session from one dashboard.
- **Subagent windows, a tile grid of live sessions, and Read My Mind** (prompt suggestions) for watching and driving
  many sessions.
- **Voice input** on mobile.
- **Submit verification.** `src/session-submit-verifier.ts` re-presses Enter while a sent prompt still sits in the
  composer, written after Claude Code 2.1.277 ignored Enter for 30 to 50 seconds after first paint. Any tool that types
  prompts into Claude Code can lose a turn to this.
- **Idle re-prompt and cron** for unattended runs without a director.
- **Multi-user mode** with per-user grants.

## Where atrium is ahead

- **A board, not a list of tabs.** Status columns that are buckets of human attention, activity badges that are never
  stored, an event log per card, a backlog and reports (`docs/how-atrium-works.md`).
- **Agents talk to each other.** `atrium_say`, `atrium_peers`, queued messages, a typing gate that never interleaves
  with the operator's half-typed line, and a held-line notice (`docs/runtime/agent-messaging.md`,
  `internal/api/web/js/heldline.js`).
- **Many machines as one board.** A hub with a room per machine over mutual TLS, a git mirror for finished work and
  cross-room say (`docs/fabric/hub-and-rooms.md`). Codeman reaches other machines by SSH and Docker from one process.
- **The hub restarts, agents do not.** The split by lifetime, with the pty host surviving a room restart and no tmux.
- **Windows without WSL.** Native conpty (`internal/daemon/conpty_*_test.go`, `internal/ptyhost`).
- **A gate on every tool call**, auto mode that still records, and a review of what ran (`docs/runtime/auto-mode.md`).
- **Worktrees per card** and a done handoff, plus runner switching designed around an exhausted account
  (`docs/rnd/runner-switch-design.md`).
- **Input lag measured per hop**, off by default and costing one atomic load (`internal/inputlag`).
- **Usage tab** with limits, burn rate and projected runout from every card's statusline.
- **No scraping of its own state**: hooks first, screen second.

## Backlog ideas worth stealing

1. **Auto-resume at a limit reset.** Type "continue" into a card when its `ResetsAt` from telemetry passes plus a
   buffer, through the `restartwake.go` gate, with a per-card switch. It matters because overnight cards stall on the 5
   hour window. It is cheaper for atrium than for Codeman since the reset time is structured. Cost: small to medium,
   about two days with tests, and it must also hold auto compact and context cycling while a card is limit-paused.
2. **A limit-paused state on the card.** Show "paused until 3pm" on the row and fire an `fyi` so the operator knows an
   overnight run is waiting, not dead. It matters because today a limited card looks like an idle one. Cost: small,
   follows idea 1.
3. **Predictive echo for claude and codex, behind a per-card setting, off by default.** Vendor the MIT
   `PredictiveEchoAddon` (about 480 lines plus 60 for rendering) and give it a composer gate per runner. It matters for
   a board reached over zrok from a phone or a hotel. Cost: medium. Record Claude's composer in a pty first, as Codeman
   did for Codex, check that the vendored xterm exposes `onWriteParsed`, and prove the gain with the existing input-lag
   log before turning it on for anyone.
4. **Prefer predict over buffer.** If atrium ports anything, port only the write-through mode. Buffer mode's
   hold-until-Enter is the source of Codeman's #218 to #222. It matters because it keeps every keystroke on the wire.
   Cost: none beyond idea 3, it is a decision.
5. **Submit verification for typed prompts.** After a peer message, a wake line or a resume line is typed, read the
   screen and press Enter again while the composer still holds the head of it. It matters because Claude Code has been
   seen ignoring Enter for 30 to 50 seconds after first paint and a lost wake costs a whole turn. Cost: small, one check
   beside `injectPeer`, but measure whether current Claude Code still does it first.
6. **A Tailscale path in the installer.** Detect Tailscale, put `tailscale serve` in front of the loopback board, print
   the URL and a QR. It matters for the single machine user, who does not want zrok. Cost: small to medium, mostly
   installer and docs, no new auth since the tailnet is the login.
7. **Voice input on `/m`.** A push-to-talk button feeding the composer. It matters on a phone where typing is the slow
   part. Cost: medium, browser speech APIs or a server transcription route.
8. **Idle re-prompt as an orchestrator tool, not a loop.** Offer "nudge idle cards" as an action the orchestrator can
   take on a timer, without importing Codeman's respawn cycling or its spend. It matters for cards the director forgot.
   Cost: small, and only if the orchestrator does not already cover it.
