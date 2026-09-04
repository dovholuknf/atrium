# Changelog

A running log of what's been built. Newest first. No formal version cuts yet (everything is `v0.0.0-dev`); each
section heading is just "what landed in this iteration."

## Unreleased

- **An inbox atrium owns and does not fill.** `POST /v1/intake` takes a normalized work item and makes a card
  with no runner. It takes one item or an array of them, because a shell script producing one thing should not
  have to wrap it in brackets and `gh issue list --json` produces an array. One malformed entry in a batch of
  forty does not discard the other thirty nine, since the source that produced it will send the same forty again
  next tick and the operator would never see any of them.

  Atrium does not know what a source is. `github`, `zendesk` and `ci` are strings it renders as a badge. Whoever
  posted the item did the reading, which is the whole reason this can serve a system nobody has thought of yet.

  Deduplication is a key column of its own rather than a constraint over source and identifier, so that
  uniqueness applies to a poller and not to a person: two ticks reporting one ticket are one card, and two
  deliberate launches naming one ticket are two pieces of work somebody asked for twice. The source is lowercased
  and both halves are required, so two scripts spelling it `github` and `GitHub` raise one card. An archived item
  still counts, because a card raised, worked and swept coming back on the next tick is the one failure mode a
  poller has.

  **No new status.** `backlog` has been in the schema since the first migration with nothing ever creating a card
  in it, and an offered item is exactly what that word means. `docs/intake-design.md` had argued for a new
  `offered` status and had enumerated six of the seven that already exist while skipping this one. That would
  have been cosmetic if a status were cheap. It is not: changing a `CHECK` means rebuilding the table, `task` is
  the parent of four `ON DELETE CASCADE` relationships, and `DROP TABLE` fires them. A migration written by
  faithfully following the pattern in `0010` would have deleted every event, permission, message and launch spec
  in the database and would have looked exactly like the two migrations it was copied from. The doc now records
  that, because the near miss is worth more than the entry it replaces.

  Starting an offered card claims the card that already exists rather than making a second one. `Register` cannot
  do it, having no wire name to match and no pid to fall back on, so it would have found nothing, made a new card,
  and left the item sitting in the inbox with its session on a card that had no link to what it was for.

- **A launch can say what the work is and where it came from.** `atrium launch` took a directory, a title and a
  reason. It now also takes `--tags`, `--prompt`, `--source`, `--external` and `--item-url`, which is intake
  layer 0 from `docs/intake-design.md` and the thing every other layer needs first.

  The point is the inversion the backlog already recorded for directories, applied one level up. Atrium does not
  learn what a Zendesk ticket is. Whatever already knows makes the worktree, then hands it over with the
  identifier and a first instruction attached, and the card arrives supervised, gated, tagged and linked back to
  the thing it came from. `external_id` has existed since migration 0005 and was written by nothing; it is now
  what deduplication is keyed on, paired with a source, because `4211` is an issue in one tracker and a ticket in
  another.

  How a runner takes an opening instruction is per runner, so it is configuration rather than a special case:
  `prompt_args` sits next to `resume_args` on the harness, with `{prompt}` where the text goes. Claude and codex
  take a bare argument. A shell has none, and a launch that hands one a prompt is refused rather than starting a
  session that would try to execute it. A resume and a prompt together are refused too: that conversation already
  has its instruction, and saying something else to it is what the message channel does.

  The prompt does not reach the audit log. The `launched` event records the command line as it was before the
  prompt was appended, plus whether there was one. A seed prompt is longer than the rest of the line put together,
  and for a support case it is somebody else's words in a database that has no encryption and a board that has no
  login.

- **Two designs written down: intake, and moving files.** `docs/intake-design.md` gains the engineering versus
  support split, which is the half of the original ask that had no answer. An engineering item names a repo and
  therefore a directory, so its card can be fully prepared. A support case names a customer and a symptom, names
  no repo, and can only be offered until somebody reads it, which turns out to be the strongest argument for the
  `offered` status the inbox needs. It also carries somebody else's data, so the rule for a support source is to
  carry the identifier and the URL and as little prose as gets the job done.

  `docs/file-transfer-design.md` is new, and most of what it bought was finding out that the obvious shortcut is
  closed. `docs/charon.md` said to derive a safe upload path from the answer `browse.go` already has. There is no
  such answer: `browse.go` applies `filepath.Clean` to caller input and nothing else, with no root, no allow list
  and no symlink resolution. So the first piece of work is a containment primitive that does not exist yet, and
  the first version of upload takes no caller-supplied path at all. The same reading turned up a gap now recorded
  in the backlog: a share publishes that unbounded listing.

- **Saying something to a session, from the board.** The endpoint has existed for a while and only curl could
  reach it. There is now a box on the card, and it reports which of the two routes the message took, because
  they are different promises: typed into the terminal means it has already landed, and queued means it has not
  and will not until the session makes its next tool call or ends its turn. That can be minutes, so anything
  still waiting is listed under the box with its age. One button doing two very different things in silence is
  how a message ends up sent four times. Enter sends and shift-enter is a newline, the chat convention, since
  this is one.

- **Reserving a zrok address, and a correction that took reading zrok's source to find.**
  `sdk.ShareRequest.Reserved` is read by nothing in zrok. The field is on the struct, no code consumes it, and
  atrium had been setting it to no effect. In v2 reserving moved off the share and onto the NAME: it is
  `create name` followed by `modify name --reserved`, and `controller/unshare.go` consults it, keeping a
  reserved name when a share is unshared and deleting an ephemeral one.

  That means the share must still be released on stop, which is what frees the ziti resources, and the name is
  what survives. A private share reaches the same place by a different route: its token is requested rather
  than owned, so releasing it puts the token back for the next start to ask for again.

  The board reserves a name in one press, doing both steps every time, because a name that exists but is
  ephemeral fails exactly like one that was never created. It takes `name` or `namespace/name` and writes back
  the fully qualified answer, so the next start does not depend on a default namespace staying put.

