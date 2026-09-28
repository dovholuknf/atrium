# Changelog

A running log of what's been built. Newest first. No formal version cuts yet (everything is `v0.0.0-dev`); each
section heading is just "what landed in this iteration."

## Unreleased

- **One command makes another machine a room of this hub, over ssh and with no admin or sudo.** See
  `docs/packaging.md` "Provisioning a room over ssh, from the hub" and `docs/test-plan.md` section BU.

  `scripts/provision-room.ps1 user@host` finds the hub from its running process, detects the remote OS and arch,
  puts a matching atrium in the remote home folder, joins the room over direct, ziti or zrok, starts it, waits for
  the hub to see it, and checks the runners asked for. It is safe to run again, prints one `provision <step>
  <status>` line per step, refuses a second room on one machine, and `-Remove` undoes what it did. The binary is the
  GitHub release by default, and there is none yet, so `-FromCheckout` builds it. The room runs detached with no
  autostart unless `-Autostart`. `-Install claude,codex` fetches runners from their vendors after a trust warning.
  Proven on Windows, Linux and macOS, and over ziti with a throwaway network. zrok is proven from the hub machine
  only.

  Around it: `atrium room join` gains `--no-run`, and `--openziti` reads a `.jwt` file. `atrium room --detach` starts
  a room in the background and returns once it answers. A ziti JWT is enrolled in process when there is no `ziti`
  CLI. A hub over zrok writes its share down so `atrium rooms token` can mint a zrok join string.
  `atrium-service.ps1 -Verb room` and `ATRIUM_SERVICE_VERB=room atrium-service.sh` register `atrium room`, and
  `atrium-service.sh` leaves lingering off unless `ATRIUM_LINGER=1`. `internal/cli`, `internal/daemon`: HUB RESTART
  for the zrok share file, and a room gets the rest when this script builds for it.

