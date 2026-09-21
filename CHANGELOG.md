# Changelog

A running log of what's been built. Newest first. No formal version cuts yet (everything is `v0.0.0-dev`); each
section heading is just "what landed in this iteration."

## Unreleased

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
    `github` followed by `openziti` paid for both and left a band of empty strip between two lines that belong
    together.
  - **The switcher has its own surface.** `body::before` lays a 60 pixel grid over the whole page at a third
    opacity, which is a texture behind the board and reads as a defect behind a list: it draws bands across the
    strip that line up with nothing in it, and the first question anybody asks is what the bands mean. It was
    invisible while the list was wall to wall cards, and group headings leave gaps.

- **Back and forward work on the board.**

  The board is one page that swaps views and attaches terminals, and it kept none of that, so the browser's
  back button either did nothing or left atrium entirely. Both moves are recorded now: switching view, and
  switching session.

  **The address bar is not touched**, and that is the part worth keeping. `#term=<id>` already means something
  here, it is how a popped-out window resolves its card on load, so a view in the hash would make every reload
  of a solo window a negotiation between two meanings of one string. That is why the view went to
  `localStorage` in the first place. `pushState` carries a state object against the same URL, so the entries
  exist and the address does not move. The two restores stay separate and each is right for its job:
  `localStorage` answers "where was I yesterday" across a reload and a daemon restart, history answers "where
  was I a moment ago" inside this visit.

  Three things that are only obvious once they are wrong:

  - **Going back must not record the arrival**, or forward points at where you just came from and the two
    buttons walk in a circle.
  - **Attaching pushes once, not twice.** `openTerm` switches view before the card is set, so the naive
    version left `terms with nothing attached` between the view you came from a