- **A ziti identity can be asked what it may host.** Configuring the overlay means typing a service name into a
  box, and whether that service exists and whether this identity may BIND rather than only dial it are both
  facts on the controller. Both failures reach the board identically, as the listener refusing, so "is this
  going to work" was only answerable by pressing start. The service field now asks, lists what came back with
  the bindable ones first and clickable, and marks a dial-only service as such rather than hiding it, since
  that is the mistake that reads as "no such service". Read-only, deliberately: creating services and policies
  stays out of scope, and reporting what a network already says is the other side of that line.

- **A missing executable no longer hides an overlay that works.** Neither overlay shares through a child
  process: both are embedded SDKs answering their own listener. The executable is used by `zrok enable` and
  `zrok disable` and nothing else. Treating it as absent-means-unusable meant a machine that was already
  enabled saw "not installed" and a download link instead of a start button. It now says what is true, and
  disables only the one button that really does need the executable.

- **zrok can be pointed at another instance,** written before enabling rather than after, since enabling talks
  to whatever it names. Changing it on an already-enabled machine is refused with the order to do it in, rather
  than leaving a token from one instance being sent to another and failing as though the token were bad.

- **The overlay configuration is an accordion,** open while there is something to do in it and folded once the
  overlay is ready, at which point it is settings rather than steps. Held per overlay, so opening one to change
  a field and coming back does not fold it mid-edit.

- **Every card can have its own bell, and alerts can be held briefly so a burst is one alert.** A tone is
  chosen on the card and stored there, like its theme and for the same reason: knowing which agent wants you
  without looking only works if the answer is the same tomorrow and in another browser. It rings for both
  kinds of alert that card raises, which trades away telling a permission from a ready by ear. That is the
  right way round, because which agent is the fact you cannot recover with your back turned. Picked and heard
  in the same place, since choosing a bell you will not hear until the next time that agent wants you is
  choosing blind. A permission carries the asking card's tone from the server, which was already joining that
  row for the agent's name.

  The hold is off by default. A delay between something needing you and being told is a real cost, and it is
  only worth paying once the pile-up is worse than the wait. When it is on, a later arrival extends the window
  rather than opening a second one, capped at three times the setting so a steady trickle still gets
  announced rather than postponed forever. Permissions and ready cards are never merged into one alert, and a
  held pile names who rather than only counting them.

- **A board card is one line, and its chips say the state they are in.** The runner is the mark the stack
  already uses rather than the word `claude`, which was the widest chip on the card and said nothing in a
  column of claude sessions. Two rows became one, so a column of eight fits on screen and seeing what a column
  holds no longer takes scrolling, which is the one thing a column exists to save you.

  The duration chip was three kinds of wrong at once. A ready card read `idle 30m` beside `waiting 31m`, the
  same minute off two clocks that start together, and `waiting` was a word the column had stopped using. A dead
  card read `dead` beside `dead 1h`. And a card in the running column read `idle`, the one column where that is
  a different claim from the column it is sitting in. One chip now: the state, in the state's own words, and
  how long it has been that way. A waiting card times how long it has waited, everything else times idle.

- **An alert says who and what, and the operating system is only used when you are not looking.** Three things
  were wrong with "atrium is waiting on you". It named a card and then said the one thing true of everything in
  that list, so an agent frozen mid-tool and one that had simply finished its turn read identically. And the
  desktop notification never arrived, because the rule for suppressing it was `visibilityState === "visible"`,
  which only means the tab is the active tab of a window that is not minimised. That stays true with the browser
  buried three windows deep, which is exactly when a notification is the entire point. Foreground now means
  visible AND focused, and the split is the obvious one: looking at the board gets a toast, not looking at it
  gets the notification.

  An alert now reads `dotfiles is ready` or `zendesk-16116 needs permission`, with the tool and command
  underneath, and several at once say which kind rather than "3 things need you". Two further things fell out
  of naming them. A blocked agent was ringing twice, because the waiting list contains permission cards and the
  permission check alerts on the same event with more to say, so the waiting alert now leaves those alone, and
  the tab title stops counting them twice. And every notification shared one tag, which replaces rather than
  stacks, so two agents finishing within a few seconds of each other showed one name and the other went by
  unseen. One tag per subject now, carried into the service worker so an alert can be taken down once its
  subject is answered rather than only when it was a permission.

  Nothing alerts for a session that is working. It never did: `/v1/waiting` only ever returns `needs-input` and
  `needs-permission`.

- **`needs input` is called `ready`, and an empty column gives its width back.** The gwt session ledger has
  called the end of a turn `done` for far longer than this board has existed, and the board called the same
  moment `needs input`, so the two disagreed in vocabulary while agreeing to the second on when it happened.
  `done` was not available here, because a board also has to say "this work is finished, stop showing it to
  me", which is a claim only a human makes and which a ledger tracking sessions has no word for. `ready` is
  the third word that means what both do. The stored status is untouched: it is in CHECK constraints, in the
  rules and in every card's history, and one table now decides what a human reads so the column heading, the
  stack chip, the terminal switcher and the card dialog cannot drift into calling one state three things.

  Separately, an empty column was still claiming an equal share of the width. On a normal morning three of the
  five are empty, which left the two being read with a third of the screen between them. An empty column now
  shrinks to its heading, which tightens rather than truncating, and keeps the heading because that is also
  where a card is dropped.