- **Every Claude card's token use is on record, a row per turn, shown only in the card's details.** See
  `docs/backlog-2.md` item 37 and `docs/test-plan.md` section BT.

  At each Stop the room reads the replies the turn wrote to the runner's transcript, with the keep-alive's own reader,
  one per message id, subagent replies left out, and writes one `session_usage` row: input, output, cache writes at
  5m and 1h, cache read, the context, and a cost estimate on the models keep-alive has prices for. Each row says what
  started the turn: `operator`, `say`, `restart-wake`, `keepalive` (from the refresh fork's receipt), `resume`, or
  `unknown`. The first turn of a resumed runner is flagged whatever started it. Rows have no foreign key, so they
  outlive the card, and a restarted room counts on from the last row. Before the room ran this, nothing is
  recorded. The card dialog has a `token use` fold: totals, context now, and the newest 200 turns. Nothing on the
  card face, the terminals list or a toast. `internal/store` (migration `0063_session_usage`), `internal/daemon`,
  `internal/api`: ROOM RESTART. `internal/api/web`: HUB RESTART for the board a hub serves, and the room's own board
  with the room restart. The hub proxies `/v1/tasks/{id}/usage` with no change of its own.

- **The launch cap counts only running workers tagged `atrium:subagent`.** See `docs/test-plan.md` section BR.

  It counted every running supervised card carrying `origin:agent`, which every `atrium_launch` stamps. So one cap of
  10 covered every orchestrator's launches plus the resident merger, and a launch was refused with seven workers up.
  Now an orchestrator, the merger or any agent launched without `atrium:subagent` does not use up the cap.
  `origin:agent` is still stamped on every launch and still marks a doer on the board. `internal/link/control_mcp.go`:
  HUB RESTART. `internal/api/web/js/terminal-list.js`: comment only.

- **A launched worker's approvals go through atrium's gate, so board-wide auto covers it.** See
  `docs/auto-mode.md` "A launched worker is gated by default" and `docs/test-plan.md` section BS.

  A worker never runs `atrium join`, so the permission hook let it through and its approvals were Claude Code's own
  prompts in a terminal nobody watched, while the board-wide switch said nothing would ask. A launch now sets
  `ATRIUM_PERM_GATE=on` unless the harness row names the variable itself, so `ATRIUM_PERM_GATE=off` on a row opts
  that runner out. With auto off the worker gates to the operator as a joined session does.
  `internal/daemon/launch.go`: ROOM RESTART, and it reaches a worker at its next launch.

- **A keep-alive refresh whose ledger row fails to save no longer forks again every minute.** Mercurius finding C1
  of session s_GqkzdtBKudfM.

  The warm window and the budget come from saved `keepalive_refresh` rows, so a paid fork whose row did not save
  left the next tick seeing the old expiry. Now the room holds that card's refreshes in memory until the cache the
  fork may have warmed would expire, a real turn, or a hand on the switch, and the card's keep-alive `why` says so.
  The design doc says what is built and that skips are not ledger rows. The gear and card-menu text say that a
  refresh sends one word from a copy of the conversation, the copy is thrown away, and the card's own conversation
  is never touched. `internal/daemon/keepalive.go`: ROOM RESTART. `internal/api/web/index.html` and
  `internal/api/web/js/keepalive.js`: ROOM RESTART (the room serves the board). `docs/cache-keepalive-design.md`:
  doc only.

- **A launched claude worker starts lean: ~25k fewer tokens on its first request.** See
  `docs/lean-workers-design.md`, `docs/backlog-2.md` item 29 and `docs/test-plan.md` section BP.

  A worker inherits none of its launcher's conversation, but it booted with the operator's whole setup: the global
  CLAUDE.md, auto-memory, every user and claude.ai skill, every agent type, prompt-time hook reminders and every MCP
  server. Its first request was 40k tokens in a worktree. Now `atrium_launch` starts a claude worker with
  `--setting-sources project,local`, a copy of the user settings that keeps the permissions, env and hooks (less the
  operator's own SessionStart and UserPromptSubmit hooks, which print into the context), atrium-control and mercurius
  as the only MCP servers, 22 tools it has no use for disallowed, a short appended system prompt with the worker
  rules, and auto-memory off. The same `-p` probe drops from 35,970 to 11,029 tokens. `mcp: [...]` adds servers from
  the runner's config, and `lean: false` starts a worker as before. The card is tagged `atrium:lean`, so a reopen stays
  lean. HUB-SIDE and ROOM-SIDE: needs both, and a room restart.

- **A big paste shows the `pasting` spinner, a dropped block of text pastes, and a paste over 4MB is refused.** See
  `docs/test-plan.md` section BQ.

  A paste of 256KB or more used to show the spinner for a frame or not at all. `send` holds the main thread about
  7ms per MB, so the 20ms timer waited behind it, and the frame drains over loopback in tens of milliseconds while the
  daemon is still writing it to the pty. The first output after the drain ended the box. Now a paste that big puts
  the box up first, leaves once it has painted, and keeps the box up at least half a second, about a second per MB up
  to four, before an echo may end it. Input typed while it waits to paint goes after it, and none of it goes if the
  terminal switches card or reconnects first. A small paste straight after a big one keeps the big one's box. A block
  of text dropped on the terminal went nowhere and is now a paste. A paste whose frame is over the daemon's 4MB read
  limit used to close the socket and lose the paste. It is now not sent, and an alert says so. The new headless
  section `pasteBig` covers every paste gesture, a bracketed runner and a popped-out window. HUB-SIDE.

- **A slash command or a restart no longer makes a launched card read STUCK, and a stuck card wears a mark.** See
  `docs/backlog-2.md` item 25 and `docs/test-plan.md` section BM.

  The silent-stop check asked only for a card waiting on its prompt with a prompt newer than its last report. A
  built-in slash command such as `/model` is a prompt that runs no turn, so it made an idle card owe a report, and a
  room restart resumed that card with a fresh `waiting_since` and rang `is STUCK` again from one minute. Now a silent
  stop needs a turn that ended after the prompt. A new `turn_end` table (migration 0062, seeded from the event log)
  records when a card goes from working to waiting, and that moment is the backoff's clock, so a restart does not
  start it over. Needs a room restart.

  On the board, a stuck card wears a stopped-clock mark in the warn colour on the stack, the board and the terminal
  strip, with a tooltip saying why and since when. It goes when the card moves. The gear's new `stuck agents`
  setting alerts and marks (the default), only marks, or is off. HUB-SIDE.

- **A Claude card whose subagents are still out no longer tells you it is waiting on you.** See `docs/test-plan.md`
  section BN (backlog-2 item 17).

  A review panel starts its reviewers in the background, so the parent's own turn ends with them still working, and
  each reviewer's report wakes the parent, which reads it and stops again. Every one of those Stops moved the card to
  `needs input` and rang the board, three to five times per review. Claude Code's Stop payload lists what is still
  running in `background_tasks`. The Stop hook now counts the running subagents in it and sends the count. While it
  is above zero the room keeps the card in `running` with an idle badge, skips the silent-stop report and the seen
  note, and ignores an `idle_prompt` notification. The Stop after the last report moves the card and rings once. A
  question, a dialog, or a background shell still rings as before. Needs a hook binary rebuild and a room restart.
  ROOM-SIDE.

- **The "not replayed here" notice opens or loads the pre-restart history.** See `docs/test-plan.md` section BO.

  An attach that replays only the newest 4 MB of a card's pre-restart history starts with a grey line that sent you
  to the terminal's cog. Now the line has two links: `open all of it` opens the same viewer as the cog entry, and
  `load all NMB in here` resets the terminal and re-attaches with `?carry=all`, under the paste spinner, to replay
  every saved byte. The links are OSC 8 hyperlinks with an `atrium:` scheme, and each carries a nonce the board made
  for that socket (`?link=`). The board swallows any `atrium:` link without that nonce, so a program that prints one
  gets plain text. A lent session's guest gets the old line with no links, and a guest asking for `?carry=all` is
  refused. The popped-out window takes the same path. The daemon half needs a room restart. The board half is
  HUB-SIDE and is safe on its own: a room that does not know `?link=` sends the old line.

- **Idle Claude cards keep their prompt cache warm, and stop at break-even.** See `docs/cache-keepalive-design.md`
  and `docs/test-plan.md` section BL.

  A cache write after a gap of more than an hour cost 9.6% of last week's spend, because a card that sits for 61
  minutes rewrites its whole context at twice the input price when it is answered. Now the room refreshes an idle
  card's cache a few minutes before it expires. The refresh is a forked headless resume of the card's conversation
  (`claude -p --resume <id> --fork-session --no-session-persistence`) that reads the cached prefix, answers OK and is
  thrown away. The card's terminal, transcript and conversation are never touched. The fork may not use a tool (a
  PreToolUse hook refuses every call), takes one turn, loads no user or project settings, and its receipt is
  checked: a fork that tries a tool stops its card, and one that acts suspends the room. A card stops by itself once
  its refreshes since it went idle have cost an eighth of one full rewrite, which is the best stop on Opus 5.5 by the
  resume odds in 21 days of transcripts. It raises a toast (logged) and draws a `❄ cold` chip with the spend in its
  tooltip. The card's next turn, or switching it back on, starts a fresh budget. Opus 5.5 and Fable 5.1 only, on the
  1h cache, context of 50k or more. A new Claude card is launched with `CLAUDE_CODE_PROMPT_CACHE_TTL=1h`. The gear's
  settings hold the default for new cards (on) and the week's refresh spend. Each card has its own switch in its
  menu. Every refresh is a row in the new `keepalive_refresh` table, priced. Needs a room restart. The hub's event
  relay tags the new `keepalive` event with its card, which is HUB-SIDE.

- **Copy on select no longer copies what the find bar matches.** See `docs/test-plan.md` section BJ.

  The search addon shows a match by selecting it, and copy on select copied every selection change, so each keystroke
  in the find bar (ctrl-shift-f), each step, and each re-search on new output overwrote the clipboard. Now copy on
  select copies on the mouseup that ends a press in the terminal, and only when that press changed the selection: a
  drag, a double-click word, a triple-click line or a shift-click extend. A plain click copies nothing. ctrl-a still
  copies the whole buffer. A popped-out window runs the same code. The new headless section `copySelect` covers it.
  HUB-SIDE.

- **A button that fires a request shows it is working and refuses a second press.** See `docs/test-plan.md` section
  BK (backlog-2 item 19).

  `launch` on the resume dialog showed nothing while the runner started, so a second click went out and came back as
  an error on a card the first click had already started. Now `busyWhile` in `js/core.js` is the one guard: the
  pressed button turns into a spinner and a working label, the other buttons in its row go dim, and a second click,
  Enter or call is refused until the request answers. A launch that fails keeps its dialog open and says why in it.
  Flows with no button left to wear it (card menu entries, confirmations) go through `oneAtATime`, keyed by card. It
  covers launch, resume, restart, unshelve, shelve, terminate, remove, the permission answers, say and note, the
  approve-everything switch, rules, shares, overlays, dispatch, and every save, remove and run button in the rooms
  and runners editors and the theme and skin pickers. The headless section `busyGuard` presses each twice.

  The daemon is safe too: a launch onto a card that a launch started in the last 30 seconds, while that runner is
  still up, answers with the card instead of `already has a runner on it`. HUB-SIDE for the board, the daemon half
  needs a room restart.

- **Any paste still on its way after 20ms shows the `pasting` spinner.** See `docs/test-plan.md` section BB.

  The spinner showed only for a paste of 2KB or more, so a short paste over a slow link sat in a terminal that looked
  like it ignored you. Now what starts it is the paste gesture (ctrl-v, right click, the paste box, a dropped or pasted
  file's path), not the size. Typed keys and escape sequences never start it, however long. The 20ms delay, the
  drain-then-echo end and the 20s cap are unchanged. A paste under 1KB names its size in bytes. The new headless
  section `pasteSpinner` covers a held one-line paste, a paste that lands inside 20ms and typed input. HUB-SIDE.

- **The `atrium2` shim and the cutover rollback are gone.** `make build` writes only `build.claude/atrium.exe`, and
  `cmd/atrium2`, `internal/cli/atrium2.go` and `scripts/live/cutover.ps1` are deleted. Start atrium with
  `scripts/live/start-atrium.ps1` (deployed as `C:\Users\claude\.atrium2\scripts\start-atrium.ps1`): it starts the hub
  with `atrium.exe run --no-room` and then the room with `atrium.exe room`, and leaves either alone when it already
  runs. Its `-Switch` is accepted and ignored. sgg stages and joins with `atrium.exe` too. `.atrium2\` stays as the
  state directory.

- **`atrium_say` types immediately by default, even mid-turn.** See `docs/test-plan.md` section BI.

  A say waited for the target's turn to end (the N7 rule), so a "stop now" sent to four busy workers reached none of
  them. Now a say is typed in as soon as the target's input line is empty, the keyboard is quiet and no dialog is
  open, whether or not the runner is working. Claude Code queues a line typed mid-turn and reads it at its next step.
  The old rule is an option: `when: "done"` on `atrium_say`, `atrium tell --when done`, and `"when": "done"` on
  `/tell`, `/v1/tasks/{id}/message` and `/v1/tasks/{id}/note/send`. A done message is typed once the turn ends, and
  the permission hook leaves it for the Stop hook. A sender is warned when a done message has nothing to carry it
  (no terminal atrium owns and no Stop hook seen). Answers carry `when`. Each runner has a new setting on the runners
  page, `this runner takes typed input mid-turn`, seeded yes for claude and codex. A runner set to no falls back to
  done for every message. On the board, the say box and the note each get an `immediately` button beside send (and
  ctrl-enter in the say box). `send` now waits for the turn to end. The held `!` chip names what is holding a message
  (your line, the turn, or a dialog) and counts them, `! 2`. Migration `0060_say_when` adds `message.wait_turn` and
  `harness.mid_turn_input`. ROOM-SIDE and HUB-SIDE: the room must be restarted for the delivery rule, the migration
  and the runner setting. The hub restart carries the `atrium_say` `when` field and the board.

- **On the terminals view the toasts sit top right, clear of the input line.** See `docs/test-plan.md` section BH.

  A toast in the bottom right covered the terminal's input line and status bar, where you were typing. While the
  terminals view shows, and in a popped-out window, the toast stack now hangs from the top right, just under the
  terminal's bar and below the paste indicator and the find bar when they show. Every other view keeps bottom right.
  The restart countdown and paused toasts ride the same stack and move with it. A view switch with toasts up moves
  them without fading them in again, and the newest is the top one either way. The new headless section `toastsTop`
  covers it. HUB-SIDE.

- **Selecting the attached terminal again focuses it instead of replaying its history.** See `docs/test-plan.md`
  section BG.

  Clicking the row of the terminal already attached tore the pane down and dialled the same socket again, so the
  daemon replayed the whole scrollback (up to 4 MB) and the scroll position was lost. The same happened from a toast,
  a card's `attach`, the switcher and a `#term=` window. `openTerm`, which every one of those reaches, now treats a
  card that is already attached with its socket open as a focus: it shows the terminals pane and focuses the
  terminal, with no new socket and nothing written. A different card, a closed socket, a runner/shell switch and the
  cursor resyncs still attach as before. The new headless section `reselect` covers it. HUB-SIDE.

- **Atrium being down says so, on an open board and on a reload.** See `docs/test-plan.md` section BF.

  When atrium stopped or crashed with no restart announced, an open board kept looking live apart from a small
  `reconnecting`, and a reload got the browser's `can't reach this page`, which never came back by itself. Now an
  open board that loses its stream and cannot reach atrium for five seconds puts up a red card in the restart
  cover's shape: `atrium is not running`, how to start it, and how long it has been down. It asks again every two
  seconds and comes down once atrium answers and the board has caught up, the same way the restart cover does. A
  planned restart never shows it, and neither does a blip. The service worker now keeps `down.html`, a page that
  stands on its own, and serves it when opening the board fails or a share in front of it answers 502 or 504. That
  page wears the board's last skin, counts, and reloads onto the board once the page answers. The new headless
  section `atriumDown` covers both. HUB-SIDE.

- **The restart notice stays on screen until atrium is back.** See `docs/test-plan.md` section BE.

  The restarting cover came down on the first stream reopen, which could be the old hub's stream coming back before
  the old hub went. A new board build then reloaded the page, and the reloaded page came up bare while atrium was
  still settling. Now `GET /_hub/restart` names the hub process in a new `boot` field, and the cover waits for a
  different name, then for the board build to be read and one refresh to finish. A reload under the cover, from a new
  build or by hand, brings the cover straight back. Nothing else can close it while it is up. A window that missed
  `restarting` keeps the countdown at `0s` until the new hub answers, and the cover then takes over. The gate's
  timings and rules did not change. The new headless section `restartStays` covers each path, frame by frame.
  HUB-SIDE.

- **A click on an alert lands where the alert is about.** See `docs/test-plan.md` section BD.

  Clicking `X is on the board` landed on the stack, not on the terminal that had just opened. A desktop
  notification only ever switched the tab, whether the service worker or the page raised it, and a toast for a new
  card gave up if its terminal was not up yet. Every alert click now goes through one function, `landOnAlert`: a
  toast, a toast log row, a desktop notification with the board in front, behind something or not open, and an
  alert in a popped-out window. An alert about one card lands on its terminal, attached and focused, or raises its
  own window if it is popped out. A card with no live terminal lands on its request if it has one, and otherwise on
  its detail. An alert about no card goes to the view it names, and one about nothing goes nowhere and no longer
  closes an open dialog. A new card whose terminal is not up yet is waited for, up to eight seconds. With no board
  open, the service worker opens one at `/?land=<card>`. `toasts.js` now writes its two NUL bytes as `\0`, so git
  diffs it as text. The new headless section `land` covers each destination. HUB-SIDE.

- **The restart and wait screens look like atrium and say what is happening.** See `docs/test-plan.md` section BC.

  The restarting cover was a bare box with a heading, a rule and `the board comes back by itself when the new hub
  answers.` It is now one card shared with the room-switch cover, in the skin's colours: a turning ring, a label, a
  headline, a line saying how long and that there is nothing to do, a moving bar and a clock. After 30 seconds the
  line says it is taking longer than usual. The countdown toast leads with `atrium restarts in 5s`, shows the
  seconds in a ring and drains a bar over them. The paused toast is `restart on hold` with a pause mark. The
  header's `reconnecting` is a red pill with a breathing dot, in smaller type than before. The words
  say `atrium` rather than `the hub`. Nothing the gate does or waits for changed. HUB-SIDE.

- **A big paste says it is on its way.** See `docs/test-plan.md` section BB.

  A paste of 2KB or more into an attached terminal shows a spinner and `pasting <size>` at the top right of the
  terminal while it is in flight. It appears only after 20ms, so a paste that lands at once never flashes it. It
  goes at the first runner output after the socket's send buffer has drained, or after 20 seconds. The send path
  is unchanged: a paste is still one frame. The input lag log's `socket had N bytes unsent` is now read before the
  key is sent, so it no longer counts the key's own 18-byte frame on every line, and it reads `before this key`.
  HUB-SIDE.

- **A theme preview recolours the card.** See `docs/test-plan.md` section BA.

  The theme picker on the terminal bar previewed a theme on the terminal alone. With card colours on, the attached
  row, its bridge, the pane's frame and the stack and board card kept the saved theme, so the pane showed one theme
  framed in another. Every surface that wears the card's theme now follows the preview, for that card only. Escape,
  attaching to another card and closing the terminal put the saved colours back. `use it` saves the theme as before.
  Nothing is written to the daemon or to localStorage while previewing. The new headless section `themePreview`
  covers a preview, a second pick, cancel, detach, a card switch, zero writes, and `use it` across a reload.
  HUB-SIDE.

- **A new card says so.** See `docs/test-plan.md` section AZ.

  A card launched from a shell, adopted, or brought by a room landed mid-list with nothing to mark it. A card this
  window has not seen before now pulses a teal ring three times over about three seconds, then wears a quiet `new`
  chip. It does this on the stack, on the board and in the terminals pane. The chip clears when the card is
  clicked, rested on for a second, or attached, and on its own after five minutes. If the card lands off screen in
  a list nobody is scrolling, that list scrolls just far enough to show it, and focus never moves. Seen cards are
  remembered per browser in `atrium.newcards`, by bare id, and bounded. So the first list after a load or a
  reload is taken as already there, and a hub restart that brings the same cards back under a new `room~` tag
  marks nothing. Storage is written only when a card is first seen or cleared. Reduced motion drops the pulse and
  keeps the chip. On a card wearing its terminal's colours the chip's text is the title colour on a teal border,
  so it holds 4.5:1 on every terminal theme. The new headless section `newCard` covers the pulse, the chip, a reload, a hub restart, a click,
  reduced motion and zero writes while idle. HUB-SIDE.

- **Untagged follows the sort on the terminals pane.** See `docs/test-plan.md` section AX.

  The pane sorted by name on the address line, but each row leads with its `display_title`. So `untagged` came
  out in path order under a tray saying `sorted by name`. It now sorts on the name the row shows first, the same key
  the board and the stack use. Pinned keeps its pin order, and your own groups keep their hand order. The new
  headless section `untaggedSort` reads `untagged` on all three views under name and activity, and on the stack
  under project and runner too. HUB-SIDE.
- **The unexpected-exit notice.** See `docs/unexpected-exit-wake.md` and `docs/test-plan.md` section AY.

  When the room crashes, is killed or is stopped for a restart or a deploy while a supervised runner is mid-turn
  (`running` or `needs-permission`), the resumed session gets one grey line: `[atrium] unexpected exit: atrium went
  away while you were working (crash|restart) at <time>. Your session was resumed. Check where you were and carry
  on.` A planned stop reads the statuses in the wind-down, before any runner exits. A crash is found at the next
  start, which sees no planned-stop mark and cards still `running`. The notice is a restart-wake row, so it uses the
  wake's gate and typing path unchanged. A card's own wake wins, and a crash loop leaves one notice, not a stack.
  This reverses "no forced turns" for this one case, by clint's decision. The room cog has a setting to switch it
  off, and it is on by default. ROOM-SIDE, and HUB-SIDE for the room cog's checkbox.

- **One atrium binary.** See `docs/test-plan.md` section AW.

  `cmd/atrium2` moved into `internal/cli`, so `atrium` now carries the hub and the room as well as the hooks and
  every session command. The names: `atrium run [--no-room]` is the hub, and without `--no-room` it also starts
  this machine's room detached when none answers, making and enrolling the room over its own link the first time.
  `atrium room` runs a room with the flags it always took, `atrium room join <string>` is a room's first join,
  `atrium rooms add/ls/token/mark/rm/log` and `atrium backups [restore]` are the hub's inventory and snapshots, and
  `atrium db compact` and `atrium ledger` are unchanged. On `atrium run` the hub's own `--dir`, `--db`,
  `--identity` and `--service` are `--atrium-*`, because the plain ones are the room's. `atrium join` stays the
  session command. The v1 `atrium room --hub` client is gone. The defaults are this machine's layout: the board on
  7778, the link on 7779, the room's board on 7781, its agent listener on 7777, and directories beside the
  database. A builds directory may name binaries `atrium_<os>_<arch>` as well as `atrium2_<os>_<arch>`.

  A room now runs the binary the hooks run, so its address file names a binary that answers `atrium hook`, and the
  board's install-hooks button writes lines that work. Before, it named `atrium2.exe`, which had no `hook`.

  `cmd/atrium2` stays as a temporary shim for this machine's live scripts: the same root with `hub`, `join` and
  `room` under their old names, flags and defaults, and the old address file. It answers every line in
  `~/.atrium2/scripts` unchanged, and `atrium2 hook` works too. The new scripts are in `scripts/live/`, and
  `docs/one-atrium-cutover.md` is the window that switches this machine over. Plan: `docs/one-atrium-plan.md`
  stage 3. ROOM-SIDE and HUB-SIDE, but nothing switches until the cutover.

- **Mode B is removed.** `atrium serve`, `atrium status` and `atrium watch` are gone, with `internal/server`,
  `internal/state` and the `run-status`, `run-watch` and `run-serve` make targets. Nothing called them, no MCP
  config registered `atrium serve`, and `gwt watch` already tails the same ledger. `docs/test-plan.md` section E is
  retired. Plan: `docs/one-atrium-plan.md` stage 2. ROOM-SIDE, the hook binary only.

- **The hub answers a read that a silent room holds.** See `docs/test-plan.md` section AU3.

  The hub's proxy has no response header timeout, because the event stream sends nothing until something happens.
  That left every other read unbounded too, so a room that sat attached and silent after a hub-only restart held
  each board poll open for minutes. A proxied GET that is not a stream now waits 12 seconds for the room's headers,
  then gets a 503 that names the room and says it may still be reconnecting. That is under the board's own
  15-second bound, so the board shows the reason. The wait stops the moment headers arrive, so a large download
  that starts promptly is not cut. Event streams and terminal websockets are untouched. HUB-SIDE.

- **The untagged group stays the way you left it in custom mode.** See `docs/test-plan.md` section AV.

  In custom grouping the board's `untagged` group starts shut, so an entry for it in `atrium.folded` means open, the
  reverse of every other project group. The render knew that and the `toggle` listener did not: it treated only the
  offline group as reversed. So opening `untagged` wrote nothing, and the next repaint shut it. Shutting then wrote
  the entry that the render draws as open, and the two went round, with a refresh on every turn. On a board whose
  cards were changing that was over a hundred storage writes and fetch passes a second. Every other window on the
  board followed the shared list and flapped with it: groups expanding and collapsing on their own, and the fetch cap
  full. The render now marks a group that comes shut with `data-fold-shut`, and the listener reads that mark
  instead of guessing from the key. The new headless section `foldStill` opens and shuts `untagged` in one of two
  windows while a card changes every second. It fails on any storage write while they sit idle, or on more than two
  toggles. HUB-SIDE.

- **A room that stops answering no longer fills the board's fetch cap.** See `docs/test-plan.md` section AU.

  After a hub-only restart the room could sit attached and silent for minutes, and the hub holds a proxied read
  open for as long as that lasts. The board capped itself at six fetches, but a held read kept its slot until the
  room came back. Each refresh pass added more, so the queue grew to 7 in half a minute and 9 in a minute. The
  browser also allows six connections per host across every tab and popped-out window on it, and each event
  stream takes one of them. Once the held reads took the rest, nothing left the browser, not even `/v1/health`,
  which the hub answers itself. That is the `fetches 6/6 in flight, N queued` line in the input-lag log. Every
  board read now gives up after 15 seconds, counts as a failure so the refresh loop backs off, and frees its slot.
  Writes are not bounded. The headless run has a new section, `idleRate`: a board, a second tab and a popped-out
  window sit idle through a room-set flip, and then the room goes silent for 40 seconds. It fails if the idle rate
  passes 12 requests a second, or if any window queues more than four fetches while the room is silent. HUB-SIDE.

- **The popped-out window notice says what to do.** Clicking a card whose terminal is in a window this board cannot
  raise (one opened before a reload) now says "this terminal is open in another window: switch to that window, or
  close it and pop the card out again", instead of explaining which windows a page may raise. HUB-SIDE.

- **A popped-out terminal stays out of the board after a room-set change.** See `docs/test-plan.md` section AP.

  The hub spells a card `room~id` while more than one room is attached and bare with one. A restart re-attaches
  rooms one at a time, so the spelling flips. A popped-out window keeps the id from its url, while the board
  re-resolves its card to the current spelling. The board's record of popped-out windows compared the raw ids, so
  it read the card as free and attached it, and the window's claim never matched the board's pane. Both views then
  held the terminal and took input. The record now keys by the bare id, and a claim matches the board's pane under
  either spelling, so the board lets go and the popped-out window keeps the card. Window names use the bare id too,
  so the board can raise a window after the spelling flips. HUB-SIDE.

- **Every tooltip on the board wears the skin.** See `docs/test-plan.md` section AT.

  The board used native `title` attributes in 244 places, and the browser draws those in its own white box
  and the system font whatever the skin is. The held-message `!` chip was the one that got noticed. All of them are
  now `data-tip`, drawn by the panel the `?` bubbles already used: themed from the skin, kept on screen, shown after
  half a second of hover or at once on keyboard focus, and taken down by a scroll that is not terminal output.
  Icon-only buttons carry an `aria-label` for their name. `scripts/check-titles.sh`, run by `check-board.sh`, fails
  on a new native title unless `scripts/title-allowlist.txt` names it with a reason. The allowlist is empty.
  HUB-SIDE.

- **The hub restart gate says which go it gave.** See `docs/test-plan.md` section AQ and
  `docs/hub-restart-gate.md`.

  The script printed `go: nobody is using a board and nobody paused` for every go. A deploy that counted down on
  an open board read exactly like one that found no board, and the gate was blamed for not counting a board it had
  counted: the hub's audit log said `restarting after a countdown nobody paused`. A go now carries `why`, either
  `no board is open` or `counted down on N board stream(s) and nobody paused`, and the script and the audit line
  both say it. Tests pin that every stream spelling a board can use counts for the gate and hears the countdown,
  on a single-room hub and a two-room one, and that input from a scoped board reaches the gate. HUB-SIDE.

- **The room's input-lag echo line says whose time it was.** See `docs/test-plan.md` section AS and
  `docs/input-lag-logging.md`.

  A 2.9s keystroke logged the same gap on the hub (2926.7ms) and the room (2926.2ms) and no other line, so the
  runner was to blame only by elimination. The room's `echo` line now ends with `runner Nms, atrium Nms`, split at
  the moment the pty handed output over. ROOM-SIDE.

- **Mode A is removed.** Stage 1 of `docs/one-atrium-plan.md`. See `docs/test-plan.md` sections A to D and F.

  `atrium hub` (the v1 terminal UI), `atrium agent` (the `atrium-agent` MCP server), `atrium daemon --tui`,
  `internal/hub`, `internal/agent` and `internal/tui` are gone, and so are the Bubble Tea, Lip Gloss and Bubbles
  modules. The agent listener no longer answers `/submit`, and the board API no longer has
  `POST /v1/tasks/{id}/prompt`. Nothing called either: the board never used the route, and only a session parked
  in the submit loop could receive what it sent.

  The daemon kept one piece: the `/permission` long-poll the dotfiles permission hook blocks on. It moved to
  `internal/daemon/permwait.go` unchanged on the wire, with the same URL, request fields, response fields and
  fail-open posture. The default database directory is `daemon.StateDir`, which returns the same path as before,
  `hub` segment included, so nobody's database moves.

  The repo's `.mcp.json` holds no servers now, but it still contains the word `atrium-agent`. The dotfiles hook
  gates a session whose `.mcp.json` mentions it, and that keeps a session opened in an atrium checkout gated.
  ROOM-SIDE, and the hook binary.

- **Groups on the terminals pane wear their colour and move by drag, and a card can leave a group by name.** See
  `docs/test-plan.md` section AR.

  The terminals pane drew every group heading in the label grey, so a recolour made on the stack or the board
  never showed there, and one made from the pane changed only the other two views. Its headings and the line down
  their rows now take the group's hue from the same place the stack and the board read it. The name blends toward
  the skin's text colour, so it stays readable on a light skin. Every named group's heading takes the group menu,
  so a group can be recoloured from the pane in any grouping mode. The colour is a per-browser preference. A second window on
  the same browser used to keep the old colour until its next poll. It now repaints when the grouping changes.

  In the `custom` grouping, a group heading on the terminals pane drags. The heading and its rows move together,
  and the drop writes the same order `move up` and `move down` write, so the stack and the board follow. `pinned`
  stays on top. A heading drag and a row drag are told apart, so a heading dropped on the pinned bucket pins
  nothing and a row dropped on a group reorders nothing. In other modes the groups are sorted by a rule, so no
  heading drags, and the tooltip says where reordering lives.

  The card menu on the stack, the board and the terminals pane has `out of <group>`, which takes the group's tag
  off. The terminals pane menu also gets `into group`, which it lacked. On the terminals pane, a row dragged from a
  group onto `untagged` leaves that group.

  Clicking a card or an alert whose terminal is popped out raises that window and no longer draws "the board sent
  you here" in it. The window coming to the front says it. The board still says "it is in its own window" in a
  browser with no `BroadcastChannel`, where it cannot tell whether the raise landed. `docs/test-plan.md` P1 is
  updated to match. HUB-SIDE.

- **A paused hub restart stays paused until somebody resumes it, and toasts stay on screen.** See
  `docs/test-plan.md` section AO and `docs/hub-restart-gate.md`.

  A click on the countdown held the deploy only until its 300 second wait ran out. Now a pause holds with no
  timeout. The wait counts only while the boards are busy, and a resume starts it over. No request hangs for the
  whole pause: the hub holds each one for at most `hold` seconds (25 by default) and then answers `waiting` with
  an ask id, and `scripts/hub-restart-gate.ps1` asks again. An ask that nobody polls for 30 seconds is dropped,
  and its countdown comes off the boards. When the hub dies or restarts while the ask waits, the script says so
  and exits 4. A script that sends no `hold` gets the old single request, and its pause still ends at `paused`.

  The countdown and "hub restart paused" toasts stay up until the hub says what comes next. The toast cap no
  longer counts or evicts them, and anything else that removes one sees it put back. A window opened during a
  countdown now shows the time left.

  Some toasts popped and went at once. The board keyed every alert toast by its subject. The same poll that
  raised the toast then reaped every keyed toast that was not a waiting card or a pending request. So "... is on
  the board", a stuck launched agent, a share that stopped and a fixture that failed all vanished in under half a
  second, and their desktop notifications closed with them. Only a pending item, a request or a card waiting on
  you, now gets a key. HUB-SIDE.

- **A prepare command works next to a profile that prints.** See `docs/test-plan.md` section AN.

  The prepare command runs in a shell that loads the operator's profile, then dumps the environment on stdout.
  Atrium read all of stdout as that dump, so a profile line such as `Write-Host "docker -> ..."` failed every
  launch with a prepare command, with "invalid character 'd' looking for beginning of value". The dump is now
  fenced by a line carrying a fresh random nonce on each side, and atrium reads only what is between them. The
  profile and the prepare command may print anything. The profile still loads. When the fences are missing, the
  launch error quotes the first few hundred bytes the shell printed instead. The same fence wraps the `env -0`
  dump on Linux and macOS, where a login shell's profile can print too. ROOM-SIDE.

- **A turn a Stop hook continued now ends on the board.** Step 1 of `docs/turn-end-spike.md`.

  When the Stop hook hands a session a queued message, Claude Code runs the next turn with `stop_hook_active` set
  on its Stop. The hook returned before it posted that Stop, so the room never heard the turn end. The card stayed
  `running` with the previous turn's unread dot, and no silent-stop check ran for a launched worker that went
  quiet. The hook now posts every Stop, with `stop_hook_active` in the body, and still prints nothing on a flagged
  Stop whatever the room answers. The room treats a flagged Stop as a turn that is over: the card goes to
  `needs-input`, the turn is noted for seen, and a launched worker that owes a report gets its silent-stop notice
  to the launcher. The room never answers a flagged Stop with a block, and it leaves queued messages queued for the
  next hook or the typist. Nothing forces a turn. The hook binary needs a rebuild along with the room. ROOM-SIDE.

- **A session can ask to be woken after a restart.** See `docs/restart-wake.md`.

  A room restart ends every terminal it owns, and a resumed session sits idle until somebody types into it. A
  session now calls `atrium_wake_after_restart` with a prompt before the restart, and a deploy script can POST the
  same thing to `/v1/tasks/{id}/restart-wake` by card id or wire name. The room keeps it in a new `restart_wake`
  table, so it survives the restart it is about. Once the card's new runner is up, its session has started, the
  turn is over and the input line is empty, the room types the prompt in behind a grey `[atrium] restart wake:`
  label, the same style as a peer's typed message, sends it, once, and deletes it. It never types into the runner
  that queued it. One wake per card, a newer one replaces the older. A wake does not expire: it waits however long
  the card takes to come back, and goes only when it is typed, cleared, replaced, or its card is removed. The card
  shows `wake queued` while it waits. Queued, replaced and cleared are `notified` events on the card, and the
  delivery is a `prompted` event from `restart-wake`. ROOM-SIDE and HUB-SIDE: the tool and the chip are the hub's.

- **A `website` skin: the board wearing the docs site.**

  The site's dark palette is harbour's already, so the skin is harbour's palette plus the three things the site
  adds: primary buttons filled with the teal-to-blue gradient on a coloured glow, a frosted header, and a soft teal
  and blue glow behind the top of the board. It is the one skin that carries rules beyond a palette, and every rule
  is scoped to its selector. No size, space or radius moves. The headless test checks the effects are there in
  `website`, that harbour reads the same before and after `website` is worn, and that harbour and noir carry none
  of it. HUB-SIDE.

- **A hub-only deploy asks the board before it restarts the hub.** See `docs/hub-restart-gate.md`.

  `POST /_hub/restart`, loopback only, holds the deploy script's request until nobody has used a board for ten
  seconds. Every board window reports keystrokes, clicks, scrolls and pastes to the hub, the popped-out ones too.
  The hub then pushes a `hub-restart` countdown to every board stream: "the hub restarts in 5s unless you click
  this". A click in any window pauses the restart everywhere, and a sticky "hub restart paused" toast with a resume
  button holds it until somebody resumes. Typing during the countdown takes it down and waits for quiet again. On
  `go` every board covers itself with a "the hub is restarting" modal. The modal clears when the event stream comes
  back. With no board open the answer is `go` at once. `scripts/hub-restart-gate.ps1` is the deploy's side: exit 0
  to restart, 3 to leave the hub alone. A hub older than the gate answers 404 and is restarted the old way.
  Covered by Go tests on the gate and the endpoint, and a headless case for the countdown, the pause, resume and
  the modal. HUB-SIDE.

- **The attached terminal's frame matches the bridge running into it.**

  With card colours on, the attached row and its bridge are framed at 3px in the row's title colour, and the
  terminal pane they run into kept a 1px border in the theme's cursor colour, so the outline thinned and changed
  colour at the pane. `placeTabBridge` now hands the attached row's `--framew` and border colour to the pane, and the
  bridge reaches across the pane's whole left border so no line crosses the join. A plain row, its bridge and the
  pane stay 1px. The headless bridge check fails when the pane's frame differs from the bridge in width, or in
  colour on a worn row, on noir and daylight, and it now runs in the full headless pass. HUB-SIDE.

- **The attached row's bridge is as thick as its frame.**

  With card colours on, the attached row's frame is its 1px border plus a 2px inset line, 3px in all, and the
  bridge across the divider drew its edge at 2px, so the frame stepped down where it met the terminal. The frame
  now says its thickness in `--framew` and the bridge takes its edge from that, so both are 3px on both copies of
  an attached row and on light and dark skins. A plain row's border and bridge stay 1px. The headless bridge check
  measures the frame, border plus inset line, against the bridge's edge on noir and daylight. HUB-SIDE.

- **The work ledger records who was handed what, and where it is.** Stage 1 of `docs/work-ledger-design.md`.

  A card another session launches now gets a work item in the room's store. It holds the brief, the launcher and
  a work state beside the card's column. A board launch gets none. A worker's `done` report still moves its card
  to `done`, and moves the work to `reported`, which means waiting on the launcher, not finished. Nothing in this
  stage closes work: the launcher's verdict is stage 2. `progress`, `blocked` and `question` reports, and every
  message between a worker and its launcher, are logged on the item verbatim. The log is capped at 300 rows and
  1 MB per item and trims chatter first. It never trims the latest `done` report.

  A session that ends before a `done` report moves its work to `ended-without-report`. This happens in the same
  transaction that records the exit, so no exit path can miss it. One notice is queued to the launcher in that
  transaction, naming how the session ended and its last report. Exits are keyed on generations, so one death
  seen by the supervisor, the reaper and the session hook is one log row and one notice. Resuming the card moves
  the work back to `open`. Atrium never resumes anything itself. A sweep on start and on every reaper tick ends
  work whose process is gone with no exit recorded. It leaves alone work whose liveness it cannot know.

  A report's writes (recap, commit, event, column, `reported_at`, the item and the launcher's notice) are now one
  transaction. Before, a failure between two of them left a card half reported, and a notice could be recorded
  as sent and then lost. The prune sweep keeps a card whose work is still open, and a pruned card's item stays.

  The room rewrites `work-ledger.md` beside its database on every change. It lists open work with the crash case
  first, then work closed in the last seven days. A failed write is logged and changes nothing else.
  `atrium2 ledger` prints the same list from the database opened read only, with `--json` for scripts. On first
  start, launched cards from the last 14 days are backfilled once and marked inferred. Migration
  `0058_work_ledger` adds two tables. ROOM-SIDE. `internal/cli` is unchanged, so the hook binary needs no rebuild.

- **A peer message waits for an empty line and the end of the turn before it is typed.**

  A message typed into a session that was mid-turn did not submit. Claude Code held it until the turn ended and
  then sent it with whatever the operator had typed in the meantime, as one prompt. A peer's message is now typed
  only when both hold at once: the operator's line is empty and quiet, and the runner's turn is over. Until then
  it waits on the held-message retry, and the permission and Stop hooks leave it for the typist. A cleared line
  lets it through. The turn ending re-arms the retry, so it lands about two seconds later. The operator's own
  channel is unchanged, and after a restart the hooks deliver a waiting message as before. See
  `docs/typing-race.md`. ROOM-SIDE.

- **Codex's hooks no longer fail, and its cursor stays at the prompt.**

  Every codex hook line atrium writes ends in `--runner codex`, and only `atrium session` knew that flag. So
  `atrium hook` exited 1 on codex's PreToolUse, PostToolUse and UserPromptSubmit, codex printed `Hook failed`
  after every command, and the card never showed a tool running. `atrium hook` and `atrium turn` now take
  `--runner`. A hook subcommand now ignores a flag it does not know, and any error parsing a hook line exits 0.
  `hook install` and `hook status`, which people type, still fail on a typo. `atrium turn` now sends its runner,
  and the room's Stop handler uses it: a codex card went back to claude's mark on its first turn ending. This is
  `internal/cli`, so the hook binary needs a rebuild as well as the room (ROOM-SIDE).

  Codex reaches the board through ConPTY, which splits each of codex's frames into two writes. The first shows
  the cursor wherever the last cell was drawn, often one of the dots codex animates across its input box. The
  second moves it back to the prompt about 2ms later. The board painted between the two, so the cursor jumped
  around the input box. Claude moves its cursor to the prompt before every show, so it never did this. A new
  `runnerprofile` table holds what each runner needs from the terminal and hooks. Codex's entry sets a 40ms
  cursor settle, which the room sends in the attach `caps` message. For such a runner the board hides the cursor
  after each write and shows it again once output has been quiet that long, if the runner last asked for it
  (ROOM-SIDE, HUB-SIDE). Claude, gemini and ollama have no settle and are written through as before. The dots
  are codex's own drawing.

  A fake runner test now installs each runner's hooks into a scratch file and runs the commands it wrote through
  the real command tree. It fires them in session order against a test daemon, with that runner's payloads, and
  checks the card's column and badge after each one. It needs no agent and no tokens.

- **Terminals list: exited rows fade in their theme, both copies of the attached row bridge, groups can be
  removed.**

  `exited rows keep their theme` laid a 70% wash of the list's surface over an exited row, which on a light skin
  turned it dark grey. It now keeps the row's theme under the ordinary exited fade, the pale, tinted look. A
  pinned session filed into a group is drawn twice, and only the first copy got the strip that bridges the
  divider into the terminal; each copy now gets its own, and a worn row's strip carries its 2px frame. A group
  made with the list's `+ new group` could not be removed from the list: its heading now takes the board's
  group menu on a right click, with move, rename and `remove from the view`. Removing one keeps every card's tag
  and closes nothing; its cards go back where they sit without it. HUB-SIDE.

- **The board reads `/v1/settings` once per load, and the read is cheaper.**

  A load read settings four times on a hub, from the skin, the global auto button and both again when the room
  list landed, all inside the first read's round trip. Reads for the same scope now share the one in flight
  (HUB-SIDE). Each read also resolved every browse root with `EvalSymlinks`, one per worktree, for a list only
  the settings dialog shows. The settings answer now reuses the last resolve for 30s while the configured list
  is unchanged; `/v1/browse`, where the roots are a permission, still resolves fresh. With 160 worktrees a read
  went from 124ms to under 5ms (ROOM-SIDE).

- **A docs site and landing page, in `website/`, describing atrium as of 0.0.1.**

  Docusaurus, themed from the board itself: the default skin's navy, teal and blue in dark mode, the `daylight`
  skin in light mode, `active-work` green for workers, and the A redrawn as an SVG. The landing page draws the
  board, a permission request and a supervised terminal in HTML rather than showing screenshots, so it follows the
  theme, reflows on a phone and shows no real paths. The docs cover install, the quick start, the two ways to run
  (watch and gate in your own terminal, or supervised), the board, cards, permissions and auto mode, terminals,
  messages, files, history, rooms, overlays, runners, intake, settings, the CLI, the control MCP server and hooks,
  plus the story of why atrium exists, told from the changelog and the decision records.

  It is refreshed at release time, not per feature. `scripts/build-docs.ps1` builds it and checks that every link
  carries the `/atrium/` base URL, and `.github/workflows/docs.yml` publishes it to GitHub Pages on a published
  release or by hand, never on push. Nothing has been published. `website/DECISIONS.md` records why Docusaurus,
  why no Tailwind, and where each colour comes from. DOCS ONLY.

- **Permission events can stay out of the database, and an old database can be compacted offline (opt-in).**

  A 57 MB live room database held no `output` events at all. Permission traffic was the bulk: `perm-requested`
  and `perm-decided` were 48k of 62k event rows and 14 of 16 MB of event payload, and the `permission` table
  with its indexes was another 25 MB. The perm events repeat what the permission table already holds.

  A new setting, `event_cold_kinds`, names event kinds that go to the cold sinks only, never the db. It is empty
  by default. It only takes effect with a cold sink in `event_sink` (say `db,file`), because an event routed cold
  with no cold sink would be written nowhere; without one it is ignored and logged. `created` and `submitted`
  are read back from the table and always stay in the db. The card's detail dialog now says what its history
  does not hold: kinds kept in the event archive only, and older events rolled off the hot window.

  `atrium2 db compact --in <db> --out <db>` writes a packed copy with `VACUUM INTO` and switches the copy to
  incremental auto_vacuum, which an existing file cannot do in place. `--window-bytes` applies the hot window to
  every card on the copy and `--drop-kinds` removes kinds from it. The input is never changed or replaced, the
  output must not exist, and a database anything has open is refused. On a copy of the 57 MB database: 54.8 MB
  plain, 37.7 MB with a 256 KiB window, 28.7 MB with the two perm kinds dropped, 27.8 MB with both. What is left
  is mostly the permission table. ROOM-SIDE, HUB-SIDE.

- **The terminals list decides which rows wear their terminal's theme.**

  Only the selected row in the terminals list was drawn in its terminal's colours, unless the board-wide `cards
  wear their terminal colours` was on, and then every card everywhere was. The gear's board pane now has three
  switches for the list, each on its own and kept per browser: `the selected row wears its theme` (on, the old
  look), `rows wear their theme when not selected` (off), and `exited rows keep their theme, faded` (off).
  The third draws an exited row in its theme under the fade every exited row gets, so a dead session reads as
  its colour, pale. Off, an exited row is the skin's card faded to grey whatever the second says, since one at
  full strength reads as live. The board-wide setting is now `board cards wear their terminal
  colours` and covers the stack and the board. A browser that had it on starts with the list's second switch on.
  HUB-SIDE.

- **Opening a terminal stays fast however many restarts its session has lived through.**

  Every attach replayed the whole of the card's saved pre-restart history, and that file grows by a ring's worth at
  each clean stop, bounded only by the scrollback setting. The main atrium card had lived through 27 restarts and
  held 23MB. Each attach ran all of it through the screen model (about 360ms) and sent 2.5MB and 21,000 lines to a
  browser that took another 800ms to draw it. An attach now replays the newest 4MB of that history, with a line at
  the top that says the rest is under the terminal's cog, `history from before the restart`. The reprint that
  follows a restart is looked for near the end of the file only. On the same card the attach now costs 90ms in the
  daemon and sends 790KB, and the cost no longer rises with each restart. The file on disk is unchanged. ROOM-SIDE.

- **A finished worker's badge stops reading `thinking`.**

  A worker that called `atrium_report` with `done` kept a `thinking` badge after its turn ended. The report marks
  the card done and forgets the activity, but the tool-end hook that follows sets `thinking` again. The Stop hook
  then saw a card that was not `running` and returned before it reset the badge. A Stop now sets the badge to idle
  whatever column the card is in, and moves the column only from `running` or `dead` as before. ROOM-SIDE.

- **Scrollback keeps what a session drew before its terminal grew taller.**

  gwt opens a runner in a console about thirty rows tall and the board attaches later at sixty. The ring wrote
  each height change over the mark in force, so the screen replay drew the whole session into a sixty row grid.
  Output drawn at thirty rows never scrolled into history, and the next repaint from the top of the old screen
  erased it. The operator saw one screen of a fresh session. A height change now lays a mark the way a width
  change does, the replay resizes the grid's height at each mark, and shrinking files the top rows into history.
  On the capture from the card that showed it, the replay went from 62 lines to 207. ROOM-SIDE.

- **The header's global auto button is never an empty pill.**

  After a deploy the button could draw as a grey pill with no dot and no word. Only a settings read that landed
  ever painted it, and a hub that was just restarted answers `/v1/settings` with a 409 until a room attaches. A
  board that loaded or reconnected in that window never read it again, because only the skin retried on a room
  attaching. The button now starts as `asking` in the markup, shows `auto: unknown` if the first read fails, and
  dims the last answer as stale if a later one fails, so an old `approving everything` does not pass for a fresh
  one. The board poll and a room attaching re-read it until a read lands. This covers the ALL view and a room
  scope whose room is not back yet. HUB-SIDE.

- **Atrium knows whether you have seen a session's last turn, and whether you answered its Open Questions.**

  An orchestrator ended turns with an `Open Questions:` block while eight workers ran, the operator never saw
  them, and the orchestrator kept referring back to questions nobody had read. A card whose last turn nobody has
  seen now wears a teal dot, and one whose turn ended on unanswered Open Questions wears `? N`, with the questions
  in its tooltip. Both show on the board, the stack and the terminal strip. A turn is seen when a focused, visible
  window shows that card's runner terminal scrolled to the bottom for 3 seconds, or when you type into it, submit
  a prompt, or send it a message. A peer's message never counts. The questions are read off the Stop hook's last
  message and only the numbered lines are kept, never the message. A reply answers them. `atrium_task` returns a
  `seen` block and defaults to the caller's own card, and `atrium_peers` counts unseen turns and open questions.
  Stored in a new `turn_seen` table (migration `0057_turn_seen`), so it survives a restart.
  `docs/seen-design.md` has the design and its open questions. ROOM-SIDE and HUB-SIDE.

- **The history view scrolls, and a live update keeps your place in it.**

  History had the audit pane's fault: it was as tall as its rows and the board's main area clips, so every run
  below the window was out of reach. The list now scrolls in its own box under the heading and the search bar,
  on a desktop and on a phone. A board event used to redraw only the first page, so after `show more` a live
  update cut the list back to a hundred rows. It now re-reads every page already shown and keeps the row you
  had scrolled to. A new search or a new visit starts at the top. Changing pane on the rooms page scrolls back
  to the top again. Nothing reached the audit pane on a board event, which logged `no renderer for the audit
  view` on every refresh; that is gone. HUB-SIDE.

- **Cards can wear their terminal colours.**

  Only the attached card in the terminals list was drawn in its terminal theme. Every other card showed its theme
  as a stripe, so finding a session by colour meant attaching to it first. A new board setting, `cards wear their
  terminal colours`, draws every card in its own theme: the terminals list, the stack and the board columns, and
  the terminals list at phone width. Off by default, and off is the look it was. The card's palette is rewritten
  on the card itself, so its chips, star, wait timer and waiting edge take the theme's ANSI colours with no rule
  of their own. Every title, path and chip is held to 4.5:1 against the surface it sits on. Six themes' accents
  were under that as title text (`deep-amethyst`, `monokai`, `orange-coral`, `orange-marigold`, `orange-tangerine`,
  `wrought-iron`), and several ANSI yellows, cyans, blues and reds were too, most on `mint` and
  `orange-marigold`. Those are moved toward white or black until they pass. A room chip on a coloured card carries
  its room's hue in its border rather than in its text. The attached card keeps a two-pixel frame, open toward
  the pane. The setting is per browser. HUB-SIDE.

- **A session launched by another session that stops without a word is reported, not left sitting.**

  An agent-launched worker (`atrium_launch`) now owes its launcher a report every turn: `atrium_report` on the
  control MCP, `atrium finish --status done|blocked|question|progress --sha ...`, or any message to the launcher.
  A turn that ends with none is a silent stop. The launcher hears at once, from the worker's handle, and the board
  rings `X is STUCK: it stopped without reporting, n minutes` on a 1m, 2m, 5m, 10m, 30m, 1h ... 24h backoff that
  resets when the card moves. A tool call running past 20 minutes is reported the same way and never killed. A
  stuck permission nags on the same backoff. Atrium never forces a turn: the Stop hook only reports.

  A `done` from a worker needs a sha or a reason there is none, and a sha the worktree does not contain is
  accepted with a `sha unverified` chip. Launches record their launcher (`spawned_by`, `@human` for the board's
  dialog). Agent-launched claude sessions get the Stop hook through `--settings` when the operator has not
  installed it. A message to a session nothing will deliver to (a gemini card atrium does not own, a claude
  session with no hooks) now answers `undeliverable` or `queued-unconfirmed` with what to do instead. Migrations
  `0055` and `0056`. See `docs/a2a-reliability-design.md`. ROOM-SIDE and HUB-SIDE.

- **The audit pane scrolls.**

  The pane was as tall as its rows and the board's main area clips, so every event below the window was out of
  reach. The feed now scrolls in its own box, and the heading and the room and kind filters stay above it, on a
  desktop and on a phone. A live event lands at the top without moving the row a reader has scrolled down to. A
  filter change starts the list at the top. HUB-SIDE.

- **A runner's row says what stops it working where atrium launches it, and fixes what atrium may fix.**

  Gemini stopped at its trust prompt in every new worktree, because it had never been told to trust the folder.
  A runner with a setup adapter now carries a `setup` chip on the runners pane. Its dialog lists named checks, a
  `fix` button for the ones atrium applies, and a command to copy for the ones it only explains. Gemini checks
  folder trust against the provider roots and trusts a root with one `TRUST_FOLDER` line. It checks sign-in and
  warns when a key sits in the runner's env in atrium. A gemini card launched in a new folder inside a provider
  root is trusted before gemini starts. A `DO_NOT_TRUST` rule is never overridden, and sign-in is never applied.
  Every edit keeps `.atrium-original.bak` and `.atrium-last.bak` beside the file. Claude has an adapter too, for
  sign-in and hooks. `docs/runner-setup-design.md` has the design and the checklist for the next runner.
  ROOM-SIDE and HUB-SIDE.

- **The terminals list's controls are a tray that rolls up to one summary line.**

  The sort, hide and group controls were a sticky header inside the list's scroll box, and the cards scrolled
  under it. They are now a panel of their own above the rows, and the rows scroll in a box beneath it. Folded, the
  tray is a summary such as `sorted by activity · by project · hiding inactive subagents (2)`; a click rolls it
  down to the controls, laid out as labelled rows of even pills: sort (name | activity), hide inactive (agents |
  subagents) and group, with `+ new group` as a full-width row under the six modes. The width buttons stay on the
  tray's bar. Folded or open is remembered per device and defaults to folded. On a phone the same tray sits at the
  top of the switcher and replaces the `filters` button. HUB-SIDE.

- **The `pinned` heading counts shown out of total, like every group heading.**

  `hide inactive` now reaches into the pinned bucket, so its heading reads `5/15` while pinned rows are hidden, and
  its tooltip says how many. A bucket whose rows are all hidden says `N hidden by hide inactive` instead of
  offering a first drag. Group headings already read shown/total, and pinned cards filed into a group now count
  there the same way. HUB-SIDE.

- **`hide inactive: agents` hides the greyed-out rows.**

  The toggle hid agents with no live connection but exempted every pinned one, and in the terminals list only a
  pinned row can be cold. So the rows it drew grey were exactly the rows it would not hide, and it hid nothing. The
  grey styling and the toggle now read one test, `termCold`, and a pin no longer exempts a row from either toggle.
  With both toggles off a pinned row still stays after its session exits, drawn cold. HUB-SIDE.

- **Every on/off row on the runners page switches from its own pill. The separate enable button is gone.**

  Runners, fixtures, sources, providers, recognisers and actions all draw the fixtures' `on`/`off` pill, and a
  click flips the row. It is a button with `role=switch`, so tab, space and enter reach it, and its title says
  what a click does. The pill changes at once. If the save is refused, it goes back and the reason shows the way
  every other error on the page does. It is one width on and off, so the columns after it line up, and 40px tall
  at phone width. A runner whose command is not on PATH can still be switched off but not on. Rooms keep their
  `marked for deletion` chip and the mark button in the room's cog. HUB-SIDE.

- **Typed text stays in claude's input box after attaching to a session that is already running.**

  The attach replays the session through a screen model, and that replay folded every run of blank rows to one,
  including the two blank rows claude leaves under its banner on the live screen. Everything below them arrived one
  row high, and on a long session the screen's first row was not the top of the viewport either. Claude draws its
  input box with absolute moves once a line wraps, so the second line of input landed on the rule under the prompt
  and the status lines drew twice. A fresh attach hid it, because claude repaints right after it starts. The live
  screen is now replayed row for row at the pty's height, the history still folds its blank runs, and the cursor is
  put back with an absolute move. ROOM-SIDE.

- **The agents half of `hide inactive` is back.**

  It was removed along with the shown/total group counts, and that removal was a mistake. The `agents` segment
  again hides agents with no live connection, and the `subagents` segment hides subagents that are not working
  right now, each on its own. A pinned session still stays whatever either says, and group headings still read
  `7/15` when rows are hidden. HUB-SIDE.

- **Typed text lands in claude's input box again, not on the rule above it, in a window taller than the pty.**

  The pty's height follows the shortest attached viewer, but a taller window kept its own row count. ConPTY places
  every row with an absolute cursor move and scrolls with a newline on the pty's last row, so in a taller grid that
  newline scrolled nothing and every later row landed one above where the runner put it. A window taller than the
  pty now draws the pty's rows, sat on the footer, and still tells the room its own height so the pty can grow back
  when the short viewer leaves. HUB-SIDE.

- **A claude terminal never goes narrower than 120 columns. A narrower window scrolls sideways.**

  Claude reprints its whole conversation at every width it passes through and keeps each copy in the scrollback,
  so a narrow width left a narrow copy nobody could read. A new `terminal_min_cols` setting (default 120, from 40
  to 400, out-of-range values refused) sets a floor. The room holds every runner terminal at or above it whatever a
  viewer reports, and the board sends the floor and draws its width with a horizontal scrollbar, phones included.
  A shell is exempt, since it does not reprint. The first time a pane goes under the floor, a notice says so, with
  a "do not show this again" box and a link to the field in the settings cog's board pane. Saved from the ALL view,
  the hub passes it to every room. After deploy, any session narrower than 120 grows to 120 once, on its next
  attach. ROOM-SIDE and HUB-SIDE.

- **A terminal's scrollback reaches back past a room restart again.**

  After a restart a claude terminal showed only what claude reprints on resume, which is its recent conversation,
  and everything older lived only in the text scrollback view. The attach now puts the saved pre-restart history
  in front of the live replay, cut where the reprint picks it up, so the older history shows once and the recent
  part is not doubled. The cut matches text, not bytes, since the reprint is drawn at another width. When no line
  matches, the saved history is kept whole. On a real 11 MB card, 81% of the saved history is kept and the joined
  replay renders in 150 ms. Also reverts the scrollback clear at a width change, which wiped that history. ROOM-SIDE.

- **A narrow window no longer shrinks a shared session for everyone. It scrolls sideways instead.**

  The pty's width now follows the widest attached viewer, and its height still follows the shortest. It used to
  follow the narrowest, so a phone or a small popped-out window dragged every other window's Claude session down
  to its width, and Claude reprinted the whole transcript at that width into everybody's scrollback. The room tells
  each viewer the pty's size in a new `{"t":"size"}` frame, and a window narrower than that draws the full width
  with a horizontal scrollbar. The hub must carry this before any room does: an older board prints the new frame
  into the terminal. ROOM-SIDE and HUB-SIDE.

- **Dragging a window's edge tells the runner its new size once, not once per step.**

  Claude Code redraws its whole conversation on every resize and keeps the old copy in the scrollback, so one drag
  left a full copy of the transcript per column it passed through. The terminal still fits while you drag. The size
  goes to the runner once it has held still for a quarter second. HUB-SIDE.

- **A group heading says how many it is showing out of how many, and the agents hide toggle is gone.**

  When `hide inactive` takes rows out of a group, its heading reads `7/15` rather than `7`, and its tooltip says
  how many are hidden. A group with nothing hidden reads its plain count. The count sits a little further from the
  name. The `agents` half of `hide inactive` is removed: a pinned row no longer hides, and the only exited agent
  the list ever holds is a pinned one, so that toggle hid nothing and only looked like a switch left on. A stored
  `on` from before is ignored. HUB-SIDE.

- **Nothing atrium types lands in a line you are writing.**

  Each terminal counts what you have typed and not sent, and atrium only types when that count is zero and the
  keyboard has been quiet for two seconds. Two holes let it type over you. The board sends shift-enter as ESC CR and
  ctrl-enter as a bare newline, and the count read both as sending the line, so a multi-line prompt counted as
  empty after its first newline. And only peer messages checked the count: the operator channel, a card's note and
  an action typed straight in, which is how a script's `/rename` landed mid-sentence. Newlines inside a prompt and
  carriage returns inside a paste now add to the line, and every automated write goes through the one gate. A held
  write is queued and retried on screen from two seconds out, and an action that ends in exit skips the exit when
  its prompt was held. Room-side.

- **A pinned terminal stays in the list after its session exits, whatever "hide inactive" says.**

  The hide-inactive toggles treated a pinned session like any other, so a pinned session that exited left the
  pinned bucket and its groups. Pinning means "keep this here", so a pinned row now always shows, drawn cold, and
  a click still starts it again. This broke in 02fe769, when the pinned bucket started reading the filtered list.
  HUB-SIDE.

- **A pinned terminal filed into one of your groups shows up in that group.**

  The terminals list took every pinned card out of the groups and drew it only in the pinned bucket. Filing a
  pinned card with `into group` added the tag, and the group still said 0 and asked for a card to be filed. A drag
  onto the group was worse: the row moved on screen as the pointer travelled, the repaint saw the same markup as
  before and skipped, and the card sat under a group that counted 0. A pinned card now draws in the pinned bucket
  and under every custom group it carries, and a finished or abandoned drag always repaints the list. HUB-SIDE.

- **A card in a directory with a dot in its name resumes its conversation after a restart.**

  Atrium found a directory's transcripts by turning its colon and slashes into dashes. Claude Code turns every
  character that is not a letter or a digit into a dash, so `build.claude` is `build-claude` on disk, and atrium
  looked for a folder that was never written. Every restart logged "which is gone. starting fresh". The session
  list, resume, export and throwaway promotion all read the same lookup. Room-side.

- **A reattach after a room restart no longer draws a blank line under every full-width diff line.**

  A resumed session reprints its transcript at the width the card was saved at. The replay laid that history into a
  grid at the pane's newer, narrower width, so the padding on each full-width line wrapped onto a row of its own. The
  replay now resizes its grid at each width mark, so every run stays at the width it was drawn at. A wind-down also
  stops growing the pty to the widest viewer as viewers detach, which had saved the card wider than the pane reading
  it. ROOM-SIDE.

- **A peer message lands seconds after a terminal frees up, and asks and answers are typed too.**

  A message that met a busy terminal was retried on screen at one minute, then two, then five, so it could sit for
  most of a minute on a terminal that was already free. The retry now starts at two seconds and widens to four
  hours (2s, 5s, 10s, 30s, 1m, 2m, 5m, 10m, 30m, 1h, 2h, 4h), and a keystroke still resets it to the front. No
  warning goes to the board in the first minute. `atrium ask --peer` and `atrium answer` were queue only, so an
  answer to a blocked asker with no Stop hook never arrived. They now deliver the way `atrium tell` does: typed
  when the terminal is free, queued and retried when it is not. A message from a session through
  `/v1/tasks/{id}/message`, which is how `atrium_say` arrives, now gets the peer bus's 8000 character cap and 20
  per minute limit. The operator's own messages stay unbounded. Room-side. See `docs/agent-messaging.md`, which
  is new, as is `docs/how-atrium-works.md`.

- **Attaching to a claude session no longer draws the typed input over the banner.**

  Claude turns the kitty keyboard protocol on and off (`CSI > 5 u`, `CSI < u`) and sets xterm's modifyOtherKeys
  (`CSI > 4 ; 2 m`), at startup and again after a key like ctrl-delete. The room's replay only knew `?` as a private
  marker, so it read those as a plain cursor restore and a dim underline. The next redraw of the input row landed
  wherever the cursor was last saved, which is the top-left unless something saved it, and every attach replayed
  it there: `load this prlandeinspect2it.280` over `Claude Code v2.1.280`, or the typed line over the rule above the
  prompt after a tab return re-attached. The replay now ignores CSI sequences with a `<`, `=` or `>` marker, the way
  a terminal does. Room-side.

- **A terminal that draws something wrong can hand over the bytes that drew it.**

  Every board terminal keeps the last 64KB of its raw attach traffic in both directions, with timestamps: what the
  daemon sent, every keystroke and resize the board sent, and each time xterm's own grid changed size. **save
  terminal trace** in the terminal's cog downloads it as JSON, along with the screen and cursor at that moment.
  Click it the moment a pane garbles. `node scripts/replay-term-trace.js <file>` replays it through the board's own
  xterm.js, and `--frames` lists every frame with its escapes spelled out. Nothing is encoded until the save, so
  an idle recorder costs a push per frame.
- **Typing through the hub stops stuttering when the machine is busy.**

  On Windows the hub and the room now run at above-normal priority. A keystroke crosses both of them, and on a
  machine full of agents and browsers Windows left a normal-priority process waiting 15 to 200ms in bursts before
  it ran again. That wait is what made echo spiky while typing fast, and it looked like the hub because the hub is
  the process in the middle. The link itself was never the delay: a keystroke through the hub and the TLS link
  takes half a millisecond, the same as going to the room directly. Measured side by side under the same load, keys
  slower than 20ms fell from 50 in 2500 to 3. The agents the room starts stay at normal priority.
  `ATRIUM_PRIORITY=normal` turns the raise off.

- **Clearing the notifications panel closes it, and an empty panel is empty.**

  **clear** now empties the list and shuts the panel in one click. Opening the panel with nothing in it shows the
  header and its buttons with no placeholder text under them.

- **The sort only reorders the groups the board makes. Yours keep the order you set.**

  The pinned bucket and every group you made under **by group** hold their hand-placed order on the board, the stack
  and the terminals strip, whatever the sort pill says. **move it up or down** on a card's menu now works in those
  lists under any sort, and on the stack and the strip as well as the board. It used to appear only when the board
  sort was manual. On the terminals strip you can drag a row within one of your groups to reorder it. Drag a row in
  from elsewhere and it joins that group too. A card in two of your groups still shows in both.

- **A custom group you just made shows up before it has any cards.**

  Adding a group under **by group** used to draw nothing until a card carried its tag, so there was no sign the add
  had worked. Every custom group is now drawn on the board, the stack and the terminals strip. An empty one says how
  to file a card into it: **into group** on the card's menu.

- **One checkbox times the whole keystroke path, with no restart.**

  **log terminal input lag** in settings now switches the hub and the room as well as the browser, from the moment
  it is ticked. In the all-rooms view the hub passes it to every attached room, and a room that attaches later is
  told when it arrives. The hub and the room keep the setting across a restart. `ATRIUM_DEBUG_INPUTLAG` still works
  and wins over the checkbox for the life of the process, and settings says so when it is set. See
  `docs/input-lag-logging.md`.

- **The notifications panel copies with an icon, not a button on every row.**

  A small copy icon shows on the row under the pointer, or on the row holding keyboard focus. Clicking it copies
  that row's title and body. It turns teal when the copy works and red when it does not. On a touch screen it stays
  visible, since there is no hover to show it.
- **`restart_atrium` brings the room back as the same room, and says what it did.**

  The detached restarter a room spawns was passing only its ports and database. It dropped `--dir`, so it read the
  default key directory, which on a machine joined more than once holds an old room's name and hub. It came up as
  that room, or not at all, and never reattached. It now passes the whole launch: `--dir`, `--isolated` and
  `--accept-upgrades` too. Every step lands in `restart.log` beside the room's keys, and the restarted room keeps
  writing to the log file the old one wrote to. On Windows it also leaves any job object it can, so a job that
  kills its members on close cannot take it down.

  A clean stop no longer ends in `HALTED: sql: database is closed`. The address file was removed after the store
  closed, and finding its shared path read a setting from the closed store. It is now removed first, and a store
  call that arrives after `Close` gets `ErrClosed` rather than halting.

- **A pinned order dragged on the terminals page stays put on the hub board.**

  The hub's board names every card `room~id`, and the strip posted those names as the new order. The hub strips
  the room from a request's path but not from its body, so the room looked up ids it has no row for, wrote no rank,
  and the next refresh drew the old order again. The strip now sends bare ids, and the room strips a stray room tag
  from a pin order itself, the same safety net a launch onto a card already has.

- **Terminal input lag can be measured instead of described.**

  Off by default. In the browser, **log terminal input lag** in settings (or `atrium.debug.inputlag` set to `1` in
  localStorage) times each keystroke from the key to its echo on screen, splits it into send, wire, parse and
  paint, and prints it to the console. It also warns when the page's own JavaScript blocks the main thread, and
  notes how full the fetch queue was, so a refresh storm shows up beside the slow key it caused. On the hub and the
  room, `ATRIUM_DEBUG_INPUTLAG=1` logs any hop over 20ms: the hub's round trip to the room, the room's frame to pty
  write (with the input lock split out), pty read to fan-out, and frame to first output. See
  `docs/input-lag-logging.md` for how to read the lines.

- **The board header holds its shape from a phone to a wide monitor.**

  Between 900 and 1150 pixels the header used to run off the right edge and hand you a sideways scrollbar. Now the
  navigation shrinks and scrolls inside its own track, so the header stays put and only the controls that cannot
  fit move. At touch widths the header tap targets reach 40 pixels, a thumb rather than a mouse point. And the
  board-wide auto toggle keeps its word: it reads ASK or AUTO even when the pill is at its narrowest, instead of
  collapsing to an empty capsule you had to guess at.

  A headless browser test pins all of this down so it cannot regress unseen. Run steps are in the header of
  `scripts/test-board-headless.js`.

- **Atrium is told where your repositories live, instead of walking the disk guessing.**

  A provider is a name you choose, a root folder, and the layout under it. Defining one adopts every checkout
  already there, so `D:/git/github` becomes forty repositories without any of them being added by hand. After
  that, picking one is an org and a repo rather than a path you type.

  It replaces a `projects` button that scanned for anything holding a `.git` two levels down and shelled out to
  a command template to make a worktree. That inferred a layout from whatever directories happened to exist, had
  no idea what an org was, and ran a command atrium could not see inside. The scan, the `worktree_command`
  setting and the `project_scan_depth` setting are gone.

  **The row is durable and whether it is on disk is worked out fresh.** That split is the whole design. Storing
  presence means one run against an unplugged drive marks everything absent and a tidy-up deletes it. Deriving
  the rows means the list is empty and the org and repo you typed are gone. Neither happens: the list is every
  repository you ever adopted, greyed where the directory is missing, and plugging the drive back in restores
  the picture with no action. Discovery only ever adds, so nothing you do to a disk can erase what you declared.

  **Worktree support is a toggle with a folder, and it cannot be turned off while that folder holds anything.**
  Turning it off would leave checkouts on disk with nothing describing them. The refusal names every entry
  rather than saying "not empty", because otherwise you hunt, and a `check` button asks the same question
  without saving. It counts directory entries rather than asking `git worktree list`, which sounds weaker and is
  stronger: git cannot see a worktree whose repository was deleted, and that is exactly the one that must not be
  silently abandoned.

  **Making one runs `git worktree add` as an argv, with no shell.** The template it replaces had four defects
  and every one of them was the shell: its default also started a session, so make then start started two; it
  had to be quoted correctly under three grammars, which is why a branch name had to pass a regular expression
  first; the resulting path had to be read back out of the output; and a repository name needed quoting. A
  branch that does not exist yet is created off the repository's own HEAD.

  Nothing about any forge is in the binary. A provider is a root and a layout: no URL parsing, no cloning, no
  network call, no credential. And no card was touched, migrated or rewritten, including by deleting a
  provider: a card's directory is a string you typed, and it outlives anything that describes it.

  `docs/providers-design.md`, `docs/test-plan-z-providers.md`, and a recorded walkthrough at
  `scripts/walkthrough/providers.spec.js`.

- **Pinned cards hold their hand-set order again, whatever the board sort says.**

  The board sort pill sorts each column by activity or name. That sort was reaching into the pinned bucket, so a
  pinned card you had placed by hand snapped back to wherever activity order put it. Pins are now ordered by rank
  always, the same field the card menu's move up and move down write and the only order a person sets by hand, so
  the sort pill governs the rest of the column and leaves the pinned bucket where you left it. Move up and move
  down still work on a pinned card.

- **The board and the room link are separate, independently-bound surfaces.**

  The hub serves two things: the board you open, and the socket rooms dial in to. They are now cleanly split. The
  board (`--addr`) is loopback-only and refuses a non-loopback bind, because it has no login. The room link
  (`--link`) may bind wide (`0.0.0.0` or a chosen interface) and carries the transport menu (direct/mTLS, zrok,
  OpenZiti) - safe on a wide bind because the direct transport is mTLS with a pinned CA and hub-signed room certs,
  which the board lacks. `--link-advertise` sets the address minted into join tokens and is required when the link
  binds wide, so a token never carries an unreachable loopback address. This lets a LAN room dial the hub over
  mTLS directly, with no overlay.

- **The db event window can be bounded, so the primary database stops growing (opt-in).**

  The db event sink can keep only a recent per-card window instead of every event forever. Off by default: with no
  window set, the db is unbounded exactly as before. When the window is set, the oldest events roll off (already
  durable in a cold sink if one is configured), and a card's event feed reports `rolled_off` so the board can show
  that history rolled off rather than pretend it is complete. Together with db-shrink handing freed pages back to
  disk, the operational database now plateaus instead of climbing.

- **A restart item in the terminal cog menu.**

  The terminal settings menu now has a restart that exits the session and resumes it on the same card, so an
  operator can pick up new defaults or clear an update nag without losing the conversation. It calls the phase-2
  `POST /v1/tasks/{id}/restart` behind a confirm, since the session drops for a few seconds before it reattaches.

- **A throwaway or second room on one machine can keep off the machine's hooks: `--isolated`.**

  A room writes its address to a FIXED shared file so hooks, the CLI and the control MCP find it without knowing its
  dir. That is right for the one-room-per-machine case and wrong for two: a second room started with only port, dir
  and database overrides still writes that same file, overwrites it, and every hook aimed at the first room starts
  arriving at the second. `atrium2 room` and `atrium2 join` now take `--isolated`, which keeps this room's address in
  a private file beside its `--dir` and never publishes the shared one, so the first room's hooks are left alone. The
  takeover warning now also names the flag. Two rooms on two different machines never needed this: each machine has
  its own file.

- **The restart and launch path is concurrency-safe.**

  Two restarts or launches racing on one card could both pass the "is a runner live" check before either spawned,
  and both resume the same conversation id, braiding one transcript out of two. Launch and restart now serialize per
  card id and per resume id through a keyed mutex held across the whole check-then-spawn window, a duplicate
  `restart_atrium` ask is dropped while one is in flight, wind-down waits for the kill to take before it returns,
  and the store is closed explicitly on the shutdown path. Covered by a concurrency test that spawns racing
  restarts and asserts one runner survives.

- **Databases give freed space back to disk.**

  New room and hub databases open in SQLite incremental auto-vacuum mode, and a timer reclaims freed pages while the
  daemon runs, so a store that pruned cards or rolled off history shrinks on its own instead of sitting at its
  high-water mark. Existing databases are left as they are, because the mode cannot be switched without a full
  rebuild, and the reclaim is a no-op when there is nothing to free. This is the complement to the event sink: the
  sink keeps the database small going forward, this hands the space already freed back to the disk.

- **A terminal theme through the launch tool, and a pluggable event sink (phase 1).**

  `atrium_launch` now takes a `theme`, so a session started through the hub control MCP comes up in the palette the
  operator meant rather than the board default. And event storage sits behind an `EventSink` interface: the SQLite
  `event` table is the `db` implementation, a rolling-JSONL `file` cold sink can run alongside it, and an
  `event_sink` setting names which sinks are active, defaulting to `db` so nothing changes unless opted in. This is
  the seam that lets the high-volume audit trail leave the primary database later, without a schema break.

- **Control MCP phase 2, fixtures on/off from the board, and the board over a zrok share.**

  Phase 2 finishes the hub-side control MCP. `restart_atrium` now forwards an instruction down the link and the
  room restarts itself: it parks its other agents, spawns a detached restarter that outlives it, winds down, and
  comes back via `atrium2 room`. One session can be restarted onto its own card with `--resume`, so a runner picks
  up new defaults or clears an update nag without losing its conversation. Launch writes `BRIEF.md` on the room
  again, so a hub-driven launch can hand a new session a briefing file. The room exports `ATRIUM_ROOM` to the
  sessions it starts, so the per-session `X-Atrium-Room` header the http MCP registration carries actually
  resolves. Separately, a fixture can be toggled on or off from its board pill without being deleted, and the hub
  can optionally serve its board over a zrok share for remote access, with a failed share isolated so it never
  takes the local board down.

- **Control MCP moved off per-session stdio children onto one HTTP server on the hub (phase 1).**

  Every claude session spawned its own `atrium-control.exe` stdio child, one process per session at about 24MB,
  a dozen live at once. A single HTTP MCP server now runs on the hub at `/_hub/mcp`, so a session opens a
  connection from inside its own claude process instead of spawning anything. It lives on the hub for the same
  reason `internal/cli/control.go` exists: the thing that restarts a daemon has to outlive it, and the hub
  already outlives rooms. Identity arrives per request in headers rather than per process in env: each session
  sends `X-Atrium-Agent` and `X-Atrium-Room`, which Claude Code expands per session at connect, and the server
  runs stateless. Loopback only, guarded on the caller's own RemoteAddr, because the overlay is not an auth
  layer. Phase 1 ships `atrium_status` and the peer tools (peers, say, task, exit, and launch without its brief
  file); `restart_atrium` and launch's brief return "not yet wired" until phase 2 adds the room-side handler.
  Turning it on is a manual flip of the `atrium-control` entry in `.atrium/mcp.json` from the stdio child to the
  http URL.

- **Board terminal bar and path chip tidied, shipped by hub-only restart.**

  Two board-CSS fixes, each built from a `claude/*` worktree and deployed by rebuilding `atrium2`, swapping the
  binary, and restarting the hub process alone while the room kept running. The terminal bar's folder and cog
  icons now match the worded buttons: a global `button.icon` rule in `dialogs.css` loaded after `terminal.css`
  and drew them as a 23px transparent glyph, so `.term-bar button.icon` now pins the size and restores the chip
  box and hover. The path chip's copy button was floating in full chip side-padding, so `#t-chips .chip.path`
  drops the gap to 3px and tightens the padding, pulling the glyph next to the path.

- **The hub snapshots its own store, and a restore is one command.**

  The store halts on a corrupt database and refuses to start on one. That is the right posture and it is only
  tolerable if there is something to go back to. Without this, "it halts" meant "it is gone".

  A running hub writes a snapshot every ten minutes, using SQLite's own `VACUUM INTO` rather than copying the
  file: everything committed since the last checkpoint lives in the write-ahead log beside it, so an operating
  system copy of `hub.db` alone is a database missing exactly what happened most recently.

  **Kept in tiers, not by count.** Everything from the last hour, one an hour for a day, one a day for a week,
  one a week for a month: about thirty files covering a month, and the oldest as easy to find as the newest.
  Fifty of something written every ten minutes is eight hours of history, all of it from today, and the two
  questions people ask are "put it back to twenty minutes ago" and "what did this look like last week".

  `atrium2 hub backups` lists them. `atrium2 hub restore <path>` puts one back, with the hub stopped, and
  **moves what was there aside rather than deleting it**, printing where it went. Restoring is done under
  pressure from a list of timestamps and the wrong one is one keypress away, so undoing a restore is another
  restore. The snapshot is opened and checked before anything moves, because restoring a damaged file over a
  working one turns a bad afternoon into a lost hub.

  Neither is a pane on the board. A restore is what somebody reaches for when the board will not come up.

- **Deleting a room is four steps, and the room takes one of them.**

  Marking a room for deletion now does the thing it promised: **that room starts no new work.** Launching,
  raising a card, posting intake or queueing a dispatch onto it is refused with the reason. Everything already
  running carries on and can still be renamed, answered, shelved and finished, because work in flight is meant
  to be worked out normally. Marking is still one click to undo and still destroys nothing.

  **The room confirms it is finished, and the confirmation is the room saying it holds nothing.** The hub cannot
  see whether a directory was cleaned up, a throwaway deleted or a session really ended, so it does not decide:
  it waits to be told, in the one way a room speaks about itself. That is withdrawn the moment the room has work
  again, so a confirmation cannot go stale into a removal.

  So the ordinary path is: mark it, clear its cards, let it say so, stop it, remove it. A connected room is
  never deleted, and one that never said it was finished is refused by name with what to do about it.

  `--force` is still there for a machine that is never coming back, and still says what it does not do: it
  removes the hub's record and nothing else.

  `atrium2 hub room log` prints the whole of that, for one room or all of them, and answers for a room that has
  already been removed.

- **A machine that is not answering still shows its work, and nothing on it can be touched.**

  A shut laptop used to drop off the board entirely, which reads as the work having gone. Its cards are drawn
  now, from what that room last said, in one group at the bottom of every column and of the stack, shut unless
  somebody opens it. What is running is what the board is for and is never pushed down the page by what is not,
  and cards nobody can act on are one line saying the work still exists rather than a screen of things that do
  not respond to being clicked.

  One group for all of them, not one per room: it is the same kind of thing, and four headings for four dead
  laptops is four times the furniture for one fact.

  **Nothing opens.** A no-entry mark where the attach button would be, saying `room <name> is offline. cannot
  restore terminal`. Clicking the card says the same in a sentence. The attach, resume and start chips are not
  drawn, the terminals list does not hold those rows, and the hub refuses every request for that room by name:
  "the room athens is not answering, so nothing on it can be opened or changed."

  **Nothing is queued.** Not "we will apply this when the machine returns". A queue of intentions against a
  machine nobody has heard from is a second source of truth, and reconciling it is the part that goes wrong.

  **A room that has never connected still appears nowhere but the rooms tab.** It cannot have cards, so there is
  nothing to draw, and it is inventory rather than work.

  One room attached no longer means the hub skips merging. It used to, which was right when one room attached
  was the same as one room existing, and it would now drop every shut machine's work off the board.

- **A room tells its hub what it is holding, so a hub whose room is offline shows what was there.**

  Rooms push, the hub writes it down. Not polled: the room is the only thing that knows something changed, and
  a timer is either late or wasteful and is usually both. The room watches its own event stream and sends when
  something happens, with a ceiling of a couple of seconds so a busy machine cannot thrash the hub's database,
  and an announcement identical to the last one is dropped rather than sent. That last part is what keeps a
  noisy room from being a noisy database: activity, output and telemetry all publish events and none of them
  are cached, so most of what wakes the announcer up has nothing to say.

  **What is cached is exactly what the room persists.** There is a new endpoint, `GET /v1/state`, that answers
  with the stored rows rather than the view `/v1/tasks` returns. The view carries what is true only this
  second: whether a card is supervised, what tool it is running, its telemetry, how long it has been idle. The
  obvious answer was to send the view and strip those, and the obvious answer is a field list that goes stale
  the day somebody adds a live field. Sending the stored row cannot drift, because there is nothing to keep in
  step.

  **The announcement is taken whole.** Anything the hub was holding that is not in it is discarded, because it
  is no longer there. No merging and no row-by-row reconciliation. A discard is written to the audit log, which
  is what turns "I am sure there was a card there" from an argument into a lookup. An announcement that
  discarded nothing is not logged: a line every couple of seconds saying nothing was lost is a log nobody can
  read.

  **The cache is read only when the room is not answering.** While a room is connected it is asked, every time,
  and the cache is written and never read. There is no third state where some of what you are reading is
  current and some is remembered and nothing says which.

  `atrium2 hub room log` prints that audit, for one room or all of them, and it outlives the rooms it is about.

- **A room's settings are behind that room's cog, not in a list called "this machine".**

  `settings -> this machine` had grown into nine unrelated things: the editor command, where pasted files land,
  what is typed in front of a pasted path, which directories the picker may open, the worktree command, how
  deep to look for repositories, the shared address file, the shell command and the scrollback. The name meant
  nothing. On a board serving four machines it meant less than nothing, and the pane asked which machine you
  meant the first time you touched any box in it.

  A ROOM IS THE UNIT, NOT A MACHINE. One machine can hold more than one room and a hub can be a room itself, so
  each of those is a fact about one room. They now open from the cog on that room's row, already scoped: the
  room is in the title of the dialog, and nothing asks again.

  **`run agents here too` moved there too**, onto the hub's own room, which now appears in the list whether or
  not it is running. That was the one control that could not live behind a cog until the row it belongs to
  existed before the switch was thrown.

  **A board with no hub draws one row, `this machine`, with the same cog.** The daemon is the hub with its own
  room, and without that row the settings would have been in the page and unreachable.

  **An offline room's pane shows what the hub knows and no fields.** The only authoritative answer about a room
  comes from that room. An empty box that saves nowhere is worse than no box.

- **The rooms tab shows every room, not only the ones that answered.**

  The tab listed what was attached, which meant a machine somebody shut disappeared from the one screen whose
  job is to say what exists. A room added and not yet joined had nowhere to appear at all.

  It now draws the hub's own record, in three groups and in this order:

  - **here now**, because what is running is what the board is for and is never pushed down the page by what
    is not
  - **not answering**, which says what was last heard and that it cannot be acted on
  - **never connected**, which cannot have cards and so appears here and nowhere else on the board

  Every row carries a transport badge, a cog, and what the machine calls itself beside what the hub calls it.
  The badge is a badge and never a column: transport is worth seeing at a glance and is not a concept in this
  UI.

  **The header counter now reports the other half.** `1/2 rooms` is the one place a missing room is counted,
  because every other number on the board is live only on purpose. Three agents waiting for permission on a
  laptop that is shut is not three things to do. A room that has never connected is not counted either: nothing
  is missing because of it.

  The cog holds what the hub knows about that room and one action, marking it for deletion, which destroys
  nothing and is one click to undo. **The board still cannot mint a join string**, and that line has not moved:
  atrium has no login, the board may be served over an overlay, and a page that could mint one would let
  anybody who opens it enrol a machine that runs agents. Adding a room, replacing its join string and removing
  one are done at a terminal on the hub, where being there is the credential.

  `/_hub/rooms` still answers what is attached, because the picker, the grouping and every counter mean that
  one. The durable list is `/_hub/inventory`, and the two are separate because they are different questions.

- **The hub knows which rooms exist, and it names them.**

  A hub held nothing at all, which made its restart free and left it unable to answer the one question only it
  can: have I already made that room. A join string authorised a join rather than a join AS ANYTHING IN
  PARTICULAR, so a room named itself at enrolment and the hub signed whatever it asked for.

  The hub now has a store of its own, `internal/hubstore`. It still holds no WORK: no sessions, no terminals,
  no agent processes, and no authority over any of them, so stopping it still costs nobody a session. What it
  holds is its own truth, which nothing else can answer. Which rooms exist, what they are called, how they may
  connect, their join secrets, and which are on their way out.

  **The hub names the room, and the name is minted into the join string.** You add a room on the hub, and the
  secret that authorises it is bound to that one name. Spending it is the proof and the answer in one step, so
  there is nothing left for a room to claim and no hand-written check refusing a name already taken. The room
  reads its own name back out of the certificate the hub signed rather than out of the string it was handed:
  editing the string changes nothing, because the string is not what the hub reads afterwards. `--name` on the
  room is gone, and so is the fallback that let a signing request supply a name when the hub had none.

  A room the hub has no record of cannot attach, even holding a certificate this hub signed. That is checked on
  every heartbeat rather than only at attach, because the store is a file and forcing a room out is another
  process writing to it.

  New commands, all of which work whether or not a hub is running:

  - `atrium2 hub room add <name>` writes the room down and prints its join string, once
  - `atrium2 hub room ls` lists every room, connected or not, with what each calls itself beside what it is
    called
  - `atrium2 hub room token <name>` mints a fresh string and retires the old one, because the hub holds a hash
    and cannot show what it printed before
  - `atrium2 hub room mark <name>` puts a room on its way out, reversibly
  - `atrium2 hub room rm <name>` refuses a room that is not marked, one the hub last saw holding cards, and any
    room heard from in the last twenty seconds. `--force` is for a machine that is never coming back and says
    what it does not do.

  `atrium2 hub token` is gone with the anonymous join it printed. An empty hub now says how to give itself a
  room instead of printing a string that would have enrolled anything under any name.

  The cache and the audit log are in the schema and nothing writes to them yet. Rooms over zrok and OpenZiti
  still name themselves, which is what they always did and is not what the direct path now does: the hub says
  so at startup rather than implying a guarantee it is not keeping. See `docs/decisions.md` 18.

  The store halts rather than degrading, the same posture `internal/store` already has, and refuses to start on
  a database it cannot read. The room listener closes and stays closed, so rooms park on the backoff they
  already have, and the board stays up to say what broke.

- **One alert per event, in one form, wherever you are looking.**

  A chime with no notification behind it, every time a session was popped out into its own window. The beep is
  unconditional and the form of the alert was not, so the half that got decided wrongly was the half you were
  meant to see.

  Each document decided alone, out of what it could see. The board asked `inForeground`, visible and focused. A
  popped-out window asked `onScreen`, visible, chosen because the stricter test had it announcing out loud a
  thing being watched on a second monitor. Neither window can see the other, and on Windows a window buried
  behind others is still visible. So a board sitting behind a popped-out window believed itself unattended and
  rang the operating system while toasting where nobody was looking, and the window actually being read drew a
  toast underneath four other windows and suppressed the notification that would have said so.

  **The question is about the set of windows, and no single document can answer it.** So they tell each other.
  Each window announces focus on every transition and on a beat while it holds it, and a claim nobody has
  repeated is not believed, for the same reason a solo claim is a heartbeat: a window that dies while focused
  would otherwise suppress every desktop notification for as long as the board stayed open.

  The rule is now one rule, applied once:

  - a window has focus, and that window toasts. Nothing else happens anywhere.
  - no window has focus, and Windows says it once. No window toasts.

  Never both. Every caller hands the whole alert to `notify` and nothing else, because the choice between a
  toast and a notification is one decision and it cannot be made twice. Callers used to make it themselves and
  then call `toast` as well, which is how the same event reached you in two forms.

- **The login moved into the panel that publishes the board, and the panel stopped claiming there is none.**

  The zrok panel ended its description with "this board has no login". That is a constant. It was written when
  it was true of every atrium, and it stays on screen unchanged after somebody sets a login, so the panel tells
  an operator no login exists while the published board is asking them for a password. Somebody hitting that
  prompt goes looking for a zrok credential, because the only other thing on the panel is a share token.

  The sentence is now read off the configuration. It names the login: the user, the provider it sends people
  to, or that there is none and where to set one.

  **A public share is the only exposure where a missing login is a hole.** A private zrok share needs zrok on
  the other end and a ziti service needs a policy on that network, so for both of those a login is something
  somebody may want rather than something absent. The sentence says which of the two it is, because "this board
  has no login" reads as a warning and is only one of those things. OpenZiti says it too, so both panels answer
  the same question.

  **`who may open it` is no longer its own pane.** It only ever applies to the published board, so it sits
  under the thing that publishes it rather than beside it in the nav, where it read as an unrelated setting and
  contradicted the panel above it. The settings nav is six entries rather than seven, and `.s-sub` is the
  heading for a group that belongs to the pane it is in.

- **Atrium could answer a permission dialog by accident, and now refuses to type into one.**

  `runner.Say` writes a message and then writes Enter, because a message typed into a session is meant to be
  submitted. If a dialog is on that screen when the Enter lands it answers the dialog, with whatever option was
  highlighted, and nothing reports it: you see a message you sent, and separately a tool call approved by
  nobody.

  It had not bitten because of luck rather than design. While atrium's own gate holds a request the runner is
  blocked inside a hook and draws nothing, so there is no dialog to hit. Every dialog atrium did not raise was
  exposed: a session with the gate off, a trust prompt, a plan approval, anything a future release adds.

  **The signal was already arriving and being thrown away.** The Notification hook fires on
  `permission_prompt`, and `wantsAHuman` filtered it out on the grounds that atrium's own gate put the prompt
  on screen so reporting it back told the card what it knew. That is true of a prompt atrium raised and false
  of every other one, and only the daemon can tell them apart, because only it knows whether a request of its
  own is pending. So the CLI sends the kind and the daemon decides.

  The flag is in memory beside the activity and the next thing the session does clears it, since there is no
  hook for a prompt being dismissed. A store error counts as pending: a message queued that could have been
  typed costs a delay, and typing into a dialog costs an answer nobody gave.

  **All four typing paths are guarded** and each falls through to the queue rather than failing: messages, a
  card's note, an action's prompt, and the peer bus, which gains a fourth state refusing the way a part written
  line already does.

- **The board has a sort, and it is on screen.** It always had one and it was `rank`, the order the card
  menu's up and down writes. That is a real answer for a column you arrange by hand and no answer at all for
  the rest: a card's rank is set while it is live and nothing touches it when the work ends, so the finished
  column came out ten days, nine, eight, one, forty seconds, ten days again.

  A `sort` segment now sits beside `group` in the board's own bar: activity, name, manual. Activity is the
  default. It applies INSIDE the grouping, so it orders the cards within every project or tag without touching
  which groups exist or what order they come in. Group headings stay alphabetical, which is the rule the
  terminal strip already follows and for the same reason: a heading that moved with its contents would make
  the list rearrange itself while you read it.

  `manual` is rank, and it exists because rank still does. The card menu's "move it up or down" now hides
  itself unless manual is selected, since otherwise it writes a field the next paint throws away, which is a
  control that visibly does nothing.

- **Atrium works out which repository a card belongs to, and lets you correct it.** A session launched without
  `--repo` had nothing for the terminal strip to anchor on, so the label fell back to the last three segments
  of the worktree path. For a worktree kept below its checkout that is three directories that mean nothing,
  drawn as three nested headings holding one row:

  ```
  D:/worktrees/github/openziti/desktop-edge-win/more-debug-skill-updates/doc/troubleshooting/debug-skill
  -> doc > troubleshooting > debug-skill
  ```

  `DisplayRepo` resolves in three tiers: an override, then whatever the launcher recorded, then a guess read
  off the path by looking for `<forge>/<org>/<repo>`. **The guess is computed on the way out and never
  stored**, so a launcher that starts sending the real answer wins immediately, and atrium still is not
  learning git: it reads a directory name out of a string it already has, the same shape the board's default
  grouping rule has always keyed on. A path it does not recognise answers empty, which leaves every caller
  where it was.

  It ships as `display_repo` beside `display_title` rather than replacing `repo`, so a client can still ask
  whether atrium was TOLD the repo. And a `which repo…` entry sits beside `rename…` in the terminal row menu,
  because they are the same act: both write an override that survives a reconnect. Rename decides what the row
  says, this decides where it sits. Empty clears it and the guess applies again.

- **Selecting text no longer opens what you were selecting.** Dragging across a filename to copy it ends in a
  click on whatever was under the pointer, and both file surfaces answered that by opening the thing.

  Two different tests, because they are two different selections. The file rows use the board's existing
  `isSelecting`, checked at click time, which is the only moment it answers correctly: a plain click collapses
  the selection on mousedown and a drag keeps it through mouseup, so an unrelated selection elsewhere on the
  page does not block a click here. Terminal path links use `term.hasSelection()`, because xterm draws on a
  canvas and keeps its own selection that the document knows nothing about.

- **A card with subagents running showed no badge, because the badge was gated on the tally alone.**

  The tally and the named list are allowed to disagree, and `subagent_test.go` pins that on purpose: an unknown
  stop takes the count down and finds nothing to remove, on the grounds that a stop is a fact even when the
  start that would have named it was lost. So `"subagents": 0` served beside a `running` array with a live
  entry in it is a state the daemon may legitimately produce. It was observed in the wild, on a session with
  two agents working.

  The board drew the badge off `a.subagents > 0` and therefore drew nothing. It now takes
  `max(count, named.length)`, which is the only reading that cannot hide an agent atrium can actually name.

  Clamping the count in the daemon was tried first and reverted: it makes the number claim an agent has not
  finished when the runner said it had, and it broke two tests that exist to say so. The disagreement is the
  design. Reading only half of it was the bug.

- **The history tab was implemented twice, under one name, and neither copy worked.** It looked unbuilt and
  was not.

  `index.html` had two elements with `id="history-list"`, and two functions called `renderHistory` to go with
  them: the card history in the history view, and the permission decision log in the permissions pane.
  `runners.js` loads after `stack.js`, so it won the name, and `getElementById` returns the FIRST match, which
  is the permissions one. So opening the history tab drew ninety one cards into a hidden block belonging to
  another view, and the decision log drew nothing at all. Two working features, each invisible, each looking
  like nobody had written it.

  The permissions side is now `renderPermHistory` into `#perm-history-list`. Nothing about either feature
  changed.

  **`scripts/check-board.sh` now fails on a duplicate id**, because none of the existing checks could see this
  one: the markup is valid, every script parses, and the only symptom is a view painting into another view's
  element. It reports every offender rather than the first.

- **The restart quiesce is bounded by the SET of cards coming back, not by a duration.** The first version was
  a floor, a tail and a cap. It reported `settling: true` correctly and the toasts arrived anyway, because ten
  sessions coming back is not an interval anybody can name in advance: `--resume` is slow, the gap between
  launches is deliberate, and on a cold machine the whole parade runs minutes.

  Atrium already knows exactly which cards it is about to restore, because it read the list in order to
  restore them. So it names the whole list before starting the first one and stays settling until every one has
  arrived or failed. Failing counts, or a deleted worktree would keep the board quiet until the backstop. The
  clock is left in as a backstop and as an opening grace for daemons with nothing to restore, whose sessions
  rejoin through their own hooks.

  One entry stands for the startup sequence itself, held for its whole duration. Without it the window closes
  in the gap between `startFixtures` emptying its list and `reopenSaved` naming its own, and that instant is
  exactly where the arrivals landed.

  **`waiting` is suppressed during the window too, not just `arrived`.** What came through the first fix was
  "X is ready" for a session that had finished its turn BEFORE the restart: the card was marked dead when its
  pid went, came back on the reopen, and reported the state it was already in. `justStarted` does not catch it,
  because the card is old. Permission requests still ring throughout.

- **A group you collapse stays collapsed.** Two things were wrong and the second one was hiding behind the
  first.

  A project group's folded state was keyed by name WITHIN A COLUMN, so `dovholuknf/atrium` in `running` and
  `dovholuknf/atrium` in `needs permission` were different groups that happened to be called the same thing.
  A card blocking on a tool call moves between those columns, so shutting a group and then watching one of its
  cards block drew an open group with the same name one column over. From the front it reads as the board
  deciding to pop the group open because something inside it updated, which is exactly what it was reported
  as. Project groups now fold board-wide by name.

  The state was also written down by an `onclick` on the summary, which records the state being LEFT rather
  than the one arrived at, and only for one way of getting there. It is now one capturing `toggle` listener
  for every group on the board and the stack, which is the event the browser fires whenever a `<details>`
  actually changes, by any means. Note the early return in it: a repaint re-asserts the `open` attribute,
  setting an attribute fires `toggle`, and a refresh on a toggle that changed nothing is a repaint loop.

  Existing folds are forgotten once, because the key changed shape.

  `board.js` also carried a literal NUL byte where a space belonged, in `": pinned"`. It worked, because the
  same wrong byte was on both sides of the comparison, and it is gone.

- **A restart no longer announces itself six times.** The cards that were running die with the old daemon, the
  fixtures come back a moment later, and to the board's arrival alert every one of those is a card that was
  not there a second ago. So restarting six terminals rang six times to say that the thing you had just
  restarted had restarted.

  The board cannot work this out on its own: a session coming back and a session somebody started arrive the
  same way. So the daemon says it is still coming up, on `/v1/health`, which every page already polls and
  already reads for the build id, and while that is true the board re-seeds what it knows instead of diffing
  against it. Bounded, so a fixture that takes two minutes does not buy two minutes of silence.

  **Permission requests still ring throughout.** An agent that comes back up already blocked is the one thing
  during a restart worth being interrupted for.

- **Atrium checks for a newer runner itself, just before it starts one, and never by running the runner.**
  `scripts/sources/runner-updates.ps1` is deleted. It was a source on a ten minute timer and three things were
  wrong with it.

  It ran `claude --version` and `codex --version` to find out what was installed, which is a whole agent
  binary starting up to print one line, and on Windows each one allocated a console window in front of
  whatever you were doing: `hideWindow` covers the command atrium spawns and not its grandchildren. The
  installed version is now read out of the package's own `package.json`, and the published one comes from a
  single request to the registry. No process is started.

  It asked every ten minutes forever, including the twenty three hours a day nobody was about to start a
  runner. The answer only changes anything at one moment, because updating replaces the binary and therefore
  has to happen while the runner is not running. So that is when it is asked.

  And its deduplication key was the package AND the version, so each release raised a new card and the one
  nobody had actioned stayed in the inbox beside it. Two releases meant two rows saying the same sentence.

  **A launch somebody pressed waits for the answer. A launch that happened on its own does not.** There is
  somebody in front of the first to read it and nobody in front of a fixture at boot. Only the first caller
  goes and asks: a wave of launches does not become a wave of requests, and the ones that lose do not queue
  behind the one that won. The wait is bounded at four seconds and says so on the board while it is happening,
  because this is the only place atrium holds up something you pressed on a request that leaves the machine.

  Which package a runner comes from is a field on the harness row, seeded for claude and codex and editable
  like everything else. Atrium still knows nothing about any runner: it knows a row named something to ask
  about.

- **An intake item that has moved on rewrites the card it already has.** `store.Offer` used to answer "this is
  known, here is the card" and leave the card exactly as it was, which is why a key had to carry the state of
  the work to say anything new. It now refreshes the title, why, prompt, url and tags of a card **that is
  still in the inbox**. Anything past `backlog` has a session and a history and is left alone, and a field the
  source did not send leaves what was there rather than blanking it, so a prompt you edited before pressing
  start survives the next tick.

- **The redraw collapse was deleting lines out of the middle of paragraphs, ahead of every renderer.** This is
  what was actually wrong with scrollback, and it was not the rendering.

  `collapseRedraws` keeps the last frame of a repeated in-place update, which is what stops the flattener
  turning a spinner that redrew four hundred times into four hundred lines. It has no grid, so it decides what
  a frame is from carriage returns and erases. And a carriage return is not only a redraw: claude-code ends
  every wrapped VISUAL ROW with one, so a paragraph three rows wide is a single newline-free segment with
  three boundaries in it. It read those as three frames and kept the last.

  It ran inside `Replay`, on everything, before any renderer saw a byte. Measured on one card's ring: twelve
  findings went in, and what came out had none of their `#:`, `Sev:` and `Location:` lines and eight of their
  twelve `Issue:` lines. Every renderer downstream then faithfully rendered the wreckage, which is why fixing
  renderers kept not fixing it.

  **It now runs only where the flattener runs.** A grid resolves a repaint by definition, so the screen model
  and the raw path get the bytes the runner actually wrote. Same ring, same renderer, all twelve findings
  intact. The test that pins this asserts both halves: the paragraph survives the screen replay, and
  `collapseRedraws` still eats it, so moving it back in front of everything fails in the suite rather than in
  somebody's terminal a week later.

- **`atrium replay` renders a captured terminal stream the way an attach would.** Every change to how
  scrollback renders is in Go, so seeing one meant a build, a wind-down, a restart with every session on the
  board interrupted, and then reading the result by eye out of a pane. That loop is slow enough that this
  rendering was twice declared fixed on the strength of tests written beside it and twice reverted.

  It reads a file or stdin and writes stdout or a file. `--all` writes one file per mode, since the question is
  usually which of the three is least wrong on this particular stream. `--cols` and `--rows` are the size the
  bytes were COMPOSED at, which is not optional in any meaningful sense: a terminal user interface writes hard
  line breaks and absolute cursor moves for a specific grid.

  Three ways to get a stream, and the first is the one that settles arguments:

  - **`ATRIUM_TAP_DIR`** writes every byte out of every pty to `<dir>/<card-id>.tap`, before the ring, the
    collapse and any renderer. The only source that can tell "the renderer lost it" from "the runner never
    printed it". Off unless the variable is set, unbounded, never cleaned up. An instrument, not a feature.
  - **`GET /v1/tasks/{id}/scrollback/raw`** is a live card's ring, with the widths and the height in headers.
    `?collapse=0` is the uncollapsed one, and the difference matters: asking the collapsed copy whether the
    missing text was ever in the ring produced one confidently wrong answer before this parameter existed.
  - **`GET /v1/tasks/{id}/scrollback/text`** is the same history as plain text in a browser tab. The pane is a
    terminal and the terminal was under suspicion, so there had to be a way to read a card's scrollback that
    does not go through one. `?mode=` picks the rendering, `?ansi=1` keeps the colour.

- **Scrollback is replayed through a screen instead of being stripped of everything that moves.** Attaching to
  a card ran its history through `flatten`, which deletes every sequence that could overwrite something. That
  is the only way an append-only replay can be safe, and it means a cursor move has to be replaced with
  something: spaces. Measured on a real session, 362 of 864 lines came back padded with trailing whitespace,
  and every intermediate repaint was printed in sequence, so one tool call appeared three times, twice
  half-drawn. The same session captured from a native terminal had none of that.

  The bytes now go through a grid, and what comes out is what the terminal would have held plus everything
  that scrolled off the top of it. Two bugs had to be fixed first, and both were found by measuring against
  that native capture rather than by reading the code.

  **The attribute state was a string that sequences were appended to.** `\x1b[31m` then `\x1b[32m` does end up
  green, so appending looks right. claude-code changes colour thousands of times without ever resetting, so
  one cell ended up carrying `[32m[33m[90m[32m[90m[38;2;255;193;7m` and every run of text re-emitted the
  pile. It is an attribute state now, one value per attribute, rendered from a reset so a row is safe to move
  into history out of the order it was drawn in. 208KB of replay became 132KB.

  **The grid grew instead of scrolling.** The ring recorded columns and never rows, so the screen started at
  24 rows and stretched to whatever row got addressed. A terminal addressed past its last row scrolls, and
  what goes off the top is history that can never be written on again. A grid that grows keeps those rows
  addressable, so the next repaint lands on them and they are gone. That is where the expanded file listings
  went, the ones claude-code prints and then collapses to `+31 lines (ctrl+o to expand)`.

  Rows ride in the ring's width marks now, `ReplaySized` hands them over, and the grid will not grow past a
  recorded height. Against the native capture: `flatten` preserved 170 of 361 lines, the screen model
  preserved 151 before this and 164 after, with no padded lines instead of 219. Height is not a small effect
  and was being guessed: replayed at 120 rows the same capture scores 4.4%.

  **`replay_flat = on` puts the flattener back, without a rebuild.** This change was made once before on the
  strength of its tests, looked excellent by every number, and had to be reverted the moment somebody read the
  pane. Read per attach, so flipping it takes effect on the next attach.

- **A subagent finishing no longer reports the card as ready.** A subagent is a session of its own: same
  directory, same settings, same `ATRIUM_AGENT_NAME`, and the name is how every hook says which card it
  belongs to. So the end of a subagent arrived looking exactly like the end of the turn that spawned it, the
  card moved to `ready`, and the board rang to say an agent wanted you while the agent was still working. The
  turn hook reads `hook_event_name` and answers `keepGoing` to anything that names itself and does not say
  `Stop`. The count of running subagents is kept by its own pair of hooks and is untouched.

- **Everything the board has told you is kept.** A toast is gone in seconds, which is right for a toast and
  wrong as the only copy: a share address, a save that failed, a card that just asked for something. A bell in
  the header opens the last 200, newest first, with repeats folded into a count and a badge for what has
  arrived since it was last opened. Rows that name a card open it. Held in this browser, because what you were
  told is a fact about this screen rather than about the work.

- **Escape closes the file drawer, and once is enough.** Neither the drawer nor the editor inside it is a
  `<dialog>`, so neither got escape for free. One layer per press. Clicking a path in the terminal opens the
  drawer and then the editor on top of it, and closing the editor now closes a drawer that was opened that
  way, because two presses to undo one click is one press too many. A drawer opened from the button stays.

- **The terminal stopped flickering when the resize handle was hovered.** The pane holds a WebGL canvas and had
  no compositor layer of its own, so anything a sibling repainted made the compositor rebuild the canvas with
  it. Hovering a 6px handle was enough, and so was leaving it. Worth recording what did NOT fix it, since all
  three are the obvious answers: removing the hover transition, `contain: paint` on the pane, and promoting the
  handle. The grip is a SIBLING, so containing the pane isolates what is inside it and says nothing about a
  neighbour dirtying the layer they share. Promoting the pane fixed it.

- **The switcher stopped being offered an email alias.** Its input was the one filter field on the board still
  typed `text`; the other three are `search`, which Chromium's autofill skips. `autocomplete="off"` does not
  help and has not since 2015.

- **Starting a second agent in a directory is a right click and a runner, not a form.** The card menu's "start
  a session here" opened the launch dialog with the directory filled in, and the other nine fields waiting.
  Every one of them is optional, so the common case (put a codex in this folder beside the claude already
  there) was a form with one answer in it.

  It is now **new agent here**, and the flyout under it is the runners themselves, one flat list. Clicking one
  starts it in that card's directory and lands in the terminals tab. Nothing is asked, because nothing else
  has to be: the directory comes off the card, and a title, a reason, tags, a first instruction and a model
  are things a human writes when a human has something to say.

  **On both surfaces.** The board's card menu and the terminal strip's own menu carry the same entry, because
  it is the same question asked from the other side, and a second agent is most wanted while you are looking
  at the first one.

  **A shell is not on the list.** Every card already has `open a shell here`, which opens one on the card
  rather than making a second card to hold it.

  Runners whose command is not on this machine are left out rather than dimmed: the only fix is in the runners
  tab, and a row that can only fail is worse than no row. `fill in a form…` is the last entry and opens the
  dialog exactly as before, so nothing is lost.

- **Pinned terminals are a bucket you arrange, and they stay in it after the session exits.** Pinning meant
  "sort me first", and the strip's pinned rows then sorted among themselves by activity or by name. Both of
  those move on their own, so a set arranged on purpose rearranged itself overnight.

  **The pinned rows are now their own group at the top of the strip, in the order you dragged them into.**
  Drag a row in from below to pin it, drag within the group to reorder. The group folds like every other
  heading in the strip, keeping its count, and a folded bucket still takes a drop: the row goes on the end.

  The order is a column on the card (`pin_order`, migration `0051`), so it survives a restart and follows to
  another browser. Every existing
  pinned card starts at zero, which reads as a tie and falls through to the sort underneath, so a board nobody
  has dragged on looks exactly as it did.

  **A pinned row outlives its runner, drawn cold.** This reverses a decision recorded in `terminal-list.js`:
  that a row which cannot be switched to is a row that does nothing. That held while pinning only meant an
  order. It stops holding once pinning means "this is mine and I put it here", because a bucket that empties
  itself when you quit a session is not a bucket, and putting the row back by hand is the work pinning was
  supposed to save. A cold row holds its place and, clicked, offers to start the session again in the same
  directory onto the same card. Unpinned rows are unchanged: no runner, no row.

  The order is written as the whole list, in one transaction, through `POST /v1/tasks/pin-order`. A drag moves
  one row and changes the position of every row it passed, so the unit of work is the order rather than one
  card's place in it, and a half-applied reorder would leave the bucket in an arrangement nobody chose.

- **A session is called one thing, and if you named it, that is the thing.** The same card was `atrium` on the
  board, `main:atrium` above its own terminal and `atrium-backlog` in the strip beside it, which is three names
  for one session and no way to tell they were one.

  **The name somebody typed now wins over the name a machine read.** `overrides.title` is what the rename box
  writes and what the store has always served as `display_title`, but the terminal pane, the switcher, the
  collapsed strip and the window title all took the address composed out of the worktree first, so a card
  renamed to `atrium` was called `github/dovholuknf/atrium@main` everywhere except the board. The composing
  half is `observedLabel` now and `terminalLabel` is the typed name in front of it, which is the rule
  `CLAUDE.md` already states: what a machine reports never overwrites what a human typed.

  **A rename does not move the row out of its group.** The strip's tree is still built from the observed path,
  so a renamed session keeps the headings its directory puts it under and wears the typed name on the row. The
  tooltip is the address, which is the fact the tooltip is there to add.

  **And the separator stops meaning two things.** `TitleFor` names a repository's own checkout `branch:repo`,
  the strip composed `path:branch`, and one colon said opposite things in opposite orders on one screen.
  Now that `B2-02` draws the path as headings the strip's separator is `@`: `github/dovholuknf/atrium@main`
  reads as a place and a branch, and the colon is left to the board, where the distinction between a checkout
  and a worktree still earns it.

- **The model control on the new agent form is a field called `model`, and the tickbox in front of it is
  gone.** It read "run this one on a different model", which was a sentence among nouns: every neighbour is
  `runner`, `working directory`, `title`, `tags`, `why`, `first instruction`. It also asked "different" from
  a default the form never showed, and carried the one-time behaviour in the words "this one", where a reader
  who did not already know it got nothing.

  Now it is `model` in the same eyebrow style, and the hintline says the rest: this launch only, the card
  remembers it so a restart comes back on the same model, and the field is empty again next time.

  The tickbox went with the wording because it was asking you to say twice what an empty box already says.
  Empty is the runner's own default, so there is nothing left for a tick to mean. A runner that cannot take a
  model still hides the field entirely.

- **The terminal strip and the stack keep rows that tie in the same place twice.** The reported symptom was the
  strip: sorted by activity, it reshuffled itself between polls. Every axis either list sorts on is coarser
  than the list it sorts. A dozen cards share a runner, a whole project shares a worktree, waiting is a yes or
  a no, and after a quiet night most of them read the same idle second. Rows that tied kept whatever order the
  last poll delivered, and the poll does not promise one, so a repaint that changed nothing still moved rows
  and the tab you were reaching for was somewhere else by the time the cursor arrived.

  Both now fall back to oldest first, then to the card id, through one `cardTieBreak` in `core.js`. The id
  says nothing to a reader, but it is on every card and never changes, so two sessions started in the same
  second still land in the same order on every repaint.

  The strip's other sort was worse than unstable: **"sorted by name" did no sorting at all.** The button said
  one thing and the branch behind it fell through to whatever `/v1/tasks` had returned, so the one mode you
  pick because it should hold still was the one that never did. It sorts by the label the row draws, then by
  the same tiebreak. Pinned rows still come first, and the order underneath them survives.

  The stack's name axis also stopped reading `display_title` straight off the card: one card without it threw
  inside the sort and took the whole repaint with it. The strip's ordering moved into `termOrder` so it can be
  run without a poll, and `scripts/test-sort-order.js` runs both lists over rows that tie, from two different
  starting orders, and over a row with nothing filled in.

- **Clicking outside a dialog closes it, and that is now a rule for the board rather than a fix for one
  control.** The switcher is what asked for it: it opens on a keystroke, dozens of times a day, to answer
  "where do I go next", and until now the only ways out were escape and picking something. Something opened
  that casually has to be dismissible just as casually.

  The rule is which dialogs may do this, and the answer was already written down on the markup. A dialog
  carrying `data-guard` holds edits that are kept only when you press save, so it keeps its explicit close:
  light-dismiss on a form that has not been saved is a way to throw work away by twitching. Everything else
  writes as you change it or writes nothing at all, so it is holding nothing you have not already been given,
  and it closes when you click away. The switcher writes nothing and qualifies.

  **The trap is that the target is the dialog either way.** With `showModal` the backdrop belongs to the dialog
  element, so a click outside the content still reports the dialog as its target. The usual `e.target === dlg`
  test is therefore wrong the moment a dialog has padding: a click on the dialog's own padded edge is a click
  on the element, and the dialog shuts with the pointer well inside it. The handler compares the click's
  coordinates against `getBoundingClientRect` instead, and requires the press and the release to both be
  outside, so selecting text in a dialog and letting go past its edge is not a request to leave.

  `scripts/check-switcher.js` holds both halves as invariants, because both fail silently in a browser.

- **A launched claude no longer stops to ask about an MCP server it was never going to use.** Starting five
  workers at once meant dismissing a "continue without this MCP server" dialog in five windows before any of
  them did anything. One flaky global server, `mcp-gateway` in this case, is one modal per session, and at
  the scale the board is built for it is sixteen. Each of those sessions is on the board, says `running`, and
  is waiting on a human nobody told to look, which is worse than an error because there is no error.

  The lever is `--strict-mcp-config`, which limits claude to the servers named by `--mcp-config`. None is
  passed, so a launched session starts with no MCP servers at all. That is what a worker spawned to edit code
  in a worktree wants anyway: the operator's global servers are the operator's own tools, and what the worker
  needs from atrium arrives through its hooks and the `atrium` command, not through a server it has to
  connect to.

  Nothing points at a config file on purpose. `--mcp-config` naming a path that does not exist is a hard
  startup failure, so a default that named `.mcp.json` would refuse to start in every worktree without one,
  and a dead launch is not an improvement on a modal.

  It is on the resume arguments as well as the base ones, because resuming REPLACES the arguments rather than
  adding to them, and the flag missing there is the same dialog on the second start. A migration puts it on
  the `claude` row of databases that already exist, since the harness table is seeded once on first run; a
  row whose arguments have been edited is left alone, because that is somebody's own command line.

- **The launch form offers the repositories on this machine, and atrium can make the worktree.** There is a
  "projects" button beside "browse". It opens a list of every checkout the daemon can see, grouped by the
  directory above it, and opening one shows the worktrees that already exist for it. Clicking any of them puts
  its path in the working directory field. Typing a branch name and pressing "make a worktree" runs the
  configured command in that repository and takes you to whatever comes out.

  **Atrium RUNS the tool. It does not reimplement it.** `internal/daemon/recognise.go` says that making a
  worktree is `gwt`'s job and that atrium has no business owning a checkout layout, and that is still true.
  Running `git worktree add` here would mean atrium deciding where a worktree lives, what it is called, and
  what happens after it is made, and the layout on a machine is a convention that some other tool maintains:
  the moment atrium encodes it, it owns it and drifts from the thing that actually keeps it. So the new
  `worktree_command` setting is a command TEMPLATE, the way a harness and a source already are. Atrium holds
  the name of a command and never the thing behind it.

  **It is hosted by a shell, and that is not a shortcut.** `gwt` is a PowerShell function defined in a profile
  rather than a program on `PATH`, so there is nothing for `exec.Command` to spawn. `internal/shellpick`
  already answered which shell this machine has, with the echelon that matters on Windows — `pwsh` is
  PowerShell 7 and a separate install, `powershell` is 5.1 and is on every Windows, `cmd` is the floor — so
  that answer is reused rather than asked a second time. The profile is loaded rather than skipped, because
  the profile is where the function being run is defined.

  **Which moves the fence.** `editor_command` can split its template into a program and arguments because the
  part it does not control is a filename, and a filename is data. A shell has no such promise: every character
  of a branch name is live. So the branch is checked against what a branch may hold — letters, digits, and
  `. _ - /`, starting with a letter or a digit — BEFORE it reaches the line, rather than quoted afterwards.
  Quoting is a claim about one shell's grammar and this runs under three. The command also gets no stdin, so a
  template that stops to ask a question gets an end of file instead of a wait, and it is bounded at three
  minutes, because a daemon holding a request open forever is how one wedged command takes the board with it.

  **The path is read back out of git, not out of the output.** What the tool prints is for a person: it is
  coloured, it is several lines, and its wording is not a promise. Where the worktree ended up is a question
  `git worktree list --porcelain` answers exactly, which also means a template doing something else entirely
  still works as long as a worktree comes out of it.

  **Already there is the answer, not an error.** The common case for "make me a worktree for this branch" is
  that one exists, and what somebody wants then is to go to it. The daemon checks first, says `existed`, and
  the board says "already there" and takes you to it.

  **The listing owns no layout either.** The repositories come from the directories the picker is already
  allowed to open, walked a fixed `project_scan_depth` — two by default, matching `<root>/<org>/<repo>` —
  rather than recursively, because a walk of a drive looking for every `.git` is a scan somebody waits on. A
  directory holding a `.git` is a repository and is not descended into. The walk is bounded while it reads
  rather than after, and says so when it stopped early. The worktrees come from git itself, asked of each
  repository eight at a time with a timeout each, so a machine that keeps its worktrees somewhere unusual is
  still described correctly and one repository on a slow share cannot hold up the list.

  Both settings are in settings, this machine, under the browse roots they depend on, and both are exported
  and imported with the rest of the configuration. An empty command means the default, `gwt new {branch} -y`,
  and `off` means there is no make button at all. `guestHandler` is an allow list, so a guest holding a lent
  session reaches neither endpoint.

- **A throwaway session, in a directory that deletes itself.** Tick "throw it away when the session ends" on the
  new agent form and atrium makes a temporary directory, starts the runner in it, and when the session is over
  the directory, the card and the conversation are all gone. For the case of wanting to try something without
  first deciding where it belongs.

  "Gone forever" is three deletions and the third is the one that gets forgotten. Claude Code keys its
  transcripts on the working directory, so deleting the directory alone leaves a transcript orphaned under an
  encoded name for a path that no longer exists, one per throwaway, accumulating forever. `ForgetTranscripts` in
  `internal/api/throwaway.go` takes the whole project directory, which is the difference from forgetting one
  conversation somebody picked out of a list.

  **All three happen in `awaitExit`, after `cmd.Wait` has returned.** The runner's working directory IS the
  directory, and Windows will not let a live process have its cwd removed. A delete written where the exit is
  REQUESTED appears to work and leaves the directory behind. Every way a session ends goes through the same
  wait, so a crash cleans up as thoroughly as `atrium finish` does, and a daemon that was killed and therefore
  waited on nothing is caught by a sweep at start up.

  **A throwaway is never reopened.** A restart now brings back every card that had a runner, and a throwaway
  whose directory has been deleted would be asked to start in a directory that is not there: a dead card after
  every restart with nothing on it to explain why. `reopenWanted` refuses on what the card says about itself
  rather than on whether the directory happens to still exist, because a daemon killed before the delete leaves
  one behind.

  **And there is a way out, which is what makes it safe to reach for.** Somebody will clone a repository into a
  throwaway and work in it for an hour. "Keep this work…" on the card menu moves the directory somewhere real
  and the card stops being temporary. While a session is still running the destination is written down and the
  move happens as it ends, for the same Windows reason the delete does; with nothing running it happens at once.
  The move falls back to a copy when a rename cannot cross volumes, which is the ordinary case: temporary
  directories are on whichever volume the operating system keeps them on.

- **A paste into a long-running session is bracketed again, however long it has been running.** Pasting a
  ninety-eight line block into a supervised claude session produced five separate `[Pasted text #n]` blocks in
  the prompt, and the same splitting ate the middle of a multi-line peer report.

  The split itself is the operating system. ConPTY's input handle is a Windows anonymous pipe with a four
  kilobyte buffer: a bigger write does not fail and does not truncate, it blocks until the child drains, so the
  child receives the paste in buffer-sized installments with real time between them. A terminal user interface
  decides whether input was typed or pasted from how it arrives, so each installment reads as its own burst.
  Bracketed paste is what denies that: the markers say "this is one paste" no matter how it lands.

  So the question is only ever whether the board wraps a paste in the markers, and its answer was coming from
  the wrong place. The board asked `term.modes.bracketedPasteMode`, which is xterm's record of what it has
  parsed, and the only evidence is the `\x1b[?2004h` the runner sends ONCE, at startup. A pane that attaches
  after the ring has wrapped past that byte sees no evidence and pastes raw. That is the intermittency nobody
  could explain: the same clipboard into the same session behaves differently on different days, because the
  difference is how much output has scrolled past since the runner started. The evidence is exactly the thing
  the ring is entitled to discard, so no amount of care in reading the replay could have fixed it.

  A harness now DECLARES it. `bracketed_paste` is a column on the runner's row, ticked for claude and codex,
  off for a shell and for anything undeclared, and there is a checkbox on the runner's form. An attach sends it
  as its first message, `{"t":"caps"}`, which is the first thing the daemon has ever said to the board down
  that socket in words rather than in bytes. The board brackets when either source says so, so a declared
  runner is bracketed from the first paste and an undeclared one behaves exactly as it did before.

  It is a fact about the PROGRAM, not about the terminal: which runner this is, fixed for the life of the
  process, and already configuration. The daemon is not tracking terminal modes and does not know what the
  terminal is doing right now. A shell is deliberately left out for that reason: it turns the mode on and off
  around each prompt, so it is not a property of the program, and it re-emits its own enable often enough that
  the stream is a good answer there.

- **A card can open its directory in a real terminal window on the desktop.** Right click a card, "open in a
  terminal window", and the configured terminal opens there, beside the board.

  The question that started this was whether `wt.exe` could be a pane's shell, with a fallback chain down to
  `cmd.exe`. It cannot. `wt.exe` is a terminal EMULATOR, not a shell: it creates its own window with its own
  ConPTY, hosts a shell inside that, and returns immediately. A supervisor that spawned it would hold a pty
  nothing ever writes to, show an empty pane, and watch its runner exit within a second, which the reaper
  would correctly file as a dead card. In the fallback chain it would look like a configuration option and
  behave like a crash.

  What the question was really asking for is this: the card's directory, in a terminal, on the desktop. That
  is an action rather than a runner, and `editor_command` is the precedent. So a new `terminal_command`
  setting carries the same three fences, in `internal/api/termopen.go`:

  **Off until configured, with no default.** There is no guess at which terminal you have. Empty means the
  menu entry is absent, not dimmed, because nothing on a card could make it work.

  **The operator writes the command, and it is never a shell.** The string is split into a program and its
  arguments and handed to `exec.Command`, using the same splitter the editor uses. A directory called
  `x; shutdown` is one argument called `x; shutdown`.

  **The directory is resolved through `internal/safepath`** against the card's own worktree, so symlinks are
  followed on both sides and what the program receives is a real directory. A worktree that has gone fails
  here rather than as an argument to a terminal.

  **And the fourth thing, which is the whole point here.** The DAEMON runs the command, so the window appears
  wherever the daemon is. For an editor that is a caveat. For a terminal it is the difference between a useful
  button and a confusing one, because a terminal is exactly what somebody reading the board on a laptop over
  a share would expect to get locally. The menu entry says "on atrium's machine" next to the label, its help
  says it again, and the toast names the machine the directory is on. A guest holding a lent session never
  sees the entry at all: `guestHandler` refuses `/v1/settings`, so the board reads no terminal command and
  offers nothing.

  `{path}` in the command is the directory, and a command without it gets the directory appended. **`wt.exe -d
  {path}`** is the one worth knowing: it opens a tab in the running Windows Terminal at that directory. The
  setting is exported and imported with the rest of the configuration, and for now it is set over the API
  (`POST /v1/settings` with `{"terminal_command": "wt.exe -d {path}"}`) rather than from the settings dialog.

- **Ending a session no longer claims atrium is restarting, and it puts you back on the terminal you were on
  before.** Operator: "it should pick the last window if i exit like that".

  Typing `exit` put up `atrium is restarting. waiting for ... to come back` and left it there for ninety
  seconds, over a bar that said `nothing attached`. Atrium was not restarting and nothing was coming back. The
  banner cried wolf about the one message that matters when a restart is real, because the real one looks
  identical.

  **The daemon already said which it was.** `whyClosed` puts `restarting`, `shell closed` or `runner exited` on
  the websocket close frame, and the board dropped the word on the floor. Every teardown then started the
  restart wait, and `waitLoop`'s only early exit tested `archived_at`, which a session you just closed is not.
  The reason is now read, written down where the teardown can see it, and only `restarting` starts a wait.

  The close frame is also believed when it says `restarting`, so a restart is still recognised when the
  `going-down` event is the half that goes missing.

  **Two other places stopped saying "restarting" without being told one was happening**: an attach that has
  never opened, which is usually a fixture that has not started yet, and the restore loop on an ordinary
  reload. Both now say they are reconnecting, and keep the restart wording for a restart that was announced.

  **And the pane is not left empty.** A new `atrium.termPrev` slot holds the terminal attached before the
  current one, written where `atrium.term` is overwritten, and an exit falls back to it. It refuses in the
  cases `waitLoop` already refuses in: something else is attached, you have left the terminals view, the card
  is in its own window, or this window is one terminal. With no previous terminal it says nothing attached
  rather than picking one at random.

- **A public share can be revoked from the board again. Both ways out of one were broken at the same time.**

  The header pill did nothing when clicked and the card menu refused to offer stop, so an operator holding a
  live public address had no way to give it up from the board at all. The two failures are unrelated in cause
  and were fixed together because either one alone still leaves a published session with no way off.

  **The pill.** `askUser` calls `showModal` on one shared `<dialog>` element, and `showModal` on a dialog that
  is already open throws `InvalidStateError`. `openSharing` did not await the dialog, so the rejection had no
  handler and the click looked like it did nothing, for the rest of the session. A question asked while
  another is up now rewrites the dialog in place rather than opening it twice, and the caller who was waiting
  on the old question is answered with a cancel instead of being left on a promise nothing settles. Every path
  out of `openSharing` is awaited, and a failure raises a toast.

  **The card menu.** `shareItem` built the whole zrok section inside `if (ready("zrok"))`, so the ability to
  STOP was gated on the same condition as the ability to START. When zrok went away, the stop option went with
  it, on a card that was still published, and the menu said "no overlay is set up yet", which was false. The
  rule now: readiness decides what can be started and nothing else. A share that exists can always be stopped.
  When the overlay is down the menu says so and says that stopping still unpublishes the card, since giving
  the reserved name back is the only part that needs zrok.

  **The `shared` chip does what it says.** It has always claimed "right click to stop" in its tooltip. Right
  clicking it now goes straight to the stop confirmation rather than to a card menu that was hiding the stop.

- **The terminal strip's grouping is checked on what it DRAWS, not on the tree behind it.** B2-32 reported
  single member orgs landing under the group above them: `openziti-test-kitchen` and `netfoundry` drawn inside
  `github/dovholuknf/atrium`, and `ziti-sdk-csharp` inside `desktop-edge-win`, each wearing the rest of its own
  path on the row because the renderer had segments it never turned into headings.

  That does not reproduce on the current renderer. `termNodeHTML` builds a container per level and every level
  gets its heading, so a row is inside the headings that spell its path and nowhere else. The behaviour the
  report describes belongs to the collapsing renderer that came before it, which folded a chain of only
  children into one heading and a single row level into its row.

  **What was missing is the test.** The grouping had been verified by reading `termTree`, and a correct tree
  flattened wrongly renders exactly the way the report describes, so the check could not have caught it.
  `scripts/test-term-nesting.js` runs the real functions over a list of sessions, parses the markup they
  produce, and walks the `.tnest` containers around every row to work out which headings the row is actually
  inside. The invariant is that a row is inside the headings that spell its own path and no others, which is
  what the indent and the guide line down the left of a group claim on screen.

  It covers the case that fails whenever the flattening is wrong, a group with several members followed by a
  single member sibling one level shallower, plus a folded org, a session with no path at all, and a chain
  where every level holds one thing. It was watched to fail: a run based flattener put back in place of
  `termGroupsHTML` broke 52 assertions and reproduced the reported rows exactly.

  **The grouper does not assume the list arrives in group order.** `termTree` files each session into a map by
  its own path, so the nesting is the same whichever order the strip is sorted in. The test asserts that by
  drawing the same sessions forwards, backwards and shuffled.

- **A write into a supervised session now finishes, instead of reporting success with the tail missing.**
  B2-14.

  `runner.Write` is the one funnel for everything atrium puts into a session: a keystroke or a paste from the
  board, an interrupt, and everything atrium SAYS to a session, which covers a queued message, a note, and an
  action's stored prompt. It called the pty once and threw away the byte count. An `io.Writer` is allowed to
  take fewer bytes than it was handed and return no error, so a short write left part of the input gone with
  nothing anywhere reporting a problem. The only party that knew was the line that discarded the number.

  `Say` is where that turns from lost data into wrong behaviour. It writes the text, pauses, then writes the
  Enter that submits it. A short first write still gets its Enter, so an agent receives half a prompt as
  though it were the whole one and acts on it.

  It now loops until every byte is taken or the pty returns an error, and a writer that reports no progress
  and no error gets `io.ErrShortWrite` rather than an endless spin.

  **The continuation is immediate and the comment says why.** A TUI decides input is a paste rather than
  typing from how fast it arrives. `Say` depends on that, and the board sends a paste as one frame for the
  same reason. A sleep between the parts of one write would split a paste in two and undo both, which is
  exactly what the obvious retry loop does.

  This is not the cause of a paste arriving in several pieces. That is the pty's input pipe, and it is its own
  bug. This is hygiene on the funnel every input passes through.

- **The tab wears the atrium A.** B2-01. There was no `<link rel="icon">` on the board at all, so every atrium
  tab carried whatever a browser shows when nothing is supplied, which the operator reads as a stray bracket.

  **No image file was added, because the mark was already in the code.** The desktop notifications draw an A
  onto a canvas: two strokes and a gradient from `#00E3B0` to `#28C2FF` on the board's navy. That drawing is
  now `drawAtriumA` in `js/core.js`, `boot.js` paints it into the tab's icon at load, and the notification
  code calls the same function. The two cannot drift, because there is one drawing. Nothing is fetched, so
  there is no new route, no asset, and no question about how long a browser holds an old icon.

  Popped-out terminal windows get it too, for the reason they get the skin: a window in the mark beside one in
  the browser default reads as two applications.

  **The settable logo is not in this.** `internal/api/icon.go` already takes an uploaded image for a card and a
  board-wide logo should reuse it, but where a custom logo appears and what empty means are decisions of their
  own. Filed separately.

- **A launch can name a model, once.** B2-29. Operator: "i really wish i could relaunch u under the fable
  model", "i want that immediately, i also want it to BE EASY to turn on and off, and ONE TIME ONLY sorta
  thing".

  The only ways to do this were editing the runner or keeping a second runner that differs by one flag, which
  turns the runner table into a list of models and multiplies: two runners and three models is six rows kept in
  step by hand.

  **One time with respect to the RUNNER, sticky with respect to the CARD.** The tickbox on the new agent form
  starts off, goes back to off every time the form opens, and writes nothing to the runner's row. The card
  keeps what it was launched with for its own lifetime, and `reopen` replays it, so a session that started on a
  model is still on it after a restart. Those are two different questions and reading them as one is how this
  goes wrong.

  **A runner declares how it takes a model, the same way it declares how it resumes and how it is prompted.**
  `model_args` with `{model}` substituted. A runner with none cannot be asked, and a launch that asks anyway is
  REFUSED rather than started on the default: a session quietly running on the wrong model is invisible until
  the output or the bill is wrong. A shell has no model and its row stays empty.

  **The model names come from what has been typed before, never from a list in atrium.** Which models exist
  changes every few months and a list written into the code ships wrong. The box is free text with a history
  behind it.

  Existing databases get `--model {model}` on their `claude` and `codex` rows by migration. Without it the
  column would arrive with no way to use it on every board that already exists, and the runner most likely to
  be asked would be the one that refuses. Nothing else is guessed at: ollama takes its model as a positional
  argument that is already in `args`.

  The card shows which model, because two cards in one directory on one runner and two models are otherwise
  indistinguishable. `atrium launch` takes `--model`.

- **The board is a stylesheet and two dozen scripts instead of one 23,724 line file. No behaviour change.**

  Every UI change edited `internal/api/web/index.html`, so no two people could work on the board at once
  without colliding in it. It is now `index.html`, `board.css`, and 24 files under `web/js/` that the page
  loads in order.

  **Plain scripts, not modules.** The script was one shared global scope: functions and top-level state
  reference each other freely across what are now file boundaries. Classic scripts on one page share one
  global lexical environment, so loading them in the order the code was written in preserves that exactly.
  Threading imports through 17,000 lines of it would not have been a behaviour-preserving change, and the
  breakage would not have shown up until a specific dialog opened.

  The cuts are at the section banners the file already carried, so the concatenation of the js files in load
  order reproduces the old script body byte for byte. Nothing was renamed, reordered, reformatted or fixed.

  **`BuildID` now hashes the whole `web/` tree.** It hashed `index.html`, which used to be the board and is
  now a loader. Left alone, the build id would have stopped changing when the board changed, and the reload
  when a new daemon is installed would have quietly stopped happening.

  **The checkers were rewritten, not weakened.** `scripts/board-source.js` puts the pieces back into one page,
  and every checker is handed that, so all nine keep their assertions. `check-board.sh` asserted "exactly one
  script block"; what replaces it is that the page names every file in `web/js` exactly once. Each checker had
  its bug reintroduced and was watched to fail.

  **A lent session serves the new files.** `guestHandler` names what a guest may fetch, and a guest refused
  `/board.css` and `/js/` would have got an unstyled page with no script on it.

- **A published board can ask for a name and a password.** Operator: "make sure it has auth now. basic auth is
  fine for starters".

  The login that existed was OIDC only, and it needs a provider, a client registered with it, a redirect
  matching the published address exactly, and an allow list. Somebody putting a board on a zrok share for an
  afternoon has none of those, so what they reach for instead is publishing it with no login at all. That is
  the case this closes.

  **A name and a password is a complete configuration.** Demanding an issuer from somebody who set a password
  would refuse the simple case for missing the complicated one. Both can be set, and the password is checked
  FIRST, so a board with a provider still opens when the provider is down and an api client can present
  credentials rather than being told to visit a login page.

  **A wrong password falls through rather than refusing.** Adding a password to a board that already had a
  provider must not lock out the people who were using it.

  The password is salted and hashed with scrypt, deliberately slow, since the threat is somebody who has taken
  the database and is guessing offline. A fresh salt every time, or two boards with one password store one
  hash. It is never stored in plain, never sent back to the page, and `GET /v1/auth` answers whether one is set
  and nothing else. Both comparisons are constant time, the name as well as the password, so the pair cannot be
  learned one at a time.

  A successful password issues the same session cookie the provider flow does, so the browser is not asked
  again on every poll and every image, and signing out means one thing either way.

  **None of this touches the loopback board.** It guards the PUBLISHED handler only, the same line the OIDC
  flow already drew.

- **A share says what your account calls it.**

  The panel showed the address and the daemon held the share token without ever sending it. A public share's
  address is a URL on a frontend and the share itself is a token, so somebody opening their zrok console to
  find what atrium made had the address here, the token there, and nothing joining them. Operator: "show me the
  share name so i can find it".

- **The terminal groups nest on screen, and fold.**

  The grouping shipped with the tree in the markup and nowhere on the display. Headings carried a depth and
  ROWS CARRIED NOTHING, so a session three levels down sat at the same left edge as one at the top: the list
  had headings in it and still read as flat. Indenting rows by depth would have fixed the alignment and
  nothing else.

  A container per level gets the rest for free. The indent belongs to the box, and a border down its left edge
  draws the vertical guide that says which rows are under which heading, the way `tree` does with line drawing
  characters. The lines are continuous because the box is, rather than being reassembled per row out of glyphs
  that have to agree with each other about depth.

  **Groups fold**, with a caret, and a count so a folded group says what is inside rather than losing it.
  Folding is per browser and keyed by PATH, so folding `github/openziti` and later starting work in a new org
  leaves the fold where it was instead of sliding onto whatever moved into that slot.

  **The heading is a button only as wide as the thing you press.** `docs/dispatch-queue.md` group F carries a
  complaint that the board's group headings toggle across their whole width, so clicking what looks like empty
  space beside a name folds what you were reading. The caret and the name are the control here and the rest of
  the row is outside it.

  In `mini` the name goes and the caret and count stay. At 104 pixels a heading spends its whole width on
  `openziti` and leaves nothing for the rows, and the indent and the guide carry the shape anyway.

  Found by rendering the real markup for the real cards and reading the nesting back out of it. The check that
  walked the TREE had been passing all along, which is what let a flat rendering of a correct tree ship.

  Three corrections after looking at it:

  - **Two headings shared a line.** The button was `inline-flex`, which makes a heading behave like a word, so
    a folded `openziti` and the `dovholuknf` after it came out side by side and read as one heading with two
    names in it. `flex` with a `fit-content` width is block level and still only as wide as the caret and the
    name, which is what keeps it from being a full-width click target.
  - **The count was the quietest thing on the row**, a nine pixel pill in the chip colours. It is the heading's
    own font and colour now. A count nobody can read is a count that is not there, and it is the one thing a
    folded group has to say.
  - **Loose sessions get a heading.** A directory with no forge and no org in it sat under nothing at the top
    of the list, which read as a session that had escaped the grouping rather than one the grouping has nothing
    to say about. They are under `uncategorized` at the BOTTOM now, since a catch-all above everything is the
    first thing read and the least interesting. Still absent when there is nothing to contrast it with, because
    a heading over the whole list names nothing.
  - **Every level keeps its own heading**, whatever it holds. Two collapses were written to keep the height
    down and both produced the same defect, one level apart. The first folded a chain of only children, so
    `openziti-test-kitchen/docpreview` was text on a row while `openziti` beside it was a heading. The second
    folded a level holding one row, so `ziti-openwrt` was text on a row while `desktop-edge-win` beside it was
    a heading, decided by nothing about either repo except how many branches happened to be checked out.

    The shape of the list says host, then org, then repo, every time. Somebody reading it should not have to
    work out which rule applied to which row, and height is the wrong thing to spend that on.
  - **A heading directly under a heading sits tight to it.** Every level carried its own top margin, so
    `github` f
