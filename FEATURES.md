# Features

What atrium can do, organised by area. `CHANGELOG.md` is the running log, newest first and mostly fixes. This file
is the other view: one entry per capability, so a person or an agent arriving later can see what exists without
reading the log end to end.

## Keeping this file current

Every commit that adds or removes a user-facing capability adds or edits its entry here, beside its CHANGELOG
entry. A fix that changes what a user can do edits the entry it changes. A removed capability moves to the "removed,
and why" list of its area. Dates are commit dates on `claude/main`. An entry marked **claude/main only** has landed
on `claude/main` and is not yet on `main`. Whether a build is deployed on a given machine is not tracked here.

## Contents

- [The board](#the-board)
- [Cards and columns](#cards-and-columns)
- [Terminals](#terminals)
- [Permissions and auto mode](#permissions-and-auto-mode)
- [Messages, peers and agent commands](#messages-peers-and-agent-commands)
- [Rooms and the hub](#rooms-and-the-hub)
- [Overlays and sharing](#overlays-and-sharing)
- [Runners and runner setup](#runners-and-runner-setup)
- [Intake, sources and providers](#intake-sources-and-providers)
- [Files](#files)
- [History, audit and notifications](#history-audit-and-notifications)
- [Settings and skins](#settings-and-skins)
- [Packaging, install and operations](#packaging-install-and-operations)
- [Diagnostics](#diagnostics)
- [The older v1 modes](#the-older-v1-modes)

## The board

- **You see every agent session as a card on one web board.**
  The board answers what is running, which session needs you most, and what you were doing in each one. It is a
  plain page served by atrium and speaks the same JSON and SSE API as any other client. It is not a terminal
  multiplexer. Docs: `README.md`, `docs/how-atrium-works.md`, `docs/architecture-v2.md`. Landed 2026-09-01
  (`fdc5c8e`).

- **You group, sort and filter cards by tags you choose.**
  A card carries free tags, and the board groups by project or by tag. Grouping off turns grouping off. Group
  headings stay alphabetical so the list does not rearrange itself while you read it. Docs: `docs/user-guide.md`.
  Landed 2026-09-03 (`4813e4c`).

- **You sort the board by activity, name or manual order from a pill in the board's bar.**
  The sort applies inside each group and never reorders the groups. Activity is the default. Pinned cards and your
  own custom groups keep the order you set by hand under any sort. Docs: `CHANGELOG.md`. Landed 2026-09-15
  (`bcdf409`), with rank held under any sort on 2026-09-22 (`12e017f`).

- **You make custom groups and file cards into them.**
  Under `by group` you add a named group, and `into group` on a card's menu files the card there. An empty group is
  drawn with a hint on how to fill it. A card can sit in more than one group. Docs: `CHANGELOG.md`. Landed
  2026-09-22 (`c2c70c3`, `081d217`).

- **You collapse a group and it stays collapsed.**
  A project group folds board-wide by name, so a card moving between columns does not reopen it. Folds are held in
  the browser. Docs: `CHANGELOG.md`. Landed 2026-09-15 (`2017744`).

- **You jump to any session with a keystroke switcher.**
  `ctrl-shift-k` opens a filter over title, directory and tags, with the sessions you visited last listed first.
  Inside a popped-out window it moves that window to another card. The key is a setting, because browsers keep
  different keys for themselves. Docs: `docs/switcher-design.md`. Landed 2026-09-07 (`3e6f425`).

- **You click outside a dialog to close it, unless it holds unsaved edits.**
  A dialog marked `data-guard` keeps its explicit close. Every other dialog closes on a click that starts and ends
  outside it. Docs: `CHANGELOG.md`. Landed 2026-09-12 (`95e9e99`).

- **The board keeps your scroll position and place across repaints.**
  Lists repaint in place, so a poll does not throw away where you were. Browser back and forward move between
  views. Docs: `docs/board-repaint.md`. Landed 2026-09-06 (`ab5afe6`), back and forward on 2026-09-11 (`d7ea87e`).

- **The board works on a phone.**
  The header holds its shape from a phone to a wide monitor, with 40px tap targets at touch widths. On a phone the
  terminals list collapses to a dropdown and the split between list and terminal can be dragged. Docs:
  `CHANGELOG.md`. Landed 2026-09-19 (`a1b23e1`, `5948080`), phone terminal on 2026-09-20 (`376a5a1`, `946332f`).

- **The tab wears the atrium A.**
  The favicon is drawn in code by the same function that draws the notification mark, so the two cannot drift.
  Popped-out windows get it too. Docs: `CHANGELOG.md`. Landed 2026-09-11 (`1f5163a`).

- **A new board build reloads open pages on its own.**
  The build id hashes the whole `web/` tree, and a page reloads when it sees a new one on `/v1/health`. Docs:
  `docs/reload-design.md`. Landed 2026-09-04 (`a874f91`).

- **You can draw every card in its own terminal's colours, to find a session by colour.**
  `cards wear their terminal colours` in the gear's board pane, off by default. On, each card in the terminals
  list, the stack and the board columns takes its terminal theme's background, foreground and accent, and its
  chips take the theme's ANSI colours. Every title, path and chip is held to 4.5:1 against its card, so a theme
  whose accent reads worse is nudged toward white or black until it passes. The attached card keeps a two-pixel
  frame, open toward the pane. Kept in this browser, like text size. Docs: `CHANGELOG.md`. Landed 2026-09-23 on
  `claude/card-colors`, not yet on `claude/main`.

### The board: removed, and why

- **Dragging cards on the board.** Removed 2026-09-06 (`c67b345`). A draggable card takes the pointer, so a sweep
  across its text started a drag instead of a selection, and copying a path or a branch off a card is a daily
  thing. `Move it to` and `move it up or down` on the card menu replace it. Dragging rows in the terminals list and
  the pinned bucket is separate and still exists.

## Cards and columns

- **A card outlives the process it describes.**
  A card is a place work happens. It survives the runner exiting, a restart and the conversation, and its runner's
  pid is only a reconnect hint. Liveness is read from the operating system, so it costs no turn and no token. Docs:
  `README.md`, `docs/how-atrium-works.md`. Landed 2026-09-01 (`c51ae30`).

- **Cards sit in columns that are buckets of your attention.**
  The columns are needs permission, ready, running, finished and shelved, plus an inbox for work not started. An
  empty column gives its width back, and a column that cannot fill hides itself. Docs: `README.md`. Landed
  2026-09-01 (`fdc5c8e`), empty lanes compress on 2026-09-03 (`2aef055`).

- **A card tells a question apart from a finished turn.**
  A session blocked on you sorts above one that ran out of work, because the Notification hook fires only for the
  first. A compacting session counts as working. Docs: `README.md`, `docs/hooks.md`. Landed 2026-09-04 (`304fbe0`,
  `4aaf174`).

- **Each card shows what its runner is doing right now.**
  A live badge says thinking, running a named tool, or how many subagents are working, and for how long. It is
  never stored, because it would be wrong the moment the daemon restarted. Docs: `docs/activity-design.md`. Landed
  2026-09-02 (`a85e2a3`).

- **A card shows how much context its session has burned.**
  A statusline script posts to `/telemetry`, and the card draws context used and account limits. The statusline
  script lives in another repository. Docs: `docs/statusline-telemetry.md`. Landed 2026-09-07 (`2ba1a2d`).

- **You pin cards, and pinned cards keep the order you set.**
  Pinned cards sit at the top of their column and of the switcher, with a line under them. Their order is a stored
  rank written by move up and move down, and the board sort never reaches into it. Docs: `CHANGELOG.md`. Landed
  2026-09-03 (`b2def4d`), line under pins on 2026-09-06 (`2985923`).

- **You rename a card, and the name you typed wins everywhere.**
  What a human typed is kept apart from what a hook reports and always wins, on the board, the terminal pane, the
  switcher and the window title. A rename does not move the row out of its group. Two cards with one title fall
  back to their wire names. Docs: `CHANGELOG.md`. Landed 2026-09-14 (`d01a9ff`), wire-name fallback on 2026-09-21
  (`182e5da`).

- **Atrium works out which repository a card belongs to, and you can correct it.**
  `display_repo` reads `<forge>/<org>/<repo>` off the path when the launcher did not say, and is never stored. A
  `which repo` entry beside rename overrides it. Docs: `CHANGELOG.md`. Landed 2026-09-15 (`bcdf409`).

- **You archive a card instead of deleting it, and reviving it brings it back.**
  Archived cards stay in history. Resuming a card falls back to a fresh start when its conversation is gone. Docs:
  `CHANGELOG.md`. Landed 2026-09-03 (`048bc7c`, `da746a1`).

- **You set a priority on a card and give it its own mark.**
  A card's icon appears on its desktop notifications, drawn to a canvas so it is never markup. Docs:
  `docs/user-guide.md`. Landed 2026-09-04 (`c248274`), priority on 2026-09-05 (`99e2d8e`).

- **You select and copy the text on a card, and file it with the card menu.**
  The card menu does not open over highlighted text. `Move it to` files a card in another column, but never into
  needs permission or ready, because only an agent puts a card there. `done` is a state only a human declares.
  Docs: `CHANGELOG.md` at `c67b345`. Landed 2026-09-06 (`c67b345`).

- **A machine that is not answering still shows its cards, and nothing on it opens.**
  Cards from an offline room are drawn from what it last said, in one folded group at the bottom of every column.
  Attach, resume and start are not drawn, and the hub refuses every request for that room by name. Nothing is
  queued for its return. Docs: `CHANGELOG.md`, `docs/hub-room-requirements.md`. Landed 2026-09-17 (`a59d4d2`).

- **A card says whether you have seen its last turn, and whether you answered its Open Questions.**
  An unread turn wears a teal dot, and a turn that ended on unanswered Open Questions wears `? N` with the
  questions in its tooltip, on the board card, the stack row and the terminal strip row. A turn is seen when a
  focused, visible window shows that card's runner terminal scrolled to the bottom for 3 seconds, or when you type
  into it, submit a prompt, or send it a message. A peer's message never counts. Only the question lines are kept,
  never the message. A reply answers them. Stored, so it survives a restart. Docs: `docs/seen-design.md`. Landed
  2026-09-23 (`e911361`, `dc88645`, `d1bff8c`). **claude/main only**.

### Cards and columns: removed, and why

- None recorded.

## Terminals

- **Atrium runs each agent under a pseudo terminal it owns, and you attach from the browser.**
  You type into the session, read it, stop it and restart it. A supervised runner dies with the daemon that owns
  its pty, so a restart resumes conversations rather than reattaching processes. Docs:
  `docs/supervision-design.md`. Landed 2026-09-01 (`b054a57`).

- **You pop a terminal into its own window.**
  The window is the same page in terminal-only mode, titled with the session's address, marked when that session
  wants you, and closed when its runner exits. It rides out a hub restart and reconnects itself. Docs: `README.md`.
  Landed 2026-09-04 (`c248274`), reconnect on 2026-09-19 (`f2c6782`).

- **The terminals list groups sessions by host, org and repo, and the groups fold.**
  Every level keeps its own heading and a guide line down its left edge. Loose sessions sit under `uncategorized`
  at the bottom. Group headings read shown out of total when rows are hidden. Docs: `CHANGELOG.md`. Landed
  2026-09-11 (`c04f880`), shown/total on 2026-09-22 (`feb00c9`).

- **Pinned terminals are a bucket you arrange by drag, and a pinned row stays after its session exits.**
  You drag a row in to pin it and within the bucket to reorder. A pinned row whose session exited is drawn cold,
  and a click offers to start it again on the same card. The order is stored on the card, so it follows you to
  another browser. Docs: `CHANGELOG.md`. Landed 2026-09-14 (`d01a9ff`).

- **You hide inactive agents and subagents in the terminals list, each on its own.**
  `hide inactive: agents` hides rows with no live connection, the grey ones, and `subagents` hides subagents that
  are not working now. The pinned bucket counts its hidden rows too. Agent-launched doers are marked by an
  `origin:agent` tag and can be hidden. Docs: `CHANGELOG.md`. Landed 2026-09-21 (`df3cbfe`, `03ef28f`), agents
  toggle restored on 2026-09-23 (`1f04814`, `af5afde`, `54d8944`). The last three are **claude/main only**.

- **The terminals list controls are a tray that rolls up to a summary line.**
  Folded, the tray reads like `sorted by activity · by project · hiding inactive subagents (2)`. Open, it lays out
  sort, hide inactive and group as rows of pills. Folded or open is remembered per device. Docs: `CHANGELOG.md`.
  Landed 2026-09-23 (`d84fe9b`). **claude/main only**.

- **The terminals list sorts stably, and sorted by name really sorts.**
  Rows that tie fall back to oldest first, then card id, so a repaint does not move them. Docs: `CHANGELOG.md`.
  Landed 2026-09-12 (`95e9e99`).

- **You search a terminal's scrollback, and scrollback reaches back past resizes and restarts.**
  History is replayed through a screen model rather than stripped. After a restart, the saved history is joined in
  front of claude's reprint so older turns show once. `replay_flat = on` puts the older flattener back without a
  rebuild. Docs: `CHANGELOG.md`. Search landed 2026-09-04 (`c4f6b7b`), screen replay 2026-09-14 (`53b728c`),
  restart history on 2026-09-08 (`45dffb3`) and joined history on 2026-09-22 (`3fb67a5`, **claude/main only**).

- **A narrow window does not shrink a shared session. It scrolls sideways.**
  The pty's width follows the widest attached viewer and its height the shortest. A claude terminal never goes
  narrower than `terminal_min_cols`, 120 by default. A drag sends one resize once the window settles. A shell is
  exempt from the floor. Docs: `docs/terminal-resize-decoupling-design.md`. Landed 2026-09-22 (`7d16408`,
  `aa5e202`, `f787af4`). **claude/main only**.

- **Atrium never types into a line you are writing.**
  Every automated write waits until your line is empty and the keyboard has been quiet for two seconds. A held
  write is queued and retried. Atrium also refuses to type into a permission dialog it did not raise. Docs:
  `docs/agent-messaging.md`. Landed 2026-09-20 (`97566c0`), all writes gated on 2026-09-22 (`5310857`), dialog
  guard on 2026-09-16 (`704b2e9`).

- **A paste reaches the session as one paste.**
  A runner declares `bracketed_paste` on its row, and the board brackets from the first paste whatever has
  scrolled past. Long messages atrium types go in as one bracketed paste too. Docs: `CHANGELOG.md`. Landed
  2026-09-14 (`93f6e59`), long messages on 2026-09-18 (`bde2da9`).

- **You open a shell beside a wedged agent.**
  `open a shell here` opens a shell on the card rather than a second card. Shells are a property of the machine and
  can be switched off. Docs: `docs/supervision-design.md`. Landed 2026-09-05 (`c34e9a2`), switchable on
  2026-09-17 (`6a6747d`).

- **You restart a session onto the same card from the terminal cog.**
  The session exits and resumes its conversation, so it picks up new defaults without losing context. Docs:
  `docs/reload-design.md`. Landed 2026-09-18 (`8f12db5`, `b1f009e`).

- **Ending a session puts you back on the terminal you were on.**
  Only a real restart shows the restart banner. Docs: `CHANGELOG.md`. Landed 2026-09-11 (`72628a8`).

- **Each terminal remembers its settings per browser, and text scales on its own.**
  Terminal themes are chosen per card. Docs: `CHANGELOG.md`. Themes landed 2026-09-03 (`88a5f0f`), per-instance
  settings 2026-09-09 (`59aab32`), text scaling 2026-09-09 (`64121e5`).

- **You dismiss a terminated terminal from its row menu.**
  Docs: none. Landed 2026-09-19 (`8bda556`).

- **A terminal row shows a pulsing mark while it holds a peer message, and a room badge.**
  The mark's tooltip leads with sender and age. Each card wears a badge in a stable per-room colour. Docs:
  `docs/agent-messaging.md`. Landed 2026-09-21 (`004b77c`, `b8e1c92`, `875862f`).

- **Paths and URLs in terminal output are links.**
  A path that is a real file in the card's directory opens in atrium's editor in your browser. A directory opens
  the file drawer. A URL opens a new tab. Selecting text does not open anything. Docs: `docs/user-guide.md`
  pattern 10. Landed 2026-09-07 (`acec34c`).

- **Keystrokes stay smooth on a busy Windows machine.**
  The hub and the room run at above-normal priority. `ATRIUM_PRIORITY=normal` turns it off. Docs:
  `docs/input-lag-logging.md`. Landed 2026-09-22 (`22a5264`).

- **Typed keystrokes can be shown in several panes at once (display only).**
  The room fans keystrokes out to other attached panes for display. The commit that added it marks it parked.
  Docs: `docs/multi-pane-input-design.md`. Landed 2026-09-20 (`c1f541e`).

### Terminals: removed, and why

- **The collapsing tree renderer.** It folded a chain of only children, and later a level holding one row, into a
  single heading. Both hid levels unevenly, so every level keeps its heading now. Removed 2026-09-11.
- **The attach preamble and the width-mismatch note.** Both printed into the terminal on every reattach. Removed
  2026-09-19 (`3a6abd8`) and 2026-09-20 (`cfb383a`).
- **The model tickbox on the launch form.** An empty model field already means the runner's default. Removed
  2026-09-14 (`93f6e59`).

## Permissions and auto mode

- **Every tool call an agent wants to make goes through a gate you answer.**
  A PreToolUse hook blocks until you approve or block, and a block hands your reason back to the agent. Every
  request says which agent is asking. A hook never fails a session: the permission hook fails open when atrium is
  down. Docs: `README.md`, `docs/hooks.md`. Landed 2026-09-01 (`fdc5c8e`).

- **A pending edit shows a real diff.**
  Unchanged context is dimmed and changed words are picked out. Docs: `README.md`. Landed 2026-09-01 (`c51ae30`).

- **You answer once with always or never, and matching requests are answered from then on.**
  A rule is a command prefix, a glob, or a folder. A folder rule covers work inside a directory, including relative
  commands. The most specific match wins. Docs: `README.md`. Landed 2026-09-01 (`fdc5c8e`), folders on 2026-09-02
  (`a85e2a3`).

- **You import the allow and deny lists Claude Code already has.**
  The import previews what it would add and reports anything it cannot map. Docs: `README.md`. Landed 2026-09-01
  (`fdc5c8e`).

- **Auto mode approves without asking and still records everything.**
  Turn it on per card, for the whole board, or for the next hour. It never overrides a never rule or a shelved
  card. `what did it do?` reads the record back grouped by tool, with unseen decisions first. Board-wide auto is
  held and enforced by the hub. Docs: `docs/auto-mode.md`. Landed 2026-09-02 (`a85e2a3`), for an hour on
  2026-09-03 (`e11e950`), hub-held on 2026-09-19 (`a7551af`).

- **Shelving a card is a standing no, and answers what it had pending.**
  Docs: `docs/how-atrium-works.md`. Landed 2026-09-01 (`b054a57`).

- **Replayed decisions are bounded and recorded, and requests from a vanished session close out.**
  Docs: `docs/how-atrium-works.md`. Landed 2026-09-03 (`b92bc30`, `1547ff4`).

- **Every session on a machine can be gated without wiring anything into the agent.**
  `ATRIUM_PERM_GATE=on` gates every session. The runners tab writes the missing hooks into Claude Code's settings,
  and preserves sibling hooks and symlinks. Codex hooks are a second target. Docs: `docs/hooks.md`. Landed
  2026-09-02 (`7eb7f0f`), codex on 2026-09-03 (`5469fa9`).

- **You put a running session on the board, or take it off, without a restart.**
  `atrium join` and `atrium leave`. Docs: `docs/how-atrium-works.md`. Landed 2026-09-01 (`b054a57`).

### Permissions and auto mode: removed, and why

- None recorded.

## Messages, peers and agent commands

- **You queue a message to a running session.**
  It is typed into the terminal when atrium owns it and your line is empty, or carried by the next hook when it
  does not. It arrives framed as a message from you. Saying anything to a card answers its open question. Docs:
  `docs/agent-messaging.md`. Landed 2026-09-03 (`39a8461`).

- **You write named actions once and press them on any card.**
  An action is a stored prompt, optionally limited to a tag or a runner, and can ask the runner to quit afterwards.
  `write it up and finish` is seeded. Docs: `docs/user-guide.md` pattern 9. Landed 2026-09-03 (`f3b7fa6`).

- **An agent says it finished, and what it did.**
  `atrium finish [recap]` moves the card to done with the recap. `--hand-back` moves it to ready instead. Docs:
  `docs/user-guide.md` pattern 8. Landed 2026-09-03 (`4a246d7`).

- **An agent says it is stuck, and what would unstick it.**
  `atrium ask` puts the question on the card. `--continue` says the session is carrying on. You can take a
  question off a card without telling the session. Docs: `docs/user-guide.md` pattern 12. Landed 2026-09-07
  (`8866594`), take-off on 2026-09-09 (`64121e5`).

- **Sessions find each other and talk through atrium.**
  `atrium peers`, `atrium tell`, `atrium ask --peer` and `atrium answer`. A message is typed when the target
  terminal is free, and queued and retried from two seconds out to four hours when it is not. Peer messages are
  capped at 8000 characters and 20 a minute. Docs: `docs/agent-messaging.md`. Landed 2026-09-06 (`cc6c921`), ask
  and answer on 2026-09-07 (`8866594`), retry and typing on 2026-09-22 (`2735470`).

- **An atrium has a name, so wire names from two machines cannot collide.**
  `atrium name` prefixes every wire name. A launched runner gets a unique wire name from its title. Docs:
  `docs/how-atrium-works.md`. Landed 2026-09-03 (`da73670`), unique names on 2026-09-21 (`6d5df49`).

- **Sessions reach atrium through one control MCP server on the hub.**
  `/_hub/mcp` serves `atrium_status`, `atrium_peers`, `atrium_say`, `atrium_task`, `atrium_exit`, `atrium_launch`
  and `restart_atrium`. `atrium_say` carries the caller's identity. `atrium_launch` can write a `BRIEF.md`, set a
  terminal theme, and is capped at 10 concurrent agent-launched sessions. Loopback only. Docs:
  `docs/agent-messaging.md`, `docs/reload-design.md`. Landed 2026-09-18 (`d192062`, `85204e5`, `09b3fee`), cap
  on 2026-09-19 (`da0866b`).

- **An agent asks whether the human has read its last turn and answered its questions.**
  `atrium_task` with no card answers about the caller's own card, and its `seen` block says whether the last turn
  is unseen and which of its Open Questions are still open. `atrium_peers` counts unseen turns and open questions
  per peer. Docs: `docs/seen-design.md`. Landed 2026-09-23 (`d03762a`). **claude/main only**.

### Messages, peers and agent commands: removed, and why

- **A per-session stdio control MCP child.** Every session spawned its own `atrium-control` process, about 24MB
  each. The HTTP server on the hub replaced it on 2026-09-18 (`d192062`). `atrium control` still builds.

## Rooms and the hub

- **A hub serves the board and rooms run the agents.**
  `atrium2 hub` serves the board and proxies to rooms, and holds no card state, so it restarts without costing a
  session. `atrium2 room` owns the database, the ptys and the agents. A room dials the hub over mutual TLS after a
  one-string join. Docs: `docs/hub-room-plan.md`, `docs/how-atrium-works.md`. Landed 2026-09-17 (`7dde248`).

- **One board shows every room, and a room picker scopes it.**
  Lists, the event stream and history merge across rooms. The picker shows one line a room with a state dot,
  updates live, and lists disconnected rooms dimmed. Each room has a settings cog. Docs:
  `docs/hub-room-requirements.md`. Landed 2026-09-17 (`44f65ff`, `7c175ab`), picker on 2026-09-19 (`53fc643`).

- **The hub names its rooms, and adding a room writes it down.**
  `atrium2 hub room add|ls|token|mark|rm|log`. The join string is bound to one name, and the room reads its name
  from the certificate the hub signed. A room the hub has no record of cannot attach. Docs: `docs/decisions.md` 18.
  Landed 2026-09-17 (`d49d69d`, `153078c`).

- **The rooms tab lists every room, connected or not.**
  Rooms are grouped as here now, not answering, and never connected, each with a transport badge. The header counts
  `1/2 rooms`. The board cannot mint a join string. Docs: `CHANGELOG.md`. Landed 2026-09-17 (`cde1028`).

- **A room tells the hub what it holds, and the hub shows it while the room is offline.**
  The room pushes its stored rows on change. The hub reads that cache only when the room is not answering. Docs:
  `CHANGELOG.md`. Landed 2026-09-17 (`c004557`).

- **Deleting a room is mark, clear, confirm, stop, remove.**
  A marked room starts no new work. The room itself confirms it holds nothing. `--force` removes only the hub's
  record. A stale room can be forgotten from the picker. Docs: `CHANGELOG.md`. Landed 2026-09-17 (`0dcefac`),
  forget on 2026-09-21 (`3414839`, `0cdc0b9`).

- **The hub snapshots its store and restores it in one command.**
  Snapshots every ten minutes, kept in tiers over a month. `atrium2 hub backups` lists them and `atrium2 hub
  restore` moves the current store aside rather than deleting it. Docs: `CHANGELOG.md`. Landed 2026-09-17
  (`4dc0e03`).

- **The board and the room link bind separately.**
  The board (`--addr`) is loopback only. The room link (`--link`) may bind wide, with `--link-advertise` naming the
  address in join tokens. Docs: `docs/hub-room-plan.md`. Landed 2026-09-18 (`48313e7`).

- **A room joins over direct mTLS, a private zrok share, or OpenZiti.**
  `atrium2 join` takes a transport flag and its material. Docs: `docs/ziti-zrok-flow-design.md`. Landed
  2026-09-17 (`97d4110`), join flags on 2026-09-19 (`6e978be`).

- **A hub offers builds, and a room decides whether to take one.**
  The hub offers a build per platform. A room started with `--accept-upgrades` installs it. Docs: `CHANGELOG.md`.
  Landed 2026-09-17 (`0054ec2`, `686d46f`).

- **A second room on one machine keeps off that machine's hooks.**
  `--isolated` keeps the room's address in a private file. Docs: `CHANGELOG.md`. Landed 2026-09-18 (`22b08de`).

- **A room's settings are behind that room's cog.**
  Editor, paste directory, browse roots, shell, scrollback and similar settings open already scoped to one room.
  Docs: `CHANGELOG.md`. Landed 2026-09-17 (`3a7d1d8`).

- **The launch dialog offers only the runners a room has.**
  Each room holds its own runner binary path. Docs: `docs/runner-scoping-design.md`. Landed 2026-09-21
  (`be6a6ab`, `9a34352`).

- **You queue work for another machine.**
  `atrium dispatch to <room>`, `list` and `cancel`. The room claims it on its check-in and launches on its own
  daemon. Docs: `docs/remote-launch.md`. Landed 2026-09-07 (`8866594`).

### Rooms and the hub: removed, and why

- **The hub as its own room.** Dropped 2026-09-18 (`c4248d7`). An old hub drops the defunct row on startup.
- **Anonymous joins.** `atrium2 hub token` and `--name` on the room went on 2026-09-17 (`153078c`), because a join
  string authorised a join as any name.
- **The `settings -> this machine` pane.** Its nine settings moved behind each room's cog on 2026-09-17 (`3a7d1d8`).
- **Heartbeat federation.** `atrium room` reporting to `/v1/rooms` every twenty seconds (2026-09-06, `c78ff42`) is
  the older mechanism. The hub and room link supersedes it. Docs: `docs/federation-design-v2.md`.

## Overlays and sharing

- **Atrium serves the board on a zrok share or an OpenZiti service.**
  The SDK hands back a listener and the board is one handler on it. Nothing is proxied and atrium holds no
  identity. A failed share never takes the local board down. Docs: `docs/overlays.md`,
  `docs/ziti-zrok-flow-design.md`. Landed 2026-09-03 (`b716356`), hub board share on 2026-09-18 (`a7f397b`),
  `--board-transport ziti` on 2026-09-19 (`611846a`).

- **You lend one session to one person.**
  `share this session` serves that terminal on its own address through an allowlist handler that refuses
  everything else, including shells. Read-only is enforced on the socket. Docs: `docs/overlays.md`. Landed
  2026-09-04 (`5af35c2`).

- **Shares survive a restart and can reserve their own address.**
  Reserve a zrok name from the board. Atrium keeps its own zrok environment. The panel shows the share's name as
  your zrok account calls it. Docs: `docs/overlays.md`, `docs/zrok-share-500.md`. Landed 2026-09-03 (`fcb13e7`),
  survival on 2026-09-06 (`cc6c921`, `c78ff42`).

- **You revoke a public share from the board.**
  The header pill and the card menu both stop a share, whether or not zrok is up. The `shared` chip goes straight
  to the stop confirmation on right click. Docs: `CHANGELOG.md`. Landed 2026-09-11 (`d5413a3`).

- **A published board asks for a login.**
  A name and password, or an OIDC provider, or both, with the password checked first. It guards only the published
  handler, never loopback. A public zrok board share is refused without a login. The login settings sit under the
  panel that publishes the board. Docs: `docs/overlays.md`. OIDC landed 2026-09-06 (`c78ff42`), password
  2026-09-11 (`fd00c11`), public-share login on 2026-09-19 and 2026-09-20 (`1f6bb6c`, `ad1aa90`).

- **Three trial surfaces for exposing the board sit side by side.**
  The `expose the board` redo ships three coexisting surfaces to compare, before one is chosen. Docs:
  `docs/ziti-zrok-flow-design.md`, `docs/test-plan.md`. Landed 2026-09-20 (`e27ca9f`).

### Overlays and sharing: removed, and why

- **The standalone `who may open it` pane.** Folded under the panel that publishes the board on 2026-09-16
  (`704b2e9`), because it only applies to the published board.

## Runners and runner setup

- **A runner is configuration, not code.**
  claude, codex, ollama, a shell or anything you add: a command, arguments, a directory, an environment, resume
  arguments and exit keys. Runners whose command is not on the machine are left out of launch menus. Docs:
  `README.md`, `docs/other-runners.md`, `docs/atrium-for-agents.md`. Landed 2026-09-01 (`c51ae30`), non-claude
  runners on 2026-09-07 (`6634742`).

- **You start an agent from the launch dialog, a card's menu, a URL or the CLI.**
  `new agent here` on a card starts a runner in that card's directory with nothing asked. `atrium launch` takes
  tags, a prompt, a source and a model. Docs: `docs/user-guide.md`. Landed 2026-09-01 (`c51ae30`), card menu on
  2026-09-14 (`d01a9ff`), launch metadata on 2026-09-03 (`dc12f26`).

- **A launch names a model, once.**
  The card remembers the model across restarts and the runner's row does not change. A runner with no
  `model_args` refuses the launch. Docs: `CHANGELOG.md`. Landed 2026-09-11 (`9dfd9b0`).

- **You pick which claude conversation to resume.**
  A fixture can take the latest. Atrium refuses to start a second runner on a conversation another one holds.
  Resume works from the terminal row menu, and the resume dialog offers terminate and remove. Docs:
  `CHANGELOG.md`. Landed 2026-09-04 (`7cf9616`, `c248274`), row menu 2026-09-16 (`704b2e9`), dialog 2026-09-22
  (`5d093ce`).

- **A restart reopens what was open.**
  Cards that had a runner come back and resume their conversations. Alerts stay quiet until every card named has
  arrived, and permission requests still ring. Docs: `docs/reload-design.md`. Landed 2026-09-08 (`45dffb3`),
  quiesce on 2026-09-15 (`2017744`).

- **Fixtures are sessions atrium keeps running, and you switch them on and off.**
  Docs: `CHANGELOG.md`. Landed 2026-09-03 (`88a5f0f`), toggle on 2026-09-18 (`2224852`).

- **A throwaway session deletes its directory, card and conversation when it ends.**
  `keep this work` moves the directory somewhere real and makes the card permanent. Docs: `CHANGELOG.md`. Landed
  2026-09-14 (`93f6e59`).

- **Launched claude sessions do not stop on an MCP server prompt.**
  The seeded claude row passes `--strict-mcp-config`. Docs: `CHANGELOG.md`. Landed 2026-09-12 (`95e9e99`).

- **Atrium checks for a newer runner just before starting one, without running it.**
  It reads the installed `package.json` and asks the registry once. A launch you pressed waits up to four seconds.
  Docs: `CHANGELOG.md`. Landed 2026-09-15 (`2017744`).

- **A runner's row says what stops it working, and fixes what atrium may fix.**
  A `setup` chip opens named checks with `fix` buttons or commands to copy. Gemini checks folder trust and
  sign-in, and a gemini card in a new folder is trusted before it starts. Claude checks sign-in and hooks. Every
  edit keeps backups. Docs: `docs/runner-setup-design.md`. Landed 2026-09-23 (`87f548a`, `dba9768`). **claude/main
  only**.

- **Every on/off row on the rooms page switches from its own pill.**
  Runners, fixtures, sources, providers, recognisers, actions and rooms all use one `role=switch` pill. Docs:
  `CHANGELOG.md`. Landed 2026-09-23 (`04ac808`). **claude/main only**.

- **You review a card's branch with one of your personas, and review what they learned.**
  The runners page lists the persona pack named by `persona_pack_path`. `review with…` on a card starts a fresh
  claude session as that persona (`claude --agent`) in a run directory of its own, with a `TARGET.md` naming the
  diff range and the repo's knowledge and memory. It reports back to the card. The `lessons` view shows memory
  changed since the last `Lessons-reviewed: <id>` commit, and its promote, keep and delete buttons edit the
  dotagents working tree only. The commit is yours. Docs: `docs/personas-design.md`. Landed 2026-09-23
  (`f34dd0c`, `7d93f3b`). **claude/persona-atrium only**.

### Runners and runner setup: removed, and why

- **`scripts/sources/runner-updates.ps1`.** It started each runner binary on a ten-minute timer to read its
  version and raised a card per release. Replaced by the in-process check on 2026-09-15 (`2017744`).
- **The separate enable button on rooms-page rows.** Replaced by the on/off pill on 2026-09-23 (`04ac808`).

## Intake, sources and providers

- **An inbox holds work atrium did not start.**
  A card with no runner behind it. `start` opens the launch dialog prefilled and starts onto that card. Docs:
  `docs/intake-design.md`. Landed 2026-09-03 (`1f8c8d8`).

- **A source is a command on a timer that posts work items.**
  Atrium holds an argv and an interval, never a credential. A source that fails three times in a row switches off
  with the reason. An item that moves on rewrites its card while it is still in the inbox. Docs:
  `docs/intake-design.md`, `scripts/sources/`. Landed 2026-09-03 (`9028a32`).

- **A URL fills in the launch dialog.**
  Recogniser rows turn a pasted URL into launch fields, and `atrium open <url>` does the same from a shell. Docs:
  `docs/scm-design.md`. Landed 2026-09-07 (`6634742`).

- **Providers tell atrium where your repositories live.**
  A provider is a name, a root and a layout, and defining one adopts every checkout under it. Presence on disk is
  worked out fresh, so an unplugged drive greys rows and deletes nothing. Worktree support runs `git worktree add`
  with no shell. Docs: `docs/providers-design.md`, `docs/test-plan-z-providers.md`. Landed 2026-09-17
  (`0f47f96`), projects dropped on 2026-09-22 (`17fdffa`).

### Intake, sources and providers: removed, and why

- **The `projects` button, `worktree_command` and `project_scan_depth`.** Added 2026-09-14 (`93f6e59`). It scanned
  for `.git` two levels down and ran a shell template to make a worktree. Providers replaced it on 2026-09-22
  (`17fdffa`), because it guessed a layout and ran a command atrium could not see inside.

## Files

- **You paste or drop files into a session.**
  The bytes land in `.atrium/incoming` under the card's directory and the path is typed without Enter. Docs:
  `docs/file-transfer-design.md`. Landed 2026-09-03 (`6b6393c`).

- **You browse a session's directory and take files out, one at a time or as a zip.**
  Everything resolves through one containment check against the card's directory. Anything outside answers 403.
  Docs: `docs/file-transfer-design.md`. Landed 2026-09-04 (`c248274`, `304fbe0`).

- **You read and edit a file in atrium's own editor in the browser.**
  Escape closes the editor, then the drawer. Docs: `docs/user-guide.md` pattern 10. Landed 2026-09-07
  (`acec34c`).

- **You open a file in an editor, or a directory in a terminal window, on atrium's machine.**
  `editor_command` and `terminal_command` are off until set, never run through a shell, and resolve paths through
  `internal/safepath`. The window appears where the daemon runs. Docs: `CHANGELOG.md`. Editor landed 2026-09-04
  (`c248274`), terminal window 2026-09-11 (`f690f32`).

- **The directory picker is bounded to configured roots and completes paths with Tab.**
  Docs: `docs/status.md`. Landed 2026-09-05 (`99e2d8e`), Tab completion on 2026-09-06 (`c78ff42`).

### Files: removed, and why

- None recorded.

## History, audit and notifications

- **Every card ever run stays searchable.**
  The history tab lists every card, cut by recap, filterable and exportable as JSON or CSV. Docs: `README.md`.
  Landed 2026-09-03 (`db2cf2c`).

- **Every permission decision records who asked and who answered.**
  You, a rule, or auto mode. The decision log sits in the permissions pane. Docs: `docs/auto-mode.md`. Landed
  2026-09-02 (`7eb7f0f`).

- **An audit tab shows hub and room events.**
  Rooms attaching and dropping, launches refused by the cap, permissions, and session start and exit, with a
  filter. Docs: `docs/audit-design.md`. Landed 2026-09-19 (`22cee75`, `4a703b0`).

- **Desktop notifications carry approve and block buttons.**
  One alert per event: the focused atrium window toasts, and with no window focused the operating system notifies
  once. Sounds are per card. Alerts name the agent and its state. Docs: `README.md`. Landed 2026-09-03
  (`5604978`, `3c3b05c`), one alert on 2026-09-16 (`7653794`).

- **A bell keeps everything the board told you.**
  The last 200 notifications, newest first, repeats folded, with a copy icon per row. Held in the browser. Clear
  empties and closes it. Docs: `CHANGELOG.md`. Landed 2026-09-14 (`38572ba`), every path logged on 2026-09-21
  (`860f807`).

- **Event history can be bounded and sent to a cold file sink.**
  `event_sink` names `db`, `file` or both. An opt-in per-card window rolls old events off the database, and the
  feed reports `rolled_off`. Databases give freed space back to disk. Docs: `CHANGELOG.md`. Landed 2026-09-18
  (`8bf4083`, `1959d8a`, `ede2f4b`, `739948a`).

- **The header nags when the persona pack is not committed or not pushed.**
  Set `persona_pack_path` in the gear. The room reads git state there once a minute, read only, and the chip
  names the personas changed, rings on the stuck-agent backoff, and can be snoozed. Docs:
  `docs/personas-design.md`. Landed 2026-09-23, **claude/main only**.

### History, audit and notifications: removed, and why

- None recorded.

## Settings and skins

- **You pick one of twenty board skins, and import terminal themes.**
  The skin follows the room picker's scope: ALL is the hub's, each room holds its own. Docs: `CHANGELOG.md`.
  Landed 2026-09-05 (`c34e9a2`), theme import 2026-09-07 (`317fc04`), per-scope skin 2026-09-19 (`6b4ebed`).

- **You back up and restore atrium's configuration.**
  `GET /v1/config/export` and `POST /v1/config/import`, dry run by default, and `back it up` in the settings cog.
  Docs: `docs/status.md`. Landed 2026-09-06 (`c78ff42`).

- **A shared address file lets hooks and scripts find the daemon.**
  `shared_location` names a directory both accounts can read. Docs: `README.md`. Landed 2026-09-06 (`c78ff42`).

### Settings and skins: removed, and why

- None recorded.

## Packaging, install and operations

- **You install atrium from a deb, rpm, macOS pkg or Windows MSI.**
  Each has a per-user path that needs no admin rights. `make release` builds five platforms. Docs:
  `docs/packaging.md`. Release landed 2026-09-06 (`ab5afe6`), packages on 2026-09-19 (`7560db7`).

- **The daemon starts at logon.**
  `scripts/atrium-autostart.ps1` registers a logon task, not a service, because a service cannot open a pty you
  can attach to. Docs: `docs/user-guide.md` pattern 0. Landed 2026-09-04 (`c248274`).

- **A session restarts atrium from inside atrium.**
  `restart_atrium` parks the other agents, spawns a detached restarter and comes back as the same room. It installs
  a staged binary on the way. Duplicate asks are dropped. Docs: `docs/reload-design.md`. Landed 2026-09-04
  (`3d89e69`, `82ba917`), room-side restart on 2026-09-18 (`c5ccfc3`), same-room restart on 2026-09-22
  (`5390365`).

- **`atrium stop` winds the daemon down instead of killing it.**
  Event streams close first and runners get ten seconds. Docs: `README.md`. Landed 2026-09-01 (`b054a57`).

- **A preview board shows a change on a copy of your cards and acts on nothing.**
  `atrium preview --from live` runs a second daemon that takes no hooks and starts passive. Docs:
  `docs/preview-design.md`. Landed 2026-09-06 (`2985923`).

- **Atrium warns when it opens a different database than last time.**
  Docs: `README.md`. Landed 2026-09-04 (`c248274`).

- **Storage failure halts rather than degrading.**
  The agent listener closes and stays closed, and the board stays up to say what broke. Docs:
  `docs/how-atrium-works.md`. Landed 2026-09-01 (`fdc5c8e`).

### Packaging, install and operations: removed, and why

- **`atrium install`.** Removed before shipping, because copying a file is the shallow half of installing. The
  packages replace it. Docs: `docs/status.md`, `docs/packaging.md`.
- **The v1 `start-atrium.ps1`.** Retired to a stub on 2026-09-19 (`ee7a589`).

## Diagnostics

- **You save a terminal trace from its cog.**
  Each terminal keeps its last 64KB of raw attach traffic. `node scripts/replay-term-trace.js` replays it. Docs:
  `CHANGELOG.md`. Landed 2026-09-22 (`02625fa`).

- **`atrium replay` renders a captured terminal stream the way an attach would.**
  Streams come from `ATRIUM_TAP_DIR`, `/v1/tasks/{id}/scrollback/raw`, or `/scrollback/text` in a browser tab.
  Docs: `CHANGELOG.md`. Landed 2026-09-14 (`53b728c`, `c6b9eda`).

- **One checkbox times the whole keystroke path.**
  `log terminal input lag` switches the browser, the hub and the room live. `ATRIUM_DEBUG_INPUTLAG` wins for the
  life of the process. Docs: `docs/input-lag-logging.md`. Landed 2026-09-22 (`97f68a4`, `fd471ca`).

### Diagnostics: removed, and why

- None recorded.

## The older v1 modes

- **A terminal hub and an MCP agent loop.**
  `atrium hub` is a terminal UI you type into, and `atrium agent` is the MCP server a session calls `submit` on in
  a loop. This `hub` is not the `atrium2 hub`. Docs: `docs/user-guide.md` patterns 1 to 6. Landed 2026-06-04
  (`fc4ccd0`) or earlier.

- **A read-only state aggregator.**
  `atrium serve`, `status` and `watch` read an external worktree ledger. Docs: `README.md`. Landed with the v1
  scaffold, commit not found.

### The older v1 modes: removed, and why

- None recorded.