- **The Stop hook, as `atrium turn --event end`.** It existed as a script in somebody's dotfiles, holding a path
  only their machine had, and was never registered, so a message queued for an idle session sat in the queue.
  That is the case the queue exists for: a busy session makes tool calls constantly and a message rides the next
  one, while an idle session makes none at all, which is exactly when you most want to reach it.

  It is the only atrium hook that writes to stdout, because that is how a Stop hook says anything, and therefore
  the only one that can change what a session does rather than just reporting. So it is the only hook marked
  optional: offered by name, with what it does said next to the switch, and never written by "install all". A
  hook that is off on purpose no longer counts as missing, though a stale one still does, since somebody asked
  for that and it is now pointing at the wrong binary. Three things hold the line, and there is a test for each:
  every failure path prints a plain continue and exits 0, `stop_hook_active` is honoured so it cannot block
  twice in a row, and the daemon's answer is passed through only when it parses as a block with something to
  say.

  The board's message box stops promising what it cannot deliver. It said a queued message arrives "when its
  turn ends", which was false on every machine, and it now says that only when the hook is actually wired.

- **A message delivery is never replayed.** A queued message rides the next tool call by refusing it, and the
  banner tells the model the call was interrupted rather than judged, and to retry. The retry is the same
  command with the same dedup key, so it landed on the replay path and was handed back the identical
  already-delivered message, carrying the identical instruction to retry. The model cannot get out of that, and
  the message it kept being shown had been delivered once and marked delivered. A block recorded as `message`
  is now excluded from replay: it was a courier and not an answer, so the retry is asked properly.

- **Turning auto mode on empties the queue it was turned on because of.** The permission chain runs once per
  request, when the request arrives, so anything already waiting had asked before the switch existed and sat
  there under a header saying nothing would stop to ask. Turning global auto on now approves what is already
  queued, through the same decide path the buttons use rather than by writing to the store, since each of those
  agents is parked on an in-memory reply channel and a decision it never sees leaves it blocked forever. The
  chain's order is kept: a shelved card and a never rule both still hold, because those are answers already
  given and auto mode does not discard answers. The count is reported and said out loud in the toast.

- **The board re-reads settings when the event stream reconnects.** A reconnect means the daemon went and came
  back, and everything the tab was holding had been decided by a daemon that is no longer running. The header
  badge claiming to be approving everything while the daemon is in fact asking is the worst way for it to be
  wrong, because the operator stops watching the queue.

- **A question asked for a session that has gone is closed out.** The reaper judges liveness two ways: ask the
  operating system about a pid, or fall back to how long a card has been silent. Neither reaches a card waiting
  on a human with no pid recorded, because waiting is supposed to be silent and marking it dead would discard
  the question. So it sat there, offering a request nobody could answer: the reply channel lives in the daemon
  process and died with the agent's connection.

  Only the hub can settle it, since it is the only thing that knows which pending requests still have somebody
  parked on them. A request the store calls pending and the hub has never heard of is an orphan. It is answered
  with a block so the queue stops offering it, recorded as `the session went away`, and the card moves to dead
  when nothing else is holding it. Three minutes of grace, because the two facts are read at different moments
  and a daemon that just restarted has a store full of pending requests and an empty map until every agent
  reconnects on its own backoff. A card with a pid is left alone: the pid check is a fact where this is an
  inference.

- **A replayed decision is bounded, and recorded.** A dedup key makes a request idempotent so a daemon that
  died between recording a decision and answering it does not ask twice. The key cannot be trusted to identify
  one ATTEMPT, though: the permission hook builds it by hashing the session, the tool and the command, which is
  stable across a retry and equally stable across running the same command tomorrow. So one `block` answered
  once would have replayed against every identical command for the life of that card. Decided requests are now
  replayable for two minutes, far longer than a crash and reconnect and far shorter than the gap between two
  deliberate runs. A still-pending request is exempt, since the agent is blocked on it right now and nothing
  can have gone stale.

  Replays also write an audit event now, marked `replayed an earlier answer`. A replay reaches an agent as a
  real answer, and it was the one path that returned a refusal with nothing anywhere saying it happened.

- **`atrium name` makes wire names unique across machines.** `wire_name` is UNIQUE on the task table and is the
  first thing registration matches, so a collision does not error: it silently hands one session another's
  card, its history and its permission rules. On one machine that cannot happen, because directory names
  cannot collide. Across machines it happens the first time two containers run the same image in the same
  working directory, which is the normal case rather than an unlucky one. Naming an atrium prefixes every
  session it registers, so `atrium` on `sg4` is stored as `sg4/atrium`.

  Immutable once set, and it refuses a change rather than accepting one: accepting would orphan every card
  already registered under the old name. A subcommand rather than a flag on `daemon`, because a flag has to be
  passed on every start and a start that forgot it would register a whole board under the wrong names.
  Qualifying happens inside `Register` and the wire-name lookups rather than at the eight call sites, since
  that is the one boundary where a name off the wire becomes a name in the database. Idempotent, so a session
  that reconnects does not become `sg4/sg4/atrium`. An unnamed atrium is untouched: one machine has nothing to
  collide with, and renaming every card on a board that will never federate is a migration for no benefit. The
  card title drops the prefix, since on a board with one atrium it is the same on every row.

- **Fixtures: terminals that come up with the daemon.** The habit this replaces is opening a terminal, changing
  directory and resuming the same agent every morning. A fixture names a runner, a directory and whether to
  resume, and they start in the order you put them in, so "the dotfiles one is always first" is a thing you can
  say. A plain shell is a runner like any other, which is how "always give me a terminal on this machine" is
  expressed with no special case. Each lands back on the card it used last time, and on its first run it adopts
  a live card already in that directory rather than opening a second one with the same name beside it. A
  fixture's card is pinned without being asked, since a fixture is by definition something worth keeping in
  front of you. Started in the background, because a board that is not answering yet looks like a hang while a
  terminal that is not open yet does not.
- **Terminal themes, ported from the operator's own.** All fifty two Windows Terminal themes, converted rather
  than transcribed, so the colours are the ones already in daily use and a session looks the same in the board
  as it does in a terminal window. The repo-to-theme map came across too, so a session picks up the colour
  already associated with its project. Choosing one on a card overrides that and is stored on the card, not in
  the browser, so it survives a restart and follows the session into another browser.

- **Pinning, on the stack and in the terminal switcher.** Some sessions are permanent fixtures and hunting for
  one in activity order is the wrong shape. A pinned card sorts above everything in either direction, since the
  sort pills are what you asked for now and the pin is what you asked for once and meant permanently. In the
  terminal switcher it stays whether or not it has a terminal: a fixture that vanishes the moment it stops is
  the opposite of one. A pinned card with no terminal offers to resume onto the SAME card rather than starting
  a second one beside it, which would leave the pinned one cold forever while its replacement did the work.
  Dead cards are excluded, because a pinned card that has been swept is not a fixture, it is gone.

- **Tags, and grouping by them.** Grouping read a project out of the worktree path, which answers "what repo"
  and nothing else. A card is also a support case, a tangent, a pull request or a lab, and none of that is in
  the path. Tags are free text, because a fixed list would be atrium deciding what kinds of work exist. Lower
  cased and deduped on the way in, so `Lab` and `lab` cannot become two groups, and the tags already in use are
  offered when editing so the second card spells one the same way as the first. The group control is now
  `by project` / `by tag` / `off`. A card with several tags appears under each, which is the one case where a
  card is in more than one group. `#tag` in the filter means that tag exactly, and clicking a tag on a card
  filters to it.

- **Getting set up is part of the feature now.** Driving a share you had already configured was the easy half.
  The gear reports which of three states each overlay is in, and offers the next thing rather than a button that
  cannot work: not installed, installed but nothing set up, ready. zrok gets an enable button that takes an
  account token, and whether an environment exists is read from `~/.zrok2/environment.json`, the same file the
  CLI checks, rather than parsed out of `zrok status` and its boxed tables. OpenZiti gets an enroll button that
  takes a one-use JWT, reads its claims to refuse an expired one here with a date instead of at a controller,
  passes the token through a file rather than an argument where anything listing processes could read it, and
  points atrium at the identity that comes out so there is no path to copy back. The zrok account token and the
  ziti private key never leave the daemon: the board is told one is present, never what it is.
- **The zrok commands match zrok v2, checked against the binary.** `zrok share reserved` no longer exists. A
  stable address is `--share-token` on a private share or `--name-selection` on a public one, and neither flag
  exists on the other subcommand. A private share prints no URL at all, so the board shows the access command it
  does print. Pressing stop is no longer reported as a failure, and the address it published is cleared with it.
- **The board can be reached from elsewhere, through an overlay atrium drives.** Atrium listens on loopback and
  has no login on purpose, and until now "use an overlay" was advice rather than a feature. The gear grows a
  panel per overlay: zrok publishes the board at a zrok address, OpenZiti hosts whatever services an identity is
  allowed to bind. Atrium keeps the configuration, starts the process and shows what it printed. It never opens
  an identity file, never proxies traffic, and has no opinion about who may connect, because that is the
  overlay's job and moving it here would be inventing the auth layer this project has ruled out twice. A zrok
  share defaults to private, and turning on a public one says out loud that the link has no login in front of it.
  Shares end with the daemon, since an address outliving the board it points at reads as the overlay being
  broken. See `docs/overlays.md`.
- **Global auto mode.** One switch for every session, including ones that have not started yet. Same slot in the
  permission chain as the per-session kind, which is last: it stops new questions and does not discard answers
  already given, so `never` rules, shelved cards and queued messages all still win. Recorded as `global-auto`
  rather than `auto`, because six hours later "I turned this session loose" and "I turned the whole board loose"
  are different answers. Kept in the database, so a restart is not consent to start asking again and not consent
  to keep approving either.
- **The header stops wrapping.** Nothing in it breaks across two lines any more: the tabs give up their width
  first and scroll, the actions keep theirs, and below 1150px the new-agent button and the auto-mode switch drop
  their labels rather than their shape.

- **Hooks can be wired one at a time, from the board or from a terminal.** Each row has its own `wire it`, and
  `atrium hook install [--event x]` makes the same edit from a shell, so the manual route is the same job by
  hand rather than a different one. Wanting the tool events and not the subagent count is a reasonable thing to
  want, and one button for all five made that impossible to say. An install that finds everything already
  correct writes nothing and keeps no backup, and says so rather than reporting a write.
- **The board hears about a hook the moment it lands.** `hook install` posts to `/hooks-changed`, which
  broadcasts over the event stream, so the count moves immediately instead of on the next poll of a tab that
  has to be open. Best effort like everything else atrium sends: the edit is already on disk and the poll is
  still behind it. The steps dialog drops each command as it is run and closes when the list empties, since a
  command you have run is not a step any more.
- **The daemon records where it is listening**, in the local runtime directory for the platform:
  `%LOCALAPPDATA%` on Windows, `XDG_RUNTIME_DIR` or `~/.local/state` on Linux. Deliberately nothing that roams,
  because a localhost port synced to another machine points a caller somewhere confidently wrong. `atrium hook`
  reads it, so a daemon on a port that is not the default needs no flag baked into `settings.json`. A file left
  behind by a daemon that was killed costs a connection refused in milliseconds, which is the fail-open path
  every hook already takes.
- **`atrium hook` cannot hang.** It read stdin to end of file, so run at a prompt it waited forever for someone
  to type EOF. An interactive stdin is not read at all now, and a pipe gets the same one second the post gets. A
  hook that can block indefinitely breaks the one rule that matters most: it must never fail a session.
- **A toast over a dialog is on top and clickable.** Two browser rules together rule out the easy answers: a
  modal is in the top layer, so nothing outside it can be drawn over it at any z-index, and a modal makes
  everything outside itself inert, so a popover drawn over one takes no clicks. Visible with dead buttons is
  worse than hidden. The host moves inside whichever modal is on top, and the stack it consults is recorded as
  `showModal` is called, because that order is not readable from the DOM: picking by document order put the
  toast in the dialog underneath the one on screen.
- **Clicking a toast or a notification closes whatever dialog is in the way.** A toast drawn over an open dialog
  lives inside it, since the top layer is the only place anything can draw over one. The click registered and
  the view changed, but the dialog stayed put over the thing you clicked to go and look at. A dialog holding
  edits nobody has saved, which is the runner form and the launch form, asks before it goes: throwing away a
  half-filled form because a toast arrived is worse than the toast being ignored.
- **The hooks are a subcommand, and the board wires them.** `atrium hook --event tool-start` replaces the
  PowerShell activity script. The script held a path only one machine had, so atrium could describe the wiring
  and never write it; a subcommand's path is this binary's own, which atrium knows. The runners tab reports
  which of the five are registered, which point at some other binary, and writes the missing ones into
  `~/.claude/settings.json`. Everything else in that file survives the round trip, the old file is copied aside
  first, and a settings file that will not parse is refused rather than rewritten. The `Stop` hook is not
  offered: one that blocks makes a session keep working, so it stays a manual decision. Reached from a button on
  the claude row, since these are claude's hooks and not that row's: they cover every claude on the machine and
  outlive the row being disabled. The count on that button can be turned off, through the same "do not ask me
  again" store as everything else, because not wiring them is a choice. Doing it by hand gets its own wide
  dialog: one command per block, each with a copy button, and the settings path copyable too.
- **Grouping is reachable from the board and the stack.** A `by project` / `off` control next to the sort pills,
  rather than only in the gear. It is not folded into the sort pills, because sorting and grouping are two
  questions and one control would mean picking a group rule costs you the sort. One setting behind both screens,
  so turning it off on the stack turns it off on the board.
- **A question you turned off can be turned back on.** Ticking "do not ask me again" saved the question as
  skipped but there was no way to undo it, and the gear's "ask me again" button called a function that did not
  exist. The gear now lists what is turned off and empties the list. Three questions carry the tick: asking a
  runner to exit, terminating one, and moving a card an agent is waiting on. Forgetting a card, clearing a
  column, deleting a runner and turning a session loose unattended ask every time, because each throws away
  something there is no way back to.
- **The subagent count comes from the subagent hooks.** `SubagentStart` takes it up, `SubagentStop` takes it
  down. It used to count `Task` tool calls on the way up, which reported the same subagent twice once the real
  hook was wired.
- **The board only rewrites the DOM when the markup changed.** The five second refresh is still blind, because
  ages tick with no server event, but the board, the stack, the terminal list, history and rules now compare
  before they write. Permissions already did.
- **Stack sort pills are one axis each.** `newest activity` and `quietest` were the same axis read two ways, as
  were `show wants me` and `sort by needs me`. Activity is first and the default.
- **Cards say what they are doing.** A live badge: thinking, running `Bash`, three subagents, and how long it has
  been at it. Fed by `POST /activity` from the tool hooks, and for free from `/permission` for gated sessions,
  since a permission request IS a tool starting. Never stored: a stored activity is a lie the moment the daemon
  restarts, and expires on its own after fifteen minutes of silence so a killed session stops claiming to be
  busy. See `docs/activity-design.md`.
- **Auto mode.** Per session: approve without asking, record everything. It does not override a `never` rule, a
  shelved card, or a queued message, because it means "stop asking me new questions" and not "forget the answers
  I already gave". Its counterpart is **what did it do?**, `GET /v1/tasks/{id}/review`: the decision log grouped
  by tool, identical calls folded into one line with a count, and the ones nobody saw put first. Interruption
  traded for review. See `docs/auto-mode.md`.
- **Rules can cover a folder.** A new rule kind covering work inside a directory: either the command names an
  absolute path inside it, or the session is working inside it and the command does not reach out. The second
  half is what makes it useful, since commands are written relative to where the session is and `go test ./...`
  names no path at all. Reaching out with an absolute path, or climbing out with `..`, still asks. This was
  previously only expressible as a command glob that had to account for the quoting itself, and
  `rm -f "C:/x/*"` silently fails against `rm -f "C:/x/y.db"` over the closing quote alone. Offered as a scope
  on any pending request, and as **allow a folder** in the rules toolbar. `POST /v1/rules` writes one by hand,
  which was not possible before: a rule could only be born from a request you had just read.
- **Board cards can be dragged and right clicked.** Drop between two cards for a midpoint rank, so an insert
  never renumbers the column; drop on a column to change status. Right click for open, attach, shelve, done,
  auto mode, review, terminate and forget, each of which previously cost opening the detail dialog.
- **Permission requests say who is asking.** With several sessions running, the same command means different
  things from different agents. On the pending card, in the decisions log, in the search, and in the CSV export.
- **Stopping is not killing.** `atrium stop` and `POST /v1/shutdown` reach the same wind-down that ctrl-c does:
  event streams released, supervised runners given ten seconds, listeners closed in order. Killing the process
  closes every pseudo terminal at once and takes the runners with it. Loopback only unless `--shutdown-token` is
  set, so a kill switch cannot be reached from a network the daemon was never meant to be on.
- **Resume ids are recorded.** Session hooks have always sent the harness's own session id and it was thrown
  away. Stored on every session event now, which is what makes a runner that was stopped, terminated, or lost
  with the daemon something to start again rather than something to lose.
- **A directory picker that browses the right machine.** `GET /v1/browse` lists the daemon's filesystem, with
  checkouts marked and sorted first. The browser's own picker reads whatever machine the browser is on, which is
  the wrong answer the moment the board is open on a phone. The launch form also gained recent directories, a
  resume checkbox that defaults to on, and enter to start.
- **Notifications take themselves down.** Permission notifications were sticky so a blocked agent could not
  scroll away unnoticed, and sticky on Windows means they never leave. There is now an expiry, default 30
  seconds, set under the gear. Choosing "never, until answered" restores the old behaviour on purpose. The
  service worker holds its own timer and the board sweeps expired ones whenever it is open, because a browser
  is free to shut a worker down before its timer fires.
- **Finished columns can be cleared.** `POST /v1/tasks/prune` deletes done and dead cards, optionally narrowed
  to one status and to those untouched for a given number of hours. A `clear` control sits in the done and dead
  column headers. Shelved is never swept, whatever it is asked, since shelving is the one act that says come
  back to this.
- **The command box fits the command.** It was two lines tall regardless of content, so a long command had to be
  scrolled inside a small window before it could be approved. It now grows to what it holds, up to 80% of the
  viewport, then scrolls.
- **The terminal fills the window.** Its height was a hardcoded `calc()` guess, which left a gap at one zoom
  level and overflowed at another. The header is measured instead, and the pane re-fits when anything around it
  changes size. An exited runner keeps its scrollback, since that holds the exit and the resume id, but stops
  presenting as a live session.
- **Runners installed as batch shims start.** `codex` and often `claude` resolve to a `.cmd` written by npm, and
  CreateProcess cannot start a batch file. It reported 0x80070002, "The system cannot find the file specified",
  for a file sitting on PATH, which sends you to look at PATH, permissions and the environment. `cmd /c` now
  goes in front of a `.cmd` or `.bat`, and PowerShell in front of a `.ps1`.
- **A launched runner has to prove it started.** The card was created before the process and nothing checked
  afterwards, so a misconfigured runner left a card in `running` describing a process that never got off the
  ground. A launch now waits two seconds, and a runner that falls over in that window puts its last terminal
  output on the card as the reason.
- **A new database is announced.** Opening the wrong path looks identical to every card and every rule having
  vanished, and `WORKTREE_ROOT` unset once made a hundred and twenty five rules appear to be gone. The daemon
  now says loudly when it created a database rather than found one.
- **Permission requests carry a dedup key.** The hook sends one built from the session and the exact request, so
  a retry after a daemon crash is recognised as the same question instead of being asked again.

## 2026-09-01 -- v2 prototype: durable state, a human-facing API, and a board

First working slice of `docs/architecture-v2.md`. New subcommand `atrium daemon` runs the whole thing.
`atrium hub` is untouched and still works exactly as before.

- **The hub is no longer amnesiac.** This reverses the "restart equals reset" invariant that
  `CLAUDE.md` and `docs/state-of-the-art.md` both declared. Restarting used to be the reset switch. It is now
  just a restart, and cards, history, and permission state survive it. The reversal is the entire point of v2:
  "how long has this been sitting" and "what was I even doing" cannot be answered from memory that dies with
  the process.
- **`internal/store`**: SQLite via `modernc.org/sqlite` (pure Go, so no cgo and no cross-compile pain). Schema
  written to stay Postgres portable: text ULID-ish keys, RFC3339 text timestamps, `CHECK` instead of enums,
  TEXT instead of JSONB, `?` placeholders. Tables are `task`, `event`, `permission`, `launch_spec`.
- **The halt.** Storage failure is not a degraded mode. Open or migration failure means the daemon refuses to
  start. `SQLITE_BUSY` is retried internally and never surfaces. Anything else halts: the agent-facing
  listener closes and stays closed, so runners see connection-refused and park on the backoff they already
  have, burning nothing. The process stays alive and the human-facing listener reports the cause.
- **Two listeners.** Agents on `--addr` (default `:7777`, same as the hub). Humans on `--http` (default
  `:7778`). Separate so a halt can kill the agent side without blinding the board.
- **Task model.** Cards, not agent names. `wire_name` is now an attribute, and pid is only a reconnect hint,
  so restarting a runner no longer splits one piece of work across two cards.
- **Observed versus overrides.** Runner-reported fields refresh on every reconnect. Operator-set values live in
  `overrides` and always win. Rename a card and the name survives the agent dying. This generalizes v1's
  `/rename`, which only ever covered the display name.
- **Human-facing API**: `/v1/tasks`, `/v1/waiting`, `/v1/permissions`, per-task events and prompts, and an SSE
  stream at `/v1/events`.
- **A board.** Kanban by status with age on every card, a stack view ordered by longest wait, and a permission
  queue with approve and block-with-guidance. Served from the binary via `go:embed`. This is a plain page for
  now rather than the agreed React SPA: it exercises the same JSON plus SSE contract, so the server does not
  care which one is talking to it.
- **`rank`** orders cards within a column, with midpoint insertion so reordering never renumbers neighbours.
  The board sorts by rank, the stack sorts by wait time, on purpose.
- **First tests in the repo.** Ten in `internal/store` covering reconnect identity, override survival, waiting
  order, the permission dedup replay, rank placement, and the halt refusing further work.

Known gaps in this slice: the TUI still consumes `*Hub` in process rather than the API, agents do not send a
registration payload yet (so observed data is limited to the wire name), and nothing launches or supervises
runners.

## 2026-06-11 -- permissions-only mode + deny-with-guidance

- The PreToolUse perm hook (`atrium-perm-hook.ps1`) now has a tri-state `ATRIUM_PERM_GATE`:
  - `on` / `force` / `1` / `true` / `yes`: gate EVERY session through the hub, no `.mcp.json` and no submit
    loop required. This is the permissions-only fleet mode: many agents funnel approvals to one hub pane.
  - `off`: never gate.
  - unset / anything else: auto-detect (gate only sessions wired to the atrium-agent MCP). Original behavior.
- Hub can now deny WITH free-form guidance, not just `y`/`n`. In the perms tab, type a message and press Enter:
  the hub denies the highlighted request and hands your text back to the agent as the block reason, so the
  agent course-corrects ("no, do X instead") rather than just getting a bare refusal.
- `/approve` and `/deny` now accept an optional trailing reason: `/deny 3 use the staging bucket instead`, or
  `/deny use a temp file` to deny the oldest with guidance. Works in both the Bubble Tea and `--simple` TUIs.
- Recommended activation (fleet-wide): set `ATRIUM_PERM_GATE=on` in the `env` block of settings.json and bump
  the atrium hook `timeout` so it blocks until you answer. The hook still fails OPEN when the hub is down.

## 2026-06-11 -- list-cursor navigation + forget-agent

- The perms and all-agents tabs now have cursor navigation: `↑`/`↓` (or `k`/`j`) move the highlight, `Enter`
  acts on the highlighted row. In the all-agents tab `Enter` opens that agent's chat; in the perms tab `Enter`
  or `a` approves and `d` denies the highlighted request. All gated behind an empty input so typing still works.
- New forget-agent action: `x` or `Delete` on the highlighted row in the all-agents tab drops the agent from
  both the TUI and the hub's in-memory maps (`Hub.Forget`). Use it to clear stale wire names left behind when a
  claude process dies. If that agent POSTs again it re-registers fresh.
- New slash command `/pick` (alias `/k`) opens the agent switcher overlay, same as `Ctrl-K`.
- These are Bubble Tea TUI only. The `--simple` line-mode TUI is unchanged (no rename / forget / pick).

## 2026-06-04 -- inline-mode TUI (native scrollback) + scoped gating

- TUI is now inline (no alt-screen). The chat content flows into the terminal's native scrollback via
  `tea.Println`. Mouse-wheel scroll, copy/paste, and terminal-side search all just work.
- The frame is a floating bottom panel: optional perm banner (top of frame, only when pending), perms /
  agents list (only on those views), header (active agent), tabs, input, status. Chat view in the frame is
  empty -- the conversation IS the scrollback.
- Perm-requests are filtered OUT of scrollback. They only appear in the floating banner. Same for
  keepalives (defensive; shouldn't arrive anyway).
- User's typed prompts are echo'd into scrollback as `[ts] you → <agent>` so the conversation reads top
  down: prompt, response, prompt, response.
- Permission hook now skips `mcp__*` (any MCP-provided tool) and `ToolSearch` (claude's meta-tool for
  finding other tools). Critically this stops the atrium-agent MCP's own `submit` from being gated, which
  was demanding a permission on every loop turn. MCP wiring itself IS the gate for those tools.

## 2026-06-04 -- two-line header + multi-pending banner

- Header is now two lines:
  - Line 1: `atrium │ <active agent name>  ← waiting  (+N unread elsewhere)  ·  K agents total`
  - Line 2: tabs `chat │ perms │ all agents`
- This makes the active agent feel like the parent of chat / perms rather than a sibling. The third tab is
  renamed `all agents` for clarity (it's the global view; ctrl-k is the inline picker).
- When more than one perm is pending, the banner now adds a "(showing oldest; N more queued -- see perms tab)"
  hint so it's obvious the banner is summarizing.
- Perm count in the tabs tab flashes red/yellow.

## 2026-06-04 -- pending-perm visibility (banner + beep)

- New permission arrivals now print the BEL byte (`\a` / 0x07). Most terminals (Windows Terminal default) beep
  on this. Fires once per new perm, not on every tick.
- Persistent, flashing, bordered banner across the TOP of every view (chat / perms / agents) whenever any
  perm is pending. Lists count, the first pending perm in detail (id / agent / tool / command preview), and
  the resolution keystrokes. Alternates yellow/red each tick. Designed to be unmissable.
- Status bar nudge "⚠ NEW permission #N -- press y/n" fires on arrival.

## 2026-06-04 -- activation-prompt content ignored + all-tool gating

- Activation phrase is now treated strictly as a trigger. Any task content in the same message (e.g.
  "atrium write a file") is ignored. The agent's only action on the activation turn is a greeting submit;
  real work waits for a hub prompt. Fixes a class of bugs where the agent did the activation-message task
  inside its claude tab, invisible to the hub.
- Permission hook now gates ALL tool calls that have side effects. Bash, Write, Edit, MultiEdit,
  NotebookEdit, and anything not in the read-only allowlist (`Read`, `Grep`, `Glob`, `WebFetch`,
  `WebSearch`, `TodoWrite`, `Task`) flow through atrium. Previously only Bash was gated, so file edits
  surfaced as claude's own in-tab permission UI instead of in the hub.
- Hub perm-request body now picks the most-useful field per tool: `command` for Bash, `file_path` (+ a
  "(replace edit)" / "(write N chars)" annotation) for Write/Edit, `url` for WebFetch, `pattern` for
  Grep/Glob, and the raw tool_input JSON as a fallback.

## 2026-06-04 -- unique default names + hub-side rename

- Default agent name is now `<cwd-leaf>-<pid-mod-100000>` (e.g., `atrium-19432`). Two agents in the same dir
  no longer collide on a shared prompt channel. Override still works via `--name` arg, `ATRIUM_AGENT_NAME`
  env, or by editing `.mcp.json`.
- New TUI slash command `/rename <agent> <new display name>`. Sets a UI-only alias; wire routing is unchanged.
  `/rename <agent>` with no second arg clears the alias.
- `@<name>` targeting now resolves wire names, display aliases, and prefix matches on either.
- Agents view and switcher show the display name with the wire name in parens when an alias is set, so you
  always know which is which.

## 2026-06-04 -- opt-in activation

- Agent instructions (MCP `ServerOptions.Instructions`) no longer auto-start the loop on first user message.
  The model now treats `atrium-agent` as opt-in: behaves like a normal claude session until the human types a
  recognizable activation phrase ("atrium", "run atrium", "start atrium", "atrium go", "enter atrium", etc).
- The exit phrase set ("stop atrium", "leave atrium") returns the session to default behavior without a
  trailing submit.
- Activation is by the model's judgment, not a regex on our side. The instructions tell it which phrasings
  count and which merely mention atrium in passing.

## 2026-06-04 -- choices picker + chat layout fixes

- Agent tool description now teaches the `{choices}...{/choices}` sentinel. When an agent has a small set of
  options for the human, it wraps them in that block; the TUI renders an inline numbered picker.
- TUI keystroke: `1`-`9` in the chat view picks the Nth choice for the active agent, sends that text back as the
  prompt, and clears the pending choices. Falls through to the agent quick-switch when no choices are pending.
- Chat viewport rendering: each message body is word-wrapped to viewport width, indented under a colored sigil
  (`◆` greeting, `·` response, `⚠` perm-request), and separated from the next message by a muted horizontal
  rule. Long (e.g., 100-line) responses are now readable instead of blowing the layout.

## 2026-06-04 -- per-agent TUI + agent switcher

- TUI now keeps per-agent scrollback. Chat view focuses on ONE agent at a time; tab bar reads `chat: <name>` and
  shows `(+N)` when other agents have unread.
- `Ctrl-K` opens the agent switcher (↑/↓, Enter to pick, Esc to cancel).
- `1`-`9` quick-switches to the Nth known agent (when input is empty and the active agent has no pending
  choices).
- Agents view shows every known agent with a flashing `← waiting` marker on agents that submitted and haven't
  been answered. Default routing target is marked with `>`.
- Hub tracks per-agent "waiting" flag: set true on every real submit, cleared on every prompt send. Exposed via
  `Hub.Waiting()` and `Hub.IsWaiting()`.
- Routing default for typed prompts is now the ACTIVE chat agent (was: most-recent submitter). Explicit
  `@<name>` overrides still work.

## 2026-06-04 -- Bubble Tea TUI (with --simple fallback)

- New `internal/tui` package: full-screen alt-screen Bubble Tea UI for `atrium hub`.
- Three tabbed views: `chat | perms | agents`. Tab / Shift+Tab to cycle. Slash-commands `/chat /perms /agents`
  jump directly.
- Status bar at the bottom shows transient feedback (e.g., "approve perm #5") and key hints.
- Old plain stdin TUI is still available via `atrium hub --simple` (single-terminal fallback when the
  full-screen UI is undesirable -- piping, scripting, dumb terminals).
- Added Charm deps: `bubbletea`, `bubbles`, `lipgloss`.

## 2026-06-04 -- permission gating via PreToolUse hook

- New hook script `dotfiles/claude/hooks/atrium-perm-hook.ps1`. Fires as a claude-code `PreToolUse` hook.
- Auto-activates when the project has an `.mcp.json` referencing `atrium-agent` somewhere in cwd or ancestors.
  Opt-out via `ATRIUM_PERM_GATE=off`.
- For Bash tool calls, POSTs to atrium hub `/permission` with `{agent, command, tool}` and blocks until the
  human at the hub runs `/approve N` or `/deny N` (or `y` / `n`). Hook returns `{decision: approve|block}` to
  claude-code, which obeys.
- Fails OPEN when atrium is unreachable: agent keeps working under claude-code's normal permission flow rather
  than getting bricked.
- Existing footgun-guarding hook (`pre-tool-use-hook.ps1`) still runs after this; an atrium-approved command
  can still be blocked by static rules.

## 2026-06-04 -- ANSI sentinel translation

- Hub TUI (both simple and Bubble Tea) translates curly-brace sentinels in agent content to real ANSI escapes
  before printing. Vocabulary: `{reset}` `{bold}` `{dim}` `{underline}`, foregrounds `{red}` `{green}` etc, plus
  `{bgred}` etc. Auto-appends `{reset}` if the message ends without one.
- Agent tool description teaches the vocabulary. No more relying on the LLM smuggling raw `0x1b` bytes through.
- Perm-request announcements use the same sentinels so they pop visually.

## 2026-06-04 -- hub/agent core (Mode A)

- New `internal/hub` package: HTTP server on `:7777` (configurable) plus an interactive stdin TUI.
- New `internal/agent` package: MCP server with a single `submit(kind, content)` tool.
- Loop: agent calls `submit` -> hub displays content + long-polls for a human prompt -> returns the prompt as
  the tool result -> agent processes -> calls `submit` again. Forever.
- Resilience:
  - LLM never sees a connection error. The agent's `post` retries indefinitely with exponential backoff
    (5s -> 60s) on every transport failure. Stderr nag once per `ATRIUM_DISCONNECTED_LOG_INTERVAL` (default 10m).
  - LLM never sees an empty prompt. Long-poll timeouts are absorbed internally as `kind="keepalive"`, which the
    hub does NOT display.
- MCP `ServerOptions.Instructions` holds the loop bootstrap so the model knows to call `submit` on first turn
  without re-pasting a long prompt.
- Agent name defaults to the cwd leaf when `--name` isn't passed. Override per `.mcp.json` if needed.
- Hub TUI commands: `@<agent> text`, `/agents`, `/perms`, `/approve [id]`, `/deny [id]`, `/help`. Plus `y`/`n`
  shortcuts to approve/deny the oldest pending permission.

## 2026-06-04 -- Mode B aggregator (read-only)

- `atrium serve` runs as an MCP stdio server with three tools backed by the gwt session ledger:
  - `snapshot` -- every session's current state.
  - `wait_for_change` -- long-poll for the next state transition (default 30s, max 300s, `since` cursor).
  - `focus_session` -- best-effort `wt.exe -w <window> focus-tab`.
- `atrium status` (CLI) prints the same data as a table; `atrium watch` is a native Go replacement for
  `gwt watch`.
- Reads `$env:WORKTREE_ROOT\sessions\*.json` (per-session state) and `watch\state.log` (transition events).
  Strictly observer; writes nothing.
