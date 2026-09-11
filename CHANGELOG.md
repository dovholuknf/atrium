# Changelog

A running log of what's been built. Newest first. No formal version cuts yet (everything is `v0.0.0-dev`); each
section heading is just "what landed in this iteration."

## Unreleased

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
    version left `terms with nothing attached` between the view you came from and the session you asked for,
    and back needed pressing twice to leave a place you were never in.
  - **The opening view needs an entry of its own**, recorded after the restore has decided where the page
    actually landed. Without it the first push replaces rather than pushes, and the first press of back steps
    straight over where you started.

  `check-terminal.js` rule 14 holds all three, because the failure is not that back stops working, it is that
  back appears to work and goes to the wrong place.

- **The terminal switcher groups by host, then org, then project.** (`B2-02`)

  Eleven entries repeated `github/` eleven times and `dovholuknf/` three or four, so the list was widest where
  it said least. It was already failing: rows rendered as `...nziti/ziti-openwrt:firmware-upgrade-recovery-docs`
  because the strip is a few hundred pixels wide and the FRONT of the path was being cut to keep the leaf. A
  hierarchy fixes that by construction, since the shared prefix moves into a heading and stops being drawn once
  per row.

  Grouping never reached this strip. The settings expression the operator writes applies inside a board column,
  and the strip is not a column, so this is hierarchy taken there rather than an extension of that.

  **Two collapses, and the second is what makes it usable.** A chain of only children is one heading, so
  `openziti-test-kitchen/docpreview` is not two levels. And a level holding a SINGLE row is not a level at all:
  it draws as `atrium:main` on the row. Without the second, the real board came out with eleven headings for
  ten rows, which is the failure this design is warned about. With it, five headings for the same ten rows, and
  every one groups something.

  **Pinning stops being a divider here**, which is the interaction this had to settle. Grouping and pinning both
  wanted the top-level split. The obvious answer is what the stack does, pinned as one group above the rest, and
  the real board refutes it: eight of ten terminals are pinned, so that block is nearly the whole list drawn
  flat with all the repeated prefixes still in it. A split that puts most rows on one side is not a split. So
  pinning keeps its meaning and loses its heading, sorting first within each group, with the star carrying the
  signal. That star was invisible until two days ago, which is probably why the divider was carrying it.

  **A heading here is a label, not a control.** No click target and no accordion. `docs/dispatch-queue.md` group
  F has an open complaint that a group heading on the BOARD toggles when you click the space around it, and
  three levels of heading would have multiplied whatever is decided there.

  `check-morph.js` rule 3 read a FIXED 4000 character window from the top of `renderTermList` and failed
  anything that pushed the row's key past it, which is `B2-21`. It searches the whole function now, and points
  at `termRow`, which is where the row markup moved and is the shape `cardHTML` and `stackRow` already had.

- **One session's message goes into another's terminal, when that terminal is free.** (`B2-28`)

  The peer bus queued always, at length and on principle: atrium owned the terminal for the human, and a peer
  was not that human. The operator overruled it. "i want agents to be able to talk to one another without me
  here but i want to see when the agent does it. i consider the pty shared between me and all agents so PUT THE
  FUCKING TEXT INTO THE STREAM."

  Two things make that more than a preference. The refusal rested on OWNERSHIP, and if the pty is shared
  between the human and the agents the question becomes CONTENTION, which is solvable. And the risk was already
  being taken everywhere else: `Say` types text, pauses and presses Enter, and a message, a note and an
  action's prompt all go through it. The peer bus was not the one safe path, it was the one path pretending the
  risk was unacceptable.

  So the objection is answered rather than dropped. Three states, and the difference between them is Enter:

  | the terminal | what happens |
  | --- | --- |
  | nobody attached, or idle | typed, attributed, and submitted |
  | somebody who typed in the last 20 seconds | typed and attributed, NOT submitted |
  | a part written line | never typed, queued instead |

  The middle one is the operator's own qualifier, "assuming i'm not using it". Putting words in front of
  somebody is a different act from pressing Enter under their hands, so they send it, edit it, or clear the
  line.

  **It reads a record, not a guess.** Atrium is the only way the operator can type into a supervised session, so
  every keystroke has already passed through `runner.Write` and the daemon knows whether a line is open. The
  tracker is in the DAEMON rather than the board, which has something similar for path completion: that copy is
  per viewer, dies on reload, and would make a decision about the pty depend on which tab is open. Nothing
  parses the runner's OUTPUT to infer this, which is the line `B2-20` declines to cross.

  **The queue stays** as the fallback for everything not typed, which is still most traffic on a machine where
  atrium owns no terminals. A typed message is written to the timeline instead, so it is still auditable.
  Recording both would deliver it twice.

  A per-card switch turns it off, on by default, offered only where atrium owns a terminal. A card lent over a
  share is the case: the guest holds that terminal and was handed exactly one session.

  `docs/supervision-design.md` and `docs/architecture-v2.md` are updated. `CLAUDE.md` still lists this as out of
  scope and names the peer bus, and it is a symlink into another repository, so it is left disagreeing until
  somebody there changes it.

- **A shell opens `pwsh`, then `powershell`, then the command shell.** Operator: "i just want pwsh not fucking
  cmd".

  `shellFor` went from the `shell_command` setting straight to `COMSPEC`, and on Windows `COMSPEC` is
  `cmd.exe`, so unless that setting was populated the answer was always cmd on a machine where everything else
  is PowerShell. The comment over the setting already knew this and offered the setting as the workaround.

  **The middle of the order is the part that matters.** `pwsh` is PowerShell 7 and a separate install,
  `powershell` is 5.1 and is on every Windows there has ever been, and `cmd` is the floor. Falling to cmd when
  `pwsh` is missing skips the one that is always there, which is the same bug with an extra step.

  **Looked for, not assumed from the platform.** Seen while setting up a room on another machine: an ssh
  default shell pointed at a `pwsh.exe` that was not installed there and the session came up on 5.1 with
  nothing saying why. `exec.LookPath` is the check, and the daemon logs what it found at startup, because a
  search of the daemon's own PATH is not something anybody can work out from the other end of a browser.

  **One resolver, two callers.** `internal/shellpick` exists as its own package for the reason
  `internal/safepath` does: the daemon opens a card's shell and the store seeds a `shell` harness row, neither
  can import the other, and answering it twice is how they disagreed. They already had: the harness said
  `pwsh` unconditionally while the daemon said `COMSPEC`, so one board offered two shells that were PowerShell
  7 and cmd. The harness seeds on first run only, so an existing database keeps the row it has.

  **And `shell_command` reached the settings surface.** It was documented, carried by the config export, and
  settable only by editing the database or importing a config. That mattered more than an ordinary gap: it was
  the workaround for this exact problem, so the escape hatch was unreachable by the person who needed it. The
  hint says what an empty box resolves to on this machine.

- **The text in a terminal scales without scaling the board around it.**

  Browser zoom was the only tool and it takes the tabs, the card strip, the bar and every chip with it. The
  terminal's cog now has **text size**, a number with a minus and a plus either side of it, which changes
  xterm's own `fontSize` for that pane and nothing else on the page.

  **A value you can type, not a list you pick from.** It started as a submenu of bigger, smaller and reset,
  which closed the menu on every press: nudging a font up four points meant opening the cog four times, and no
  number of entries lets somebody say 22. So the menu learned one new row shape. It is the only row here that
  does not close the menu, because it is the only one that is a value being adjusted rather than a decision
  made once, and it is a `div` rather than a `button` so the row itself does nothing and the three controls in
  it do. The row handler now scopes to DIRECT children for that reason: a nested button carrying no index
  would have looked the item list up with a NaN.

  Closing the menu also blurs anything focused inside it. The menu is hidden rather than removed, so a control
  that had the focus kept it and every keystroke after that went somewhere nobody could see. The stepper's
  field is the first thing in there anybody focuses on purpose.

  **Remembered per card, in this browser.** It shipped for an hour dying with the pane, on the reasoning that
  it answers "I cannot read this right now" rather than "this is how I like terminals". The reasoning was fine
  and the implementation of it was not: `openTerm` builds a terminal every time you SWITCH session, so clicking
  a second card and coming back put the size straight back to 14. Kept in `localStorage` keyed by card now, the
  way a popped-out window's size already is, and per card rather than one number for the board because one
  agent drawing wide tables and another printing a log want different answers.

  It is still NOT on the daemon, and that line is worth holding: this is a property of the screen somebody is
  reading rather than of the work, so it has no business travelling to another machine or being served to a
  guest holding a share of one session.

  **A font change is a resize**, which is the part that makes it more than a setting. xterm measures in cells,
  so changing the size changes how many rows and columns fit and that number goes to the runner. It goes
  through `onTermResize` rather than fitting on its own, because that already ignores a fit that changed
  nothing, which would otherwise snap a scrolled-up terminal to the bottom, and already restores the viewport
  and holds it while the runner repaints. Those are `check-terminal.js` rules 9 through 12 and reimplementing
  them here would be a second copy to get wrong.

  Rule 13 keeps it that way, and asserts the size IS remembered across a switch and is NOT sent to the daemon.
  Its first version asserted the opposite of the first of those, which is how the reset survived review: the
  check agreed with the code and both were wrong. It SEARCHES the function rather than slicing a fixed number
  of characters off the top of it, which is a trap a sibling rule has.

  The pty follows the smallest attached viewport, so a bigger font does narrow the columns the agent draws
  into for anybody else watching. Left that way: a terminal has one size, and an exemption would mean the agent
  drawing into a width nobody is looking at.

- **A question can be taken off a card without telling the session anything.**

  Every way an ask got settled delivered text: saying something to a card types it into the terminal, sending
  its note does the same, and a session declaring its work over is the session's own decision. None of them is
  available to somebody who already answered by TYPING IN THE TERMINAL, which atrium cannot see and which is how
  anybody sitting in front of a session replies. `askAnswered` says so in its own comment and leaves it there.

  So the question stays open forever. Nothing expires an ask, and `wasAsked` on the board falls back to the ask
  field when no waiting reason is set, so every LATER turn-end on that card announces itself as a question. One
  card carried two of them for two days and rang as `asked you something` every time it finished a turn, for
  questions answered within a minute of being asked.

  `DELETE /v1/tasks/{id}/asks` settles them all and sends nothing, and the card menu offers it when there is
  something to dismiss. Recorded in the event log as `dismissed` rather than as answered, because that is what
  happened. A question put to a peer comes off too: it is still one nobody is going to answer, and a menu entry
  that said it cleared them and did not would be worse than not having one.

  What this does NOT do is stop the mislabelling. `wasAsked` inferring a question from a stale ask field is a
  separate defect and is reported rather than changed here.

- **Four things the board drew wrongly.** (`B2-10`, `B2-11`, `B2-03`, `B2-09`)

  **A pinned session in the terminal switcher was the wrong colour.** The row wrote its star as `class="pin"`
  with no state, so `.pin.on, .pinned > .pin` could never match and every star drew in the same faint grey. Only
  the glyph changed, filled against hollow, which at strip size is five pinned rows and one unpinned row
  looking like six rows with slightly different characters in them. It now says what `pinChip` on a card has
  always said.

  **And a rule between the pinned sessions and the rest.** The switcher already sorted pinned to the top and
  said nothing about it, and sorting by something invisible reads as not sorting. The stack answered this first
  with `.pinbreak`, so this is the same component rather than a second answer, drawn only when the list is
  MIXED: all pinned or none pinned names nothing.

  **The divider survives `mini` and the star does not.** That looks inconsistent and is the decision. The star
  goes because pinning is a decision and a decision does not belong on the control you shrank to get it out of
  the way. A divider is not a control, it is the shape of the list, and with no stars in that mode it is the
  only thing left saying the top of the list is deliberate. Tightened to fit 104 pixels rather than inheriting
  the star's rule by accident.

  **The tab bridge went missing after leaving the terminals view and coming back.** The span drawn across the
  gutter joins the attached row to its pane, and the row went on marking itself attached with nothing reaching
  it. Two causes, both real:

  - `placeTabBridge` had three callers, the strip render, a resize and the pane teardown, and none of them was
    the attach completing. It gives up when `term` is null, and on the restore path the strip renders before
    the socket opens, so the one call that fired measured a terminal that did not exist yet. It is called from
    `onopen` now.
  - Every rectangle on a hidden subtree is zero, and zeroes read as a card scrolled out of sight beside a
    gutter with no width, so a window resized while you were on another view hid the bridge for a reason that
    had nothing to do with it. A measurement taken on a hidden layout is discarded now rather than acted on,
    and the placement runs on the way back into the view.

  **Board columns were uneven and the narrow one clipped its own cards.** The weight was
  `Math.min(2, Math.max(1, Math.round(Math.sqrt(n))))`, and the rounding threw away the thing the square root
  was chosen for: `sqrt(2)` rounds to 1 and `sqrt(3)` rounds to 2, so a column jumped from narrowest to widest
  between two cards and three, and three cards and fifty one came out identical. `flex-grow` takes fractions,
  so the root is used as it is, capped at four.

  **Two comments disagreed about the same number and one of them is gone.** The weight's comment said the
  columns had a flex basis of ZERO and justified its cap of two with it. The stylesheet says the opposite,
  deliberately and at length: a basis of 268 with a `min-width` to match, so the weight divides only the
  SURPLUS. The stylesheet is the one that is true, and it describes correcting the zero basis, so the weight's
  comment was stale and has been deleted rather than left to be reconciled again. The old cap of two was chosen
  under the zero basis and is not load bearing now, since the basis is what protects the quiet column.

- **`scripts/start-atrium.ps1`, for a daemon that has died rather than one you want to replace.**

  `atrium stop` and `restart_atrium` both need a daemon that is answering. The case neither covers is the
  process being gone: the board refuses connections, every supervised session went with it, and the session you
  would normally ask for a restart from is one of them. A machine that slept, a console that was taken away, or
  a crash all land there.

  It refuses to start a second daemon, since two on one database is the most likely way to make things worse:
  the second cannot bind the port, exits, and leaves you looking at the first one wondering why nothing
  changed. If the port is held by something that is not answering it says which process and stops rather than
  killing anything.

  It reads the database path out of the address file rather than assuming the default, because a daemon started
  against another file would otherwise come back on the wrong one and show an empty board, which reads as data
  loss. It reports how many terminals it is about to reopen and WHEN that list was recorded, since a daemon
  that was killed never wrote one and both the list and the scrollback are from the last clean stop.

  `-InstallStaged` puts a binary staged by a build that never got its restart into place, which is the one
  moment that can be done. Off by default: starting a daemon and changing which daemon you start are two
  decisions.

- **Flattening a replay no longer turns bracketed paste off, which was making one paste arrive as five.**
  (found while investigating `B2-18`)

  `flatten` keeps SGR and drops every other escape sequence, on the grounds that anything else can move the
  cursor or erase. `ESC [ ? 2004 h` is neither. It is how a session says it understands a paste as ONE thing
  rather than as fast typing, and the board reads it off this stream to decide whether to wrap a paste in the
  markers. Dropping it meant every pane opened after a replay believed the session did not support bracketed
  paste, and pasted a raw body.

  A raw body is then at the mercy of arrival timing, and the pseudo terminal's input pipe destroys it.
  Measured with a real ConPTY: **41,001 bytes written in one call arrive at the child as 4096 byte
  installments**, milliseconds apart. A session that tells typing from pasting by timing sees one paste as
  five, which is the reported symptom.

  So bracketed paste survives flattening, and nothing else that resembles it does. Keeping every private mode
  that does not draw would be the shorter rule and is how `ESC [ ? 1049 h` gets back in, which takes the whole
  scrollback off the display at once.

  This is not the whole of `B2-18`. It made an intermittent fault unconditional, and the underlying question,
  what a pane should do when the enable has scrolled out of the ring entirely, is reported rather than
  answered here.

- **Reading twelve lines of a dying runner's output no longer copies its whole scrollback, three times.**
  (`B2-17`)

  `awaitExit` wants the last twelve lines to say why a runner died. It asked for `Snapshot`, which copies
  everything the ring retains, and handed that to `lastOutput`, which made a string of it, ran a regexp over
  that to strip the escapes, and split the result. Three copies. With the scrollback setting at its ceiling
  that is over a gigabyte of allocation to read about a kilobyte, paid on EVERY runner exit, and worst for a
  runner that lived all day and filled its ring. Go grows the heap to the peak and hands it back to the
  operating system lazily, which is what leaves a resident set far above anything the process holds.

  `ringBuffer.Tail(n)` reads the last n bytes, clamped to the oldest byte still retained so a wrapped ring can
  never hand back what it overwrote. Both callers take it, at 64KB, which holds twelve lines of anything.
  `settleFor` was not costing anything yet, since it only fires within two seconds of a launch, and it is
  fixed too because it is one widened window away from being the same bug.

  `lastOutput` also trims what it is given, so widening a caller cannot quietly bring the cost back.

  The test that matters measures BYTES rather than allocation counts, since one allocation of eight megabytes
  and one of sixty four kilobytes are both one allocation. Against the old call it reports 8,388,608 bytes for
  a 65,536 byte read.

- **A terminal shows one session again, and the older scrollback is asked for rather than pushed.**

  Joining the pre-restart scrollback onto the front of every attach was confusing in a way that took a while to
  name. A resumed claude session REPRINTS its own recent history, so the carried bytes ended mid-conversation
  and the same conversation started again below the restart divider: two copies of the last hour, with nothing
  on screen to say why. Chasing that produced a lot of correct fixes to the wrong problem.

  So an attach now shows what THIS process has produced, which is what a terminal running claude shows. The
  carryover is still written at every clean stop, still folded forward so it reaches back through many
  restarts, and reachable from the terminal's cog: **history from before the restart**, served as plain text on
  `GET /v1/tasks/{id}/scrollback/older`.

  It opens in a tab rather than in the terminal because xterm can only append. There is no way to put bytes
  above what is already in its buffer, so a "load more" at the top of the scroll would have to clear the pane
  and redraw it, throwing away the live screen to show history. Colour is stripped for the tab, since a browser
  renders escape codes as literal text.

  A card with nothing saved says so, and says why, because that is the part nobody can guess: the file is
  written when atrium is stopped, so a daemon that was killed left none.

  A lent session cannot reach it. `overlay_guest.go` is default-deny and the route is named in its refusal
  list: a guest was lent a session, and everything that terminal said yesterday is a different offer.

- **A terminal reopens at the width it was last looked at.**

  A pseudo terminal opened at a fixed 120 columns and was resized by the first browser to attach, so every
  restart pushed a stretch of output composed for a terminal nobody was sitting at into the scrollback, with
  hard line breaks a third of the way across the window. Flattening a replay can drop a cursor move but cannot
  undo a line break that is already in the bytes, so the only fix is not to produce it.

  The width is recorded on the card at the wind-down (`last_cols`, migration `0045`), never on the resize
  itself: a browser sends a resize frame whenever anything on the page moves, and that is not a rate to write
  to sqlite at. The resize a moment after launch is then not a change at all, and the ring merges the two marks
  into one run.

- **A restart reopens the terminals that were open, not just the fixtures.**

  Six supervised terminals went down with the daemon and four came back. The two that did not were the two
  nobody had written a fixture for: a card launched from a pull request, a piece of work picked back up by
  hand. A fixture is a standing decision, "this terminal exists every day", and it was the only thing that
  survived a restart. A restart is maintenance rather than a judgement about which work matters.

  The wind-down now records which cards had a runner, beside the scrollback it already writes, and the next
  daemon starts them again: fixtures first, since a fixture is what pins its card, themes it and decides how
  it resumes, then everything else that was open. What gets recorded is only the LIST OF CARDS, because
  everything needed to start one is already on its row: the harness is `Runner`, the directory is `Worktree`,
  the conversation is `ResumeID`. Copying those into a file would be a second source of truth that goes stale
  the moment somebody moves a worktree.

  Not reopened: a shelved card, since putting work down is a standing no and a restart must not undo it; a card
  that has since been pruned; one whose worktree or harness is no longer recorded. A stale resume id is dropped
  and the session starts fresh in the right directory, because a resume that fails exits within a second and
  reaches the board as a dead card and a terminal that never appeared. An empty list is written rather than
  skipped, so closing everything and then stopping does not reopen yesterday's set.

  Pairs with the scrollback carryover below and neither is much use alone: that one brings back what a terminal
  said, this one brings back the terminal.

- **The scrollback limit says when it has been hit, and raising it no longer needs a restart.**

  A history that stops is either everything there was or the buffer's own ceiling, and those call for opposite
  reactions: nothing, or a number in the settings. Nothing distinguished them, so every short scrollback read
  as the limit. The top of a replay that was cut now says so and names the size.

  The size itself was read once when a runner spawned, so raising it did nothing until every runner had been
  restarted, and a restart is what somebody raising it is trying to survive. `Grow` copies the retained bytes
  into a bigger buffer, keeps every width mark and loses nothing. Shrinking still throws bytes away, so it is
  refused. It runs on attach, which is a person opening a terminal rather than anything on the output path.

- **Scrollback survives being looked at. It took three goes.**

  Resizing a window, changing the browser zoom, or popping a card out emptied the terminal and left one grey
  line saying the output could not be redrawn. Each attempt at this fixed a real defect and uncovered the next
  one, so all three are worth writing down.

  **One: the buffer refused to hand over anything.** `SnapshotAt` returned only the run of output composed at
  the width the terminal is at now, and `setViewport` lays a width mark BEFORE the pty is told its new size. So
  a viewer attaching at a new width asked for a run that was zero bytes old and got nothing. Dragging a window
  edge cost an hour of scrollback.

  **Two: it handed over the trailing run.** Better, and still wrong, because a run ends at every width mark. A
  session that had been resized twice replayed only what it drew after the second resize, which for a quiet
  agent is a couple of screens out of sixteen megabytes. Indistinguishable on screen from the first bug. The
  ring's width marks now DESCRIBE the output instead of gating it: `Replay` returns everything retained plus
  every width it was drawn at, and the attach says which above the replay. The same reversal applies to the
  restart carryover, which had been declining to save anything at all for a session that had been resized, so
  a resize plus a restart lost the history twice over.

  **Three: the bytes arrived and erased each other.** Two megabytes down the socket, two screens on display. A
  terminal user interface draws by moving the cursor to an absolute row and erasing the line it lands on, which
  is right against a live terminal and destructive on a replay: every redraw lands on the history instead of on
  the older version of itself it means to replace. One card's 1.6MB of carried scrollback held 33,957 cursor
  moves and 1,879 erases. Replayed history is now flattened first (`internal/daemon/flatten.go`): anything that
  can move the cursor off its line, erase, scroll, or switch to the alternate screen buffer is dropped, and a
  bare carriage return becomes a line ending. Colour is kept, since SGR cannot move or erase anything.

  What that costs, and it is a real cost: a box or a progress bar that redrew in place becomes every version of
  itself in turn, and the final rendered state no longer matches what the runner believes is on its screen
  until the next full redraw. Both are paid on history alone, live output is untouched, and the alternative on
  offer was an empty pane.

- **Two windows on one terminal is refused, rather than quietly wrecking the wider one.**

  A `#term=<id>` url pasted into a tab attached a second viewer to a terminal that already had one. The daemon
  then sized the pty to the SMALLEST viewer, which is correct, and nothing told the larger window's xterm to use
  the agreed size. So every cursor-positioning escape the runner emitted was computed for a narrower line than
  the one it landed on, and a wrapped input line redrew on top of itself: `accepting newlines for some reason`
  came out as `acceptingenewlineshforisomewreason`, one character eating each space at what would have been a
  wrap column. The narrow window looked perfect throughout, so the window being typed in read as the broken one.

  A solo window now asks, before it attaches, whether anybody already holds that card, using the same broadcast
  roll call `attach` uses rather than the fifteen second heartbeat, so a window that was closed without
  releasing does not lock a card out. The refusal offers **take it anyway**: the holder yields, releases its
  claim and tears its own pane down. Taking rather than closing the other window, because `window.close()` on a
  window this script did not open is refused silently, so a close button would have lied about half the time.

  `docs/supervision-design.md` says nothing arbitrates two views onto one terminal. Something does now.

- **The board no longer draws an empty board at a lent session's address.**

  A share hands out a link ending `#term=<card>`, and the fragment is the part the server never sees. Open the
  address without it and the page rendered its own chrome with every list empty, because `guestHandler` refuses
  `/v1/tasks`, `/v1/events`, `/v1/permissions` and `/v1/rooms` on purpose. Nothing leaked and it looked exactly
  like atrium being broken: the first person to try it asked whether it was a clone of his own board.

  The page now recognises what it is. A 403 from `/v1/tasks` is the signal, and it says what the address is for
  and that the link needs its fragment back. Paste and drop are withdrawn there too, because both post to an
  endpoint a guest is refused, and offering somebody a thing that cannot work is worse than not offering it.

- **A refusal reaches the page in the words the daemon chose.** `api()` read the body as JSON and threw away
  anything that was not, so every `http.Error` sentence arrived as the single word `Forbidden`. That is what
  made a refused upload say `that did not go up` instead of `this link is one terminal. nothing else here is
  shared.` One layer, every refusal on the board.

- **Scrollback survives a restart, which atrium does to itself constantly.**

  The daemon keeps the last N bytes of a runner's output in a ring buffer, in memory, sized at spawn. A restart
  exits the daemon and closes every pty, so every ring goes and a resumed fixture is a new process with an empty
  one. The only surviving copy was whatever xterm held in a page that happened to be open, so closing that
  window discarded the last copy of the morning. `docs/reload-design.md` is an entire document about installing
  a new daemon from inside a session it is running, which is how often this happened.

  Each ring is now written out during the wind-down the daemon already narrates, and handed back to the next
  runner on the same card. Bounded by construction because a ring is bounded, and NOT a transcript: nothing is
  appended as output is produced.

  Keyed by the CARD, because a card outlives the process it describes and a pid is only a reconnect hint. One
  width in the header rather than a list of marks, because the ring only ever replays the run composed at the
  width in force now, and that rule is what keeps an attach readable. Offered once per card per daemon, so a
  card relaunched by hand three hours later is new work rather than this morning's output. The join is drawn
  rather than hidden: a dim `atrium restarted here` divider. Reading it back cannot fail a start, and writing
  it out cannot delay a shutdown past the budget it already has.

  **It does nothing for a crash.** A killed daemon never runs its wind-down. The file is deliberately kept when
  claimed, so a kill at least comes back to the last clean stop.

- **A second question no longer destroys the first.**

  An ask used to be written into `why`, the sentence an operator writes once and reads in a week, so it
  destroyed the standing answer to "what was I even doing". That got fixed by giving the ask its own columns,
  which fixed the collision with `why` and left a worse one: one column holds one value, and

      atrium ask --continue "which of these two schemas is authoritative"
      atrium ask "which branch is base"

  silently dropped the first, with nothing anywhere recording that it had been asked.

  An ask is a row now, keyed by card, the way a message and an event already are. Each is answered on its own,
  because `ask_peer` is per question and one card can be waiting on a peer for one thing and on you for another.
  A peer's answer settles only what was routed to that peer. A message from the operator settles everything,
  since it is addressed to nobody in particular. Ten outstanding per card, and the eleventh retires the OLDEST
  with the drop recorded on the row and in the event log, because the original defect was never that a question
  was lost: it was that it was lost silently.

  The old columns stay as a mirror of the oldest outstanding question, recomputed after every change by the one
  function allowed to write them, so the board, `fleet.go` and `atrium peers` see exactly what they saw before.

- **The question is on the card, and it is a control.** The stack row drew it and the card dialog did not, so
  what a session SAID IT DID had a field and what it is STOPPED ON had none. It has one now, beside the recap
  and shaped like it, naming the peer when there is one. Pressing the question, on the row or in the dialog,
  opens a box aimed at that card, since saying anything to a card is already what answers it.

- **`atrium ask --working` is `--continue`.** The old name described the state the session was already in. The
  new one describes the decision, which is the only thing the flag controls, and the pair now reads as stop by
  default and carry on by request. `--working` still works and is hidden from help.

- **A command that explains itself no longer prints the explanation, then the error, then the usage, then the
  error again.** `atrium ask --peer nobody-here` wrote a refusal naming every handle that would have worked, and
  cobra then buried it under twelve lines of flag descriptions. Suppressed for the paths that write their own
  sentence, and only those: a failure with no message of its own still reports itself.

- **Switching zrok account there and back stopped refusing over a trailing slash.** `https://api-v2.zrok.io` and
  `https://api-v2.zrok.io/` are the same instance; `!=` disagreed. The board's environment toggle was also
  sending the address box's contents as an address CHANGE, so choosing which account asked to move an instance
  nobody had touched. Endpoints are compared normalised now, and the refusal that remains names the environment
  and both addresses so a one character difference is visible.

- **The card dialog says that it saves.** Every field committed on leaving it, and the only button said
  `close`, which reads as the discard half of a pair. It says `save and close`, the fields say they are kept on
  leaving them, and there is one commit path rather than two.

- **The pinned cards are labelled.** A rule under them read `the rest, <whatever the sort is>`, naming the sort
  the control at the top of the screen already names, and nothing labelled the block above it. `PINNED` and
  `THE REST`.

- **`ctrl-shift-v` pastes straight through where it can.** It opened the paste box unconditionally. The box
  exists because reading the clipboard is a permission granted per origin and every share is a new origin whose
  prompt never gets answered, so the read never settles. On loopback it settles at once. It now races the read
  the way right click already did, and the box appears only when the read does not come back. No locality test:
  whether the clipboard answers IS the test, and it stays right on a browser nobody anticipated.

- **The restart banner was unreadable on a light skin, for the second time.** Detaching cleared `--term-bg` and
  left `--term-fg` and `--term-line` behind, so the pane carried half of the last terminal theme and the
  leftover foreground paired with the board's own background. The first fix treated the variables as missing
  when they were stale. Every theme variable is cleared together now, and one rule sets the banner's background,
  border and colour in one place. The same leftovers were quietly reaching the file drawer and the find bar.


- **Work can be sent to another machine, and the hub still never dials one.**

  A room already dials this hub every twenty seconds and says what is on it. Cards travelled inward and there
  was no way to say "start this there", which is the whole reason four idle machines stayed idle while one
  desktop ran thirteen runners.

  A queued launch is now a durable row on the hub with a room name on it, and it rides the reply to the
  check-in that room was already making. Nothing new is dialled, nothing has to be reachable, and a machine
  behind NAT needs no configuring. A room that is switched off is still a room you can queue for: it collects
  the queue when it comes back.

  Named, not offered. A laptop, an M1 mini and two cloud boxes are not interchangeable, and the operator
  handing an item over already knows which one it belongs on. Two `atrium room` processes under one name
  cannot both take an item, because a claim is a conditional state change and the winner is handed a token
  that a result must carry back. A claim nobody answers goes back to the queue once and then gives up with
  the reason on the row, so a permanently broken item does not start a permanently broken launch every time
  that machine reappears.

  `atrium dispatch to <room>`, `atrium dispatch list`, `atrium dispatch cancel`, and a `queued for other
  machines` pane on the board beside the rooms. `docs/remote-launch.md`.

- **A remote machine does not have the worktree, and atrium still does not make one.**

  Every launch names a directory that already exists because something outside atrium made it, and a cloud box
  has no `D:/worktrees/...`. The tempting answer was to let a dispatch carry a prepare command, since the
  harness table already has one. That is refused: `Prepare` captures an environment, and turning it into the
  thing that makes the workspace is where atrium starts holding somebody else's git commands.

  So the room is the authority on its own filesystem. A dispatch that names no directory uses the working
  directory on that room's own runner, which is the ordinary case and means the hub never learns a path on
  another machine. A dispatch that names one is only honoured by a room started with `atrium room
  --workspace`, and only inside it, resolved through `internal/safepath`. There is no third fallback: the
  local launcher lands on the process's own directory here, and doing that for a remote instruction starts a
  session somewhere plausible and wrong on a machine nobody is watching.

  A directory that is not there is refused before anything starts, and the refusal travels back onto the queue
  row in that machine's own words. `atrium room --no-launch` reports cards and takes no work at all, said on
  every check-in so the machine granting the permission is the one that can withdraw it.


- **A session can ask ANOTHER SESSION for help, and get an answer back.**

  `atrium ask` could say a session was stuck and what would unstick it, and the only reader was a human.
  `atrium ask --peer <handle>` now routes the question to another session over the bus `atrium tell` already
  uses, and `atrium answer <handle> <text>` carries the reply back. Both directions are QUEUED and delivered by
  a hook, never typed into anybody's terminal, which is the line `internal/daemon/peers.go` draws and the one
  most likely to be simplified away by reusing the path that does type.

  A routed question arrives saying it is a question, whether the asker has STOPPED or is carrying on, and the
  exact command to answer it with. Without that last part a well behaved model writes a good answer into its own
  transcript, where nobody will ever read it.

  A handle nobody has REFUSES, and records nothing. Falling back to the board would be atrium deciding who
  answers, and the card would then claim a peer was on the hook when nobody was. The refusal comes back with the
  handles that would have worked, the same way a bad `tell` does. `atrium peers` now shows which sessions are
  stuck asking something, so the one row worth acting on is the one you can see.

  Asking a peer still moves a blocked card to waiting. It has genuinely stopped, and a stopped session hidden
  from the board because somebody else owes it an answer is worse than one shown as waiting on a named peer.
  What changes is what the card says.

- **A question a session asked is no longer stored where the operator's note goes.**

  `atrium ask` wrote into `why`, which is the field you write once and read in a week. So a question raised five
  seconds ago and a note typed last Tuesday were the same italic line under the title, and the reading was that
  somebody had left a strange note. Worse, the ask overwrote what the card was FOR, and nothing could put it
  back.

  They are two different claims. `why` is what this card is for and is still true tomorrow. An ask is what it
  needs right now and stops being true the moment somebody answers. An ask now has its own column, its own
  timestamp, and the handle of the peer it went to when it went to one.

  On the card it is drawn as a question: labelled **this agent has a question**, or **asked `<handle>`** when it
  went to a peer, in the warn tints everything else meaning "somebody is waiting" already wears, and never in
  the italic voice `why` owns. The distinction is in the DATA and not only in the stylesheet, which is what lets
  the rest of the board act on it: the waiting sort ranks a question you owe an answer to above one another
  session owes, above cards that merely stopped, and a desktop alert names which of those it is interrupting you
  for.

  **What counts as answering it.** A peer's `atrium answer`, and the operator saying anything to the card, which
  is the same act through the other channel. Both take the question off. Typing into the terminal does not:
  atrium cannot see that and never could, so a question answered that way stays on the card until the session
  finishes, which also clears it. A card in `done` that is still asking would make the one field meaning
  "somebody owes this session something" collect cards where nobody does.


- **The permissions queue on a phone, judged by narrowing a window rather than by reading the stylesheet.**

  The breakpoint that answers backlog item 11 was written and never watched. Opened at 390 pixels against a
  queue with three frozen agents in it, four things were wrong, and the first two were the kind that decide
  what your thumb does.

  **The four buttons sat above the question.** The 900px block puts `.actions` at order 1 and `.who` at order
  2, which is right where it was written for: a popped-out window is one card on one screen and the buttons
  are the only thing in it you came for. Inherited by a phone, the first thing under the header was `approve
  once`, with the command it would approve below the fold. Everything else in that block is about not pressing
  the wrong button. This was about not pressing any of them blind. The phone puts the question back on top.

  **The command box never grew, and had not since it was written.** `permCard` builds a detached card and
  autosized the box there, and a detached element reports `scrollHeight` as zero, so it wrote `height: 2px`
  inline and left it. What you saw was `min-height` doing the whole job: every command clipped to two lines,
  permanently, with the inline height beating any rule that would have grown it. A six line command showed its
  first two. It cost nothing on a desktop, where two lines is most commands, and it made the phone unusable,
  where two lines is none of them. Sizing now happens once the card is in the document, and again when the
  perms view is switched to, and again on resize, because a hidden element measures as zero exactly like a
  detached one and rotating a phone rewraps every command.

  **The announcements buried the queue.** Three toasts is the cap, and at 390 pixels a toast carrying a
  command wrapped to five lines and stood 194px tall, so the cap was 582 of an 844 pixel screen, drawn over
  the permissions list. One at a time on a phone now, clamped to three lines.

  **And a toast talked over the row it was about.** `notify` already refuses to raise an operating system
  notification while you can see the board, on the grounds that the toast has already told you. The same
  argument one level down: if the request's own row is in front of you, with its command and its four buttons,
  a floating copy of the first 120 characters is a panel drawn over the answer. On screen, not merely on the
  perms tab, because a queue of six on a phone is taller than the phone and the one frozen twelve minutes may
  be below the fold, where the toast is the only thing that would take you to it.

  That last one is three-valued and the third value is the whole of a bug. The alerting pass and the repaint
  are two separate requests, so on the first poll after a reload there is no list to measure yet. Reading that
  as "not on screen" put a toast over the card you were looking at, on every single reload, which is the one
  moment you are most certainly looking at it. It says `unknown` and waits for the next poll, and it does not
  claim the minute's alert on the way past.

  Also **`sw.js` awaited a function nobody wrote.** Press approve on a notification for a request somebody had
  already answered, and instead of the "too late" message, a `ReferenceError` inside a service worker and
  nothing on screen at all: the same silence that message exists to prevent, reached by another route.

  `scripts/check-phone.js` holds all of it, in the mould of `check-terminal.js`, and every rule names the
  failure it prevents. None of this is reachable from a parser: the markup stays valid and the script keeps
  running with every one of these broken, and what breaks instead is which button a thumb lands on. Each rule
  was watched to fail with its own bug put back.


- **Right click pasted into a terminal on loopback and did nothing at all on a share.**

  Reading the clipboard from script is a permission, and a browser grants it per ORIGIN. Loopback is one
  origin: the operator answered its prompt once and the browser remembered. Every share is a new origin with
  no answer on file, so the prompt goes up and `navigator.clipboard.read()` and `readText()` NEVER SETTLE
  while it stands there. Not a refusal, which would have been caught and said out loud. Nothing at all. The
  `await` in `pasteIntoTerm` waited forever, and right click on a shared board looked like a dead menu.

  Measured rather than guessed, on a real zrok share in Chrome and in Brave: the share is a secure context,
  `navigator.clipboard` is present, zrok injects no policy header and serves the page byte for byte, and
  `clipboard-read` reads `prompt` with neither call resolving. The same page on loopback in the same browser
  pastes in about forty milliseconds. `ctrl-v` and `shift-insert` were never the problem and are unchanged:
  those go through the browser's own `paste` event, which is a person handing the clipboard over rather than
  a page reading it, and no browser has ever asked permission for that.

  So the wait is now bounded, and what it ends in is a paste box. The box is a text field the operator
  presses `ctrl-v` into, which is the same clipboard arriving by the door that is always open. It takes
  files too, so a screenshot pasted into it uploads and hands the runner a path exactly as `ctrl-v` over the
  terminal does, and what leaves it goes through `sendPasteText`, so a multi-line paste is still bracketed
  rather than one Enter per line. `ctrl-shift-v` opens it on purpose. It is cleared when it closes, because
  what gets pasted into a terminal is often a secret.

  An origin that has been granted the permission never sees the box: the read resolves long inside the
  bound, and loopback behaves exactly as it did.


- **You can bring your own terminal theme, and edit any of them.**

  The sixteen ANSI colours per theme were a table baked into the board's page, so adding one was a code
  change and the fifty two that shipped were one person's. People have a colour scheme they have used for
  years, and the format they have it in is Windows Terminal's, which is where the shipped ones came from.

  Paste a `settings.json`, or its `schemes` array, or one scheme out of it, and the schemes in it arrive as
  themes. Comments and a trailing comma are read, because that is what the file Windows Terminal writes
  actually looks like. `One Half Dark` arrives as `one-half-dark`. Windows Terminal calls magenta `purple`
  and atrium moves it to the slot xterm reads, which is the one mistake in the conversion that looks like
  nothing at all when it is made by hand.

  The editor is twenty one boxes, each a swatch and a hex, and it paints the attached terminal as you type,
  so a real session is the preview whenever there is one. When there is not, a block of the palette in the
  dialog is. It is on the terminal bar beside the theme picker, and in the gear under **the board** for when
  nothing is running yet.

  A theme saved under a name atrium ships shadows it, which is how somebody gets THEIR dracula, and deleting
  theirs puts atrium's back. Deleting a theme that cards are wearing leaves those cards on their project's
  colour rather than rewriting them.

  **Where a theme lives, and why it is not `localStorage`.** A grouping expression is refused daemon-side
  storage because it is compiled and run by whichever browser loads the board. A colour is not code, and the
  validator that keeps it that way is total: six hex digits behind a hash, no second grammar, checked at the
  point of storage rather than at the HTTP boundary, so the editor, the import and a restored configuration
  all go through it. The name was already stored on the card, so a palette kept in one browser would have
  been a card with a colour there and the project default everywhere else.

  Brought themes ride along in the configuration export, so a machine rebuilt from a checkout comes back
  looking like itself.

  One bug fell out of writing it: the theme table was an object literal, so a card whose theme was set to
  `constructor`, `valueOf` or `toString` handed xterm a function to read colours off. Reachable from the
  fixture dialog since there was one. The table now inherits nothing, and the daemon refuses those names as
  well.


- **A colour that is invisible on one of twenty one skins is now found by a script instead of by somebody
  hovering it.**

  `scripts/check-contrast.js` parses every palette out of the stylesheet, composites each `rgba` lift onto the
  surface it actually sits on, and scores 1176 text-and-background pairs against a floor. `scripts/ci.sh` runs
  it beside the skins check. The skins check says every skin sets every variable. This one says the values are
  legible.

  Compositing is the whole difficulty. A hover is `rgba` over a backdrop, so `rgba(255,255,255,.05)` compared
  against a text colour scores wonderfully and describes no pixel on the screen. Deleting the compositing from
  the script turns 338 pairs red, which is how you can tell it is doing the work.

  It also asserts something a floor cannot: **a hover may not read as less prominent than the colour it
  replaces.** A hover that changes lightness in a fixed direction is wrong on half the skins, so the rule is
  not about the direction, it is that the pointer has to make the thing more legible against the surface it is
  on, whichever way the skin points. The hover in the self test that fails this scores 4.1, well over its
  floor, against a resting colour scoring 6.8.

  The hue-derived colours are read out of their rules by selector rather than copied into the table, because a
  table of copies keeps checking the old colour after somebody edits the stylesheet. A selector it names and
  cannot find is an error, not a skipped check.

  And it tests itself on every run: seven colours broken on purpose, each asserting the check catches it and
  names the right pair. A check that cannot fail is worse than no check, because it is also a claim that the
  colours were looked at. This one passed on its first run against a palette with two real defects in it.

- **The pinned group's heading was under the floor on `linen`, and thin on the other three light skins.**

  Found by the contrast check on the day it was written. The heading was the raw `--warn`, a mid amber, on the
  loudest wash on the board: 2.73 on `linen`, and 2.95 to 3.08 on `paper`, `daylight` and `frost`. It is now
  blended toward `--head`, about 4.9 on `linen`, which darkens it on a light skin and lightens it on a dark
  one and still reads as amber.

  The check was wrong about this first, and the correction is the more useful half. It reported 1.96 because
  it swept the pinned block through all twelve group hues, and the pinned group does not have a hashed hue:
  the markup fixes it at `--ghue:41`. So the first number came from a hue that group cannot have. A pair that
  sweeps a value the markup pins will invent defects, and a check that cries wolf is a check somebody switches
  off.

  A second defect of the same kind is recorded and not fixed: at rest, a project or stack group's NAME hits a
  ratio of 1.00 against its own block on those four skins. Fixing it is a choice about what a group name may
  look like rather than a value, so it is `docs/css-nits.md` open nit 4, and the check holds it pinned at the
  ratio it scores today. A pin fails in both directions: make it worse and it fails, fix it and it fails and
  tells you to delete the pin.


- **A switcher, on a keystroke.** `ctrl-shift-k` opens it over whatever is on screen. Type a few letters
  against the title, the directory or the tags, Enter goes, Escape closes. The last few sessions come first, so
  the common case is the key and Enter with nothing typed.

  The keystroke is a setting with a default, not a constant, and that is the whole design. A browser keeps a
  handful of accelerators for itself, the handful differs per browser, and a page asking to keep one is
  sometimes refused SILENTLY, which ships a feature that works on the machine it was written on. The default
  survives Chrome, Edge and Brave, and it is not `ctrl-k`: that one is readline's kill-line, and a board made
  of terminals may not quietly take it. Firefox takes `ctrl-shift-k` for its Web Console, so settings says so
  and offers to rebind. When a browser takes the bound key anyway, atrium notices, because the focus leaves
  the document, and says which key to change.

  **It works inside a popped-out window,** which is the half that is not a list. A solo window IS one card, so
  switching there releases the old claim, claims the new one, renames the window and rewrites its address.
  Skipping the release leaves the board believing a card is popped out in a window that has moved on, and
  claims are fifteen second heartbeats, so that mistake heals itself and presents as a flicker somebody
  diagnoses as something else. The board now yields its pane when another window claims the card it is
  showing, and drops the window handle when a claim is released. `docs/switcher-design.md`, and
  `scripts/check-switcher.js` holds every one of those as a rule.


- **A path in the terminal is a link, and the daemon is what decides which words are paths.**

  `POST /v1/tasks/{id}/files/probe` has been there and unused since it was written. The board now calls it: on
  hover it takes the words on a row, sends the ones that could be a path, and underlines whatever comes back as
  a real file inside that card. The answer is cached per card, misses as firmly as hits, so a word that is not a
  file is asked about once rather than once per line it appears on.

  Not underlining everything is the whole problem, and it is why nothing on the board judges by looking. A
  terminal is full of things shaped like a filename and not one: `v2.1.263`, `zrok.io`, `foo.bar()`, `Opus 5`.
  Every one of those is offered to the daemon and refused, and the rule stays "it is a link if it is a file",
  which also keeps `Makefile` clickable. Punctuation is taken off first, so a quoted path with a comma after it
  and the `internal/api/api.go:248:1` a compiler prints both resolve to the name underneath.

  Clicking opens **atrium's own viewer, in the browser doing the clicking**, and never `files/open`. That one
  starts an editor on the daemon's machine, which is right for the `open there` chip that says so and wrong for
  everything here: the board is meant to be driven from another machine over a share, and a window opening
  beside the agent is a window in front of nobody. A directory opens the file drawer instead. `check-terminal.js`
  now fails if that ever gets rewired, because on the machine the daemon runs on both are the same machine and
  both look correct.

  A wrapped path is one link. Rows are joined back into the line they came from before anything is probed, so a
  filename broken across two rows is still a filename rather than two halves that are not files.

- **A URL in the terminal is clickable, which it never was.**

  The board prints its own address at people. `http://localhost:7778/#term=<id>` could be pasted into a bar and
  could not be clicked where it was written, because atrium vendors the fit, webgl and search addons and never
  vendored `web-links`. It is vendored now, at 0.11.0, from the same wave as the rest, and loaded the same way:
  from disk, never a CDN, with the same one-sentence console error the search addon has for a bundle that did
  not arrive. A URL opens in a new tab with `rel="noreferrer noopener"`, and never through the file viewer.

  Shipped together with the path links above because they are two link providers on one terminal and the
  ordering between them is a real contract. xterm asks every provider about a row and then drops links that
  overlap something an EARLIER provider claimed, so registration order decides who owns a run of text both
  matched. The web-links addon registers first, so a URL wins over a path found inside it. Reversed, the mistake
  does not look like one: the link is still drawn, and clicking it opens atrium's file viewer on something that
  was never a file. `check-terminal.js` fails if the two are ever swapped.


- **Every codex hook atrium has ever written fails if the atrium binary sits under a path with a space.**

  Claude Code hands a hook command to a shell, so the path is quoted and the shell strips the quotes. Codex
  takes the FIRST WORD of the command as the program and does no quote handling on it at all, so the same
  quotes make it look for a program whose name begins with one. Two runs differing by two characters, one
  `hook: SessionStart Completed` and one `hook: SessionStart Failed`, and the failure is a single line in a
  runner nobody is watching.

  The writer now asks the target how it reads a command instead of assuming a shell. Quotes on the arguments
  are kept, because codex honors those; it is the program alone.

  There is no third spelling, so a path with a space in it means codex cannot be wired on that machine at all.
  Atrium refuses before it touches the file and the board says why on the row, rather than offering a button
  that writes hooks which then fail one at a time.

- **Installing codex's hooks wrote claude's command line into codex's file.**

  `upsert` took the target it was installing for and then asked `HookCommandFor` and `reportsEvent`, both of
  which are the claude ones. It agreed by accident for as long as the two runners wanted the same events with
  the same subcommands behind them, and would have started writing the wrong line into the wrong file the
  moment either one said something the other does not.

- **A codex session says it is codex.**

  The session hook posted `"runner": "claude"` as a literal, whoever ran it, so a codex card came up wearing
  claude's colour on the board and offering claude's resume. `atrium session` now takes `--runner`, which the
  codex hooks file carries, and prefers `ATRIUM_RUNNER`, which a launched session is told. The environment
  wins because it names the harness ROW: two rows can both run codex against different models and both read
  the same `hooks.json`, so the file's answer is the right default and never the better one.

- **A codex hook entry that does not say it is codex now reads as pointing elsewhere.**

  Drift the path check cannot see. An entry written before atrium had a second runner is the right binary and
  the right subcommand, and it reports `claude` whoever ran it. Without a second check it reads as wired, is
  not counted as missing, and the board never offers the one button that would correct it, so every codex card
  keeps coming up as claude with no way to find out why.

  Narrow on purpose: it asks only whether the command names the runner, and only for a runner that needs
  naming. Claude entries do not move, because every `settings.json` already installed says nothing about a
  runner and marking those stale would report six working hooks as broken. Anything else somebody wrapped
  around the binary is theirs and is left alone.

- **Codex can be resumed, and its row said it could not.**

  `codex resume <SESSION_ID>` takes the same uuid codex hands every hook in `session_id`, which is the id a
  card already records. The seeded row shipped with an empty `resume_args` and a note saying to confirm the
  flag first, so a codex card has been carrying a working resume id and refusing to use it. Migration
  `0039_codex_resume` fills it in, guarded on the empty list so an operator's own value survives.

- **Resume arguments that never mention the id are refused instead of resuming somebody else's work.**

  Found by running the migrations against a copy of a live database, which is what
  `internal/store/CLAUDE.md` says to do: codex on that machine was configured as `resume --last`. That runs, it
  succeeds, and it picks up whichever codex conversation the machine saw last. The card's own id is dropped on
  the floor and nothing anywhere says a substitution did not happen, so the terminal comes up holding work the
  card has nothing to do with.

  A launch that is resuming now checks that some argument contains `{resume}` and refuses with the arguments it
  was given if none does. Refused rather than corrected: which spelling a runner wants is the operator's to
  write, and a launcher that guessed would be inventing a command line for a program it knows nothing about. A
  plain start is untouched.

- **Codex reports a compaction now, and the board stops offering it a hook codex does not have.**

  `PreCompact` is added to the codex set; it is the same `atrium session --event compact` behind it. Codex has
  no `Notification` and no `PostToolUseFailure`, so those two claude rows have no codex row rather than
  appearing as permanently missing.

- **The runners table stops implying that every row can report.**

  Only the claude row ever carried a hooks button, so the codex, ollama and shell rows carried a blank where
  the count goes, which reads as wired. A codex row now carries its own button over its own file and its own
  count. A row atrium has no hooks target for carries `reports nothing`, with the reason on hover: no
  activity, no session lifecycle, no resume id, and none of that is coming, because the runner has nowhere to
  say it from.

  `docs/other-runners.md` is what each runner was actually measured to offer, including the two that offer
  nothing and the one that was not measured.


- **Paste a pull request URL and get a launch dialog that already knows what it is.**

  Every session used to begin with somebody pasting context by hand: find the worktree, type the path, type a
  title, retype the link into a first instruction. A recogniser is a row that does all four. It is a regular
  expression with named groups and a set of templates over what it captured, and it turns a URL into a filled-in
  form.

  **Nothing about GitHub is in the binary.** A recogniser is a pattern and a mapping, and whoever wrote the row
  did the understanding. That is what lets this serve a ticketing system nobody has thought of yet, and it is
  why the table ships empty rather than with a helpful default. `scripts/recognisers` has working rows for
  GitHub and Bitbucket, and a script that loads them.

  The table is ordered, most specific first, and the first row that matches wins. A specific "pull request"
  pattern sits above a generic "any repository" one, or the generic one swallows it. The row at the bottom is a
  shrug rather than an error: a URL on a host you know that matches nothing specific is still worth a dialog.

  **It makes no directories, and atrium is not learning git.** `cwd` is a template that produces a path. Where
  the worktree is not there, the dialog says `... is not here yet. make the worktree, then start it` and stops.
  A tool that cloned here would be a second, worse implementation of something already on the PATH.

  A hole is left standing rather than blanked. A template reading `{branch}` with nothing to put in it produces
  a field that still says `{branch}`, because `feature/{branch}` blanked becomes `feature/`, which is a
  directory somebody creates by accident.

  Three places take a URL: the launch dialog's new link box, `atrium open <url>`, and an offered card in the
  inbox, whose link is put in that box ready to press.

- **A recogniser's optional `fetch` command turns a number into a title, and holds no credential.**

  The pattern gives you an issue number. Turning that into a title needs an authenticated network call, and
  atrium does not make one: it runs an argv the operator wrote, exactly as a source does, and reads a JSON
  object of facts off stdout. Every key in it becomes a variable the templates can read. `gh` already has a
  token, in the keyring it already uses, and there is nowhere in a recogniser row to put one.

  **The captures win over the fetched facts.** A fact only fills a name the pattern left empty. A fetch reads
  whatever an issue tracker holds and anybody can write into an issue tracker, so one that could redefine
  `repo` could move `cwd`, which would mean the contents of an issue chose the directory a runner starts in.

  A fetch that fails is reported on the row and is never fatal: the URL still resolves from its captures, and
  the dialog says why the title is thin. It does not switch the row off, which is deliberately unlike a source.
  A source is a timer nobody is watching. A recogniser runs because somebody just pasted a link with the board
  in front of them, and a row that switched itself off would make the next paste silently match nothing while
  they watched.

- **The launch dialog has a tags field, and carries what a recogniser knew.**

  `--tags` has been on `atrium launch` since it existed and the board had no way to set them. It does now, and
  the repo, org, host, branch, window, theme and origin a recogniser worked out ride along with the launch
  without needing a box each: they are not things to type, they are what a launcher knew.

- **Recognisers are exported and imported with the rest of the configuration.**

  The understanding of somebody else's ticketing system lives in a template rather than in the binary, which
  makes it exactly the kind of thing worth reviewing, diffing and keeping in a repository. An import compiles
  each pattern, so a hand-edited file with a broken expression fails naming the row rather than landing a row
  that can never match.


- **A room can now be added, read and forgotten from the board, without the board learning to store one.**

  The rooms pane drew what turned up and nothing else. Adding a room meant knowing the flags, knowing this
  hub's ziti service name and typing both on the other machine from memory. A room that had gone said
  "not heard from since" and left you to work out whether that meant wait or go and look. A name left behind
  by a test sat there for ten minutes with no way to clear it.

  `add a room` writes the command out instead. It reads what this hub can say about how to be reached, which
  is the ziti service it is answering on, a public zrok address when one is up, and this machine's hostname
  and port, and builds an `atrium room` line with the service filled in and the identity path, the name and
  the room's own board address as fields. Nothing is saved. There is no pending room and there cannot be,
  because a room exists exactly when it checks in, so the dialog is a text generator and closing it loses
  nothing.

  A stale room now carries its deadline: what is listed is what it last said, and it drops off in so many
  minutes unless it checks in. `GET /v1/rooms` reports `forget_in_seconds` per room so the page is not
  holding a second copy of the constant and drifting from it.

  `DELETE /v1/rooms/{name}` forgets one early, and it deliberately writes nothing. A machine still running
  `atrium room` reappears on its next heartbeat, and the confirm says so. It is the same operation time
  performs at ten minutes, done now, for the machine that was reimaged or renamed. Making it stick would
  mean the hub holding a durable record of a refusal, which is the second source of truth the whole design
  exists to not have, and there is a test named after that.


- **A permission request on another machine is answered from one board, and the agent there carries on.**

  Rooms landed as a status page. Another machine ran its own atrium, dialled this one, and its cards appeared
  on one board. What it could not do was the thing the board exists for: an agent on a room that was frozen
  mid-tool waiting to be allowed something was invisible unless somebody opened that room's own board, which
  is exactly the trip having one board is supposed to save. On two machines that is a nuisance. On five it
  means an agent waits an hour because nobody thought to go and look.

  A room now reports what it is waiting to be allowed, and those requests are drawn in the same perms queue
  as the local ones, ordered oldest first across both, because who has waited longest is the question and
  which machine they are on has nothing to do with it. Every part of the alerting loop counts them: the badge,
  the window title, the desktop notification and the widening nag. A frozen agent on a cloud instance now
  rings the same bell as one in the next directory.

  **The channel does not move, and that is the whole design.** The request is blocked on a channel held in
  that room's own process. Nothing here can reach it, so nothing here tries: the decision is queued, the room
  collects it on its next check-in, and the room posts it to its own daemon, which unblocks its own request.
  The hub answers nothing on the room's behalf, records no decision, and never learns whether the agent moved.
  Approving on a room's behalf from here would be a second source of truth about whether a command ran.

  **Always and never work, and the rule is written on the machine that was asked.** That is the only place it
  could mean anything, because that is the daemon that will be asked again and the only one whose rule table
  is consulted when it is. The card says so, above the command, along with which machine the command would run
  on. A queue that merges two machines and does not say which is a queue nobody can safely press approve in.

  A room with somebody frozen on it checks in every two seconds instead of every twenty, because the reply to
  a check-in is the only thing travelling in that direction and how often it asks is how fast an answer
  arrives. At the ordinary heartbeat, approving something would take up to twenty seconds to release the
  agent, which reads as a button that did nothing.

  Nothing acknowledges, on purpose, and the failure that needed designing is the quiet one. A room can refuse
  a decision: its own board may have answered first, or its store may have halted. So an answer that has been
  handed over and has not made the request go away expires after two minutes and the request becomes
  answerable again. A decision that hid a request forever would leave an agent frozen on another machine with
  nothing on any board to say so, which is the failure this whole feature exists to remove.

  The wait travels as seconds rather than as a timestamp, measured on the clock that recorded the request, for
  the same reason `wait_seconds` on a card does: two machines that disagree by a minute would otherwise draw a
  request as frozen before it was made. A report is cut to fifty requests and four kilobytes of diff each,
  cut on the room and again on the hub, because a report refused for being too big would take that machine's
  cards off the board with it.

- **A remote card's terminal is one click away, on the board that owns it.**

  A pseudo terminal cannot leave the machine that made it. That is a fact about ConPTY rather than a policy
  and it is not going to change. What was missing was the link: the room already reports where its own board
  is, and the board already opens a single terminal at `#term=<id>`, so a remote card's title is now a link
  that joins the two. It goes to that machine's board with the right terminal already open, which is the
  nearest thing to attaching that exists here.

  Offered only for a card with a runner in one of the three states somebody would want to type into. A
  finished or dead card has nothing behind it, and a link that opens an empty terminal pane reads as broken
  rather than as a card with no session.


- **A card can say how much context its session has burned, and how close the account is to a limit.**

  The board could not tell which of sixteen agents was about to compact, and compacting is where a session
  forgets what it was told at the start. That is the most useful single input to "which one do I interrupt",
  and atrium could not see it: hooks carry which tool started and which subagent ended, never a token count.
  Exactly one thing on the machine gets that number, and it is the statusline script Claude Code hands its
  per-session payload to.

  So there is now a `POST /telemetry` on the agent listener for it to post to, keyed by the harness's session
  id, which atrium already stores as the card's resume id. The card grows a `ctx 92%` chip, quiet under sixty
  and in the danger colour above eighty five, with the tokens and the model in its tooltip, plus a `5h` or
  `week` chip when a rolling limit is at eighty percent or more.

  Never stored, for the reason in `docs/activity-design.md`: it is a fact about a process that is running
  right now, and written down it would outlive the session it described. It sits in memory beside the activity
  and dies with the daemon. It expires more slowly than an activity does, thirty minutes against fifteen,
  because context only grows, so an old figure is a floor on the current one, and an idle card is exactly the
  one whose context decides whether you resume it.

  The endpoint follows `/activity` line for line: it answers before it reads the body, it swallows a session
  nobody has heard of, and nothing it can be sent produces a non-2xx. A statusline renders many times a
  second, so it also enforces a two second floor per caller, well under the ten to fifteen seconds the
  contract asks for. A statusline post is not activity and does not move the card's idle clock: a terminal
  redraws when a human types in it.

  The statusline script itself lives in another repository. `docs/statusline-telemetry.md` is the contract,
  written to be implementable without a conversation.


- **The zrok panel says what the account is already using, before you press a button that fails.**

  A zrok account has a ceiling on environments, on shares open at once, on reserved names and on how much
  traffic it carries in a period. The way you found out you were at one was a button that thought for several
  seconds and then refused inside somebody else's API.

  The panel now reads the account, above the configure fold, and counts the three things the zrok controller
  compares before it refuses: environments, shares open everywhere, and reserved names. It says how many of
  the shares are this machine's and how many of the reservations are atrium's, since atrium takes one per
  board address and one per lent session and can fill an account on its own. When zrok reports the account as
  limited the block turns into a warning, because that is not a forecast: the controller checks it before
  allocating anything, so the next start is already refused. That flag is the TRANSFER allowance and nothing
  else, which the warning says, because sending somebody to delete shares over a bandwidth block wastes their
  time and does not lift it.

  **There is no denominator and that is deliberate.** zrok does not tell an account token where its ceilings
  are. They sit on a limit class only an admin may read, and an account with no class applied is measured
  against the controller's own configuration, which no endpoint exposes at all. "6 of 10 shares" would be a
  number atrium invented, and a number somebody plans around that is not real is worse than no number.

- **A zrok account at its name limit was told to pick a longer name.**

  The error messages were built on the belief that zrok answers a limit with `429`. It does not. Read from the
  2.0.4 controller, a share limit and an environment limit both come back as `401` with no body, and a name
  limit comes back as `409` carrying `names limit reached`.

  So both branches pointed the wrong way. A share refused for a limit read as a revoked token and sent people
  to re-enable an environment that was fine. Worse, reserving a name swallows a `409` on purpose, because a
  name that already exists is the ordinary case on a second press, and it was swallowing the limit refusal
  too: the second call refused for the same reason and the message that came out was "that name is taken, try
  a longer one", to an account where no longer name would ever have worked.

  A `409` carrying `limit reached` is no longer read as a name that was already there, and it now says to
  release one. The `401` names both of its causes rather than picking one, and points at the account block,
  which is the thing that tells them apart.

- **The zrok demo instance's `POST /share` 500 is characterised, in `docs/zrok-share-500.md`.**

  Not an atrium defect, and it is why the reserved-share work is built and unproven. Bisected against the live
  instance: `POST /share` fails for every share, but the requests that stop earlier all answer correctly, and
  one of those, the private-share-token availability check, is a READ against the ziti controller that returns
  the right conflict. So the zrok controller authenticates to ziti and reads from it, and what fails is inside
  resource allocation, which opens with `ziti.Configs.Create`. Which step inside allocation gave up needs the
  controller's log, and the document says so. It is written to be filed upstream and has not been.

  It also names a second, smaller upstream defect: the `500` on that operation carries no payload at any of
  the eight places it is returned, though the spec declares one and the handler's own `409`s do carry a
  sentence. That is why the body is `""` and why a client can report nothing.


- **The published board's login now carries PKCE, and it renews itself without holding anything.**

  Two of the three things the login shipped without. The third, roles, is a design question and there is now a
  proposal for it in `docs/overlays.md` and no code.

  **PKCE, on every login, with no switch.** The authorize request carries an S256 challenge and the exchange
  carries the verifier behind it. State only ever proved that a callback belonged to a login this board
  started. A code lifted out of a callback URL, a browser history, a referer or a proxy log was spendable by
  whoever held it, because the token endpoint could not tell that the caller was not this board. Now spending
  one needs a secret that never left the daemon's memory. It is on for a confidential client too, since a
  provider that does not implement PKCE ignores the extra parameters, so there is nothing to configure and
  nothing to forget.

  **Renewal asks the provider rather than remembering.** A refresh token is a long-lived credential belonging
  to the person who signed in, and keeping one would be atrium holding somebody else's credential, which is
  the rule this feature was shaped around. So there is no stored token. A browser with no usable session goes
  to `/auth/renew`, an ordinary authorize request with `prompt=none` on it, which asks the provider to answer
  from the session it already has or refuse without showing anybody anything. A live provider session comes
  back as a new cookie through a redirect nobody sees. A provider that has forgotten the browser answers
  `login_required`, and that is a normal answer rather than an error: the callback recognises a declined
  renewal and sends the browser to a real login form, carrying the page it was going to.

  Two things follow. A first visit tries silent sign-on before it shows a form, so somebody already signed in
  at the provider never types anything. And the board, which is one page that never navigates, now turns a
  single `401` into one trip through the renewal endpoint, because a redirect is not something a `fetch`
  follows and the page was otherwise just filling up with failures.

  A session is still twelve hours, and where somebody lands after signing in is now the page they were on
  rather than the front page. That value travels through the provider and back, so it is refused unless it is
  a path on this board: `//elsewhere.example` looks like a path and is not.

  The login states this board holds are also capped now. Starting a login is unauthenticated by definition, so
  each one was an entry anybody who could reach the board could ask for, held for five minutes, with no bound.


- **The board told you it had raised a window, in the window it had just taken you out of.**

  Attaching to a card that is already popped out raises that window and says so. The toast was drawn in the
  document that was clicked, which is the board, which is the one now going behind the window it raised. The
  message was always over your shoulder by the time it appeared. It now crosses to the raised window on the
  `atrium-solo` channel and is drawn there, which is where you are about to be looking. Locally only when the
  browser has no `BroadcastChannel`, where behind you beats nowhere.

- **The board cannot raise a window it did not open, and it used to claim otherwise.**

  A popped-out window is found again by name, and only a window a script opened has one. Paste a `#term=<id>`
  url into a tab and that window claims its card on the solo channel, so the board knows it exists, and can
  never reach it. The board would then open a SECOND window onto the same terminal, which is the thing
  `docs/supervision-design.md` says nothing arbitrates, and announce "it was not there any more" about a window
  still on screen.

  A failed lookup is now two situations rather than one, and they are told apart by asking. The board calls the
  roll and waits a third of a second: a window that answers is out there and unreachable, and it says so and
  opens nothing. Silence means the claim was a few seconds stale, and it opens one, which is what it always
  should have meant.

- **Clicking an alert for a popped-out card stole its terminal.**

  `attachTask` refuses to attach a card that is in a window of its own, for the same reason popping out
  detaches the board's pane. The path a toast and a desktop notification land on had no such guard: it attached
  anyway, pulled the terminal out of the window holding it, and reported success. It now raises that window
  instead, like every other route to a session.

- **The notification test said nothing when the notification never appeared.**

  With a service worker there is no object handed back and no `onerror`, so the button returned in silence
  whether Windows drew the notification or swallowed it. Silence reads as working, and it was silent in exactly
  the case the button exists for. It now asks the registration what is on screen and reports what it finds.


- **A terminal you scrolled up jumped to the bottom when the window lost focus.**

  Nothing scrolled on purpose. `onTermResize` calls `fit()`, `fit()` hands new rows and columns to xterm's
  `resize()`, and xterm clamps the viewport to the bottom whenever the row count changes. Every layout event
  runs it, and a focus change is one: a scrollbar appearing, the window manager, the board's chrome settling.
  A position set by hand was thrown away by an update nobody asked for, which is the board-repaint complaint
  on the other surface.

  Two guards. A fit that changes nothing is no longer treated as a resize, which covers most of it, since
  moving focus does not usually change the size at all. A fit that did change something restores where you
  were, measured as **distance from the bottom** rather than as an absolute line: a width change reflows the
  buffer, so the line you were reading has a different index afterwards and there is no exact answer. The rows
  between you and the end are few and the rows above are many, so anchoring to the end lands closest.

  Being at the bottom already is left alone, and typing still pins the view down for a moment through
  `followScroll`. Stay put until I type.

- **The board threw away where you were, on every event.**
  Every update repainted wholesale. `setHTML` assigned `innerHTML` for a whole list, so every node under it was
  destroyed and rebuilt, and a browser has nowhere to put the scroll position of an element that no longer
  exists. The view went back to the top. That fired on every SSE event, and with sixteen live agents an event
  arrives constantly, so scrolling down was something you could not finish doing. Scroll was the loudest
  symptom and not the only one: the same swap dropped a text selection mid-drag, moved focus, and restarted
  every CSS transition, which is why the board looked like it flickered under load.
  `setHTML` reconciles now, and every list on the board goes through it, so the board, the stack, the terminal
  switcher and every list in every dialog all changed at once. Two tiers, and the second is what makes it hold.
  The container is never rebuilt: rows are matched by key, so anything untouched keeps its DOM and the scroll,
  the selection and the focus survive with it. And a matched row is not rebuilt either. It is walked, and only
  the attributes and the text that actually differ are written, so the age ticking on a card two chips away
  does not kill a selection over its title.
  Tier one on its own would have been a fix that looks complete and is not. The age changes every second, so
  the card being destroyed is the card being read: the scroll survives and the selection dies anyway.
  Preserving `scrollTop` around the swap was the previous answer here and it is gone. It fought the browser
  every frame, did nothing for selection or focus, and went wrong whenever the content above the viewport
  changed height.
  Two consequences worth knowing. A card that was already there is now THE SAME ELEMENT after a repaint, with
  its listeners still on it, so `wireDragging` guards against wiring a node twice: a `drop` handler added once
  per event would file the same move repeatedly against ranks that had already changed. And the file picker's
  ticks survive a repaint, because attributes are written and a dirty checkbox stops reflecting them.
  `scripts/check-morph.js` and `scripts/test-morph.js` run from `check-board.sh`. The first checks that the
  shape holds, including that every reconciled row still carries `data-id`. The second runs the reconciler
  against a small DOM and asserts that a row nobody changed comes out the same object, never written to.
  `docs/board-repaint.md` has the design.

- **Multi-tenancy was decided rather than deferred again.** `docs/multi-tenant-decision.md`.
  No behaviour change. Backlog 3 was parked on three objections and two of them moved, so it needed new reasons
  or none. The answer is a fork rather than a flag, for a stronger reason than the standing note gave: a flag is
  a `WHERE` clause, and the permission chain, the reaper and the supervisor all decide with no caller identity
  to put in one. `authGuard` verifies an OIDC subject and discards it, the agent listener mints a card for any
  name off the wire, and every runner starts from the daemon's own `os.Environ()` as the daemon's own user.
  The smallest tenancy boundary that is not a lie is an operating system user, which is one whole atrium. The
  recommendation is neither fork nor flag yet: one daemon per person, rooms aggregating them, which is backlog 1
  and already half built. Postgres is sequenced after that decision rather than before it, because everything
  two active daemons fight over is outside the database.
- **The Postgres claim was checked, and it is the schema that survives.**
  `internal/store/schema.go` has said since `0001` that it was written to stay Postgres portable, and nothing
  had ever run it there. It has now run there. All 80 statements across all 38 migrations apply to PostgreSQL
  17 unmodified and in slice order, and all 91 queries parse and type-check once `?` becomes `$n`. The claim
  holds.
  What does not hold is everything around the SQL. The migration runner tolerates a duplicate column by
  matching a SQLite error string, and its `continue` cannot work inside a Postgres transaction at all, because
  a failed statement there poisons the rest of the migration and the row that records its name. The halt
  treats every error that is not lock contention as permanent, which is right for a file and wrong for a
  socket that goes away and comes back in two seconds. Four read-then-write pairs depend on
  `SetMaxOpenConns(1)` for correctness rather than throughput, and `Offer` racing would halt the daemon on a
  unique violation raised by the index built to prevent exactly that.
  No code changed and no driver was added. `docs/postgres-probe.md` is the writeup, with the error text for
  each one and what backlog item 10 should say now.
- **`atrium preview` was written down.** No code change.
  A second board, on a copy of the cards, with its own ports and its own address file, so a change to
  `index.html` can be judged by using it rather than by restarting the daemon every live session is attached to.
  It shipped documented nowhere, which meant the next person to want two boards would have invented it again,
  and the first version of this one spawned the operator's fixtures and went after their reserved zrok name from
  a process that was supposed to be a window.
  `docs/preview-design.md` has the feature and the two rules that make it safe: its own `--location-file`,
  because every hook on the machine reads that file and the last daemon to write it takes all of them, and
  `Options.Passive`, because everything in a copied database is real and an ordinary start acts on it.
  The same file answers the question that produced it. Several atriums on one database is not blocked by sqlite,
  which allows it. It is blocked by both of them ACTING: fixtures, overlay names, the address file, the
  migrations and the pty are all machine-wide and none of them is mediated by a database, so Postgres changes
  nothing about it. `Passive` is one writer and any number of readers, and that is exactly as far as it
  generalises.
  Pointers from the README subcommand table, `docs/user-guide.md` as pattern 11, `docs/overlays.md` where the
  share is not re-bound, and `docs/supervision-design.md` where the terminal cannot be. `docs/test-plan.md`
  gains section L, written as the damage a preview must not do.
- **One command cuts a release, and it refuses by default.**
  Everything needed to publish already existed as three scripts and a document with six commands in it, which
  meant the release was six chances to run something out of order and a set of questions somebody had to
  remember to ask at the worst moment. `scripts/cut-release.sh` is the one entry point and it asks them itself.
  A dry run is the DEFAULT and it is not a preview: it compiles all five platforms, builds the deb and the rpm,
  runs the binary it just built to check `atrium version` reports the tag rather than `dev`, rebuilds the
  commit in a clean `git archive` export and compares the binaries byte for byte, re-hashes every artefact
  against `checksums.txt`, and writes the scoop manifest with the version, URL and hash filled in. Then it
  prints the `gh` command it did not run. Nothing leaves the machine without `--execute`.
  Five refusals, each with its own exit code so a caller can tell them apart, and only one of them waivable. A
  dirty working tree, because a release script that will happily publish uncommitted work is the one that
  eventually does. A tag that already exists, because a tag is the only name a release has. A binary stamped
  with a version that is not the one being released. A build the tagged commit does not reproduce, which is how
  a file nobody committed gets into an artefact. A checksum that does not match. `--skip-ci` is the waiver, and
  it exists because CI is the one gate that also runs somewhere else.
  `scripts/check-release.sh` asserts every refusal against a throwaway git repository using `--preflight`, so
  it needs no toolchain and no network and runs in about a second. It is in `scripts/ci.sh`. It compares the
  exit CODE rather than checking for failure, because a test that only wants non-zero passes when the script
  refuses for the wrong reason.
  `.github/workflows/release.yml` now has one script in it instead of three, called with `--from-tag`, which is
  the flag that makes the same code correct in both places: run by hand it creates the tag you named, run on a
  runner it builds the tag whose push started it and checks that tag really is the commit checked out.
- **Attaching to a session that had been running a while showed an unreadable screen.**
  The cause is width, not corruption. A session launched with nobody attached composes its output for the size
  its terminal was opened at: hard line breaks at that column count, boxes drawn to it, absolute cursor moves
  worked out for it. Attach a wide window an hour later and the pty is resized, so everything drawn from then
  on is right, but the daemon replayed the retained buffer first and those bytes were composed for the old
  width. Against a wider grid they do not come out ragged, they come out on top of each other: sixty column
  text in a two hundred column window, diff output overlapping itself, and two thirds of the window empty.
  The width is now recorded with the bytes. Every change of the agreed size leaves a mark in the ring buffer,
  and an attaching viewer is sent the trailing run of output composed at the width its terminal is at now.
  Anything older is left out, and the terminal says so in one dim line rather than leaving a near empty screen
  to explain itself. Nothing further is needed to fill it: output is only dropped when the width just changed,
  and a width change is a resize the runner is told about, so it is already repainting.
  The size is also read before the backlog is written. A viewer's size arrives as its first frame, which is
  after the socket is up, so the daemon used to decide what to replay and then resize underneath what it had
  just sent. An attach now waits briefly for that frame first.
  A terminal is opened at 120x30 rather than at whatever the platform defaults to, which on Windows is 80x25.
  Recording the width the first byte was composed at only works if that width is something atrium chose.
- **A snapshot of a wrapped ring buffer could begin halfway through an escape sequence or a rune.**
  The write cursor is a byte offset with no idea what is at it. Once the buffer had wrapped, the oldest
  retained byte could be the middle of a cursor move, whose introducer is then gone, so its tail arrived at the
  terminal as printable characters typed across the screen. A severed multi byte rune had the same shape.
  A snapshot that does not start at the beginning of the stream now starts after the first line ending it
  finds, which cannot fall inside either. Losing a line beats shipping a broken escape. Fixed on the way out
  rather than on the way in, because the buffer has to keep taking bytes as fast as a runner produces them.
- **A card could say `dead` while its runner was alive and working.**
  A daemon restart kills every supervised runner and files its card dead, which is correct. The cards are then
  started again ONTO, with `task_id` on `POST /v1/launch`, and the pid on the card was updated while the status
  was not. So the board drew a dead card, the sweep archived it off the board a minute later, and the process
  behind it went on posting activity and raising permission requests against a card nobody could see. Some of
  those cards recovered because their SessionStart hook happened to fire and move them, and some did not, which
  made the behaviour "it depends on whether a hook fired" rather than a rule.
  There is a rule now. A launch onto an existing card moves it to `needs-input` with the reason `started`,
  which is exactly where a session that has only just come up lands anyway, so the two paths agree instead of
  racing. It applies to `dead`, to `done` and to `backlog`: an offered item being started is out of the inbox,
  and a live runner under a `done` card is worse than under a dead one, because `done` is never swept and
  nothing but a waiting status can be revived by the next turn. A card already in a running column is left
  alone, so a session that got to work during the settle window is not dragged back to announce work it has
  begun.
  **A shelved card is refused.** Shelving is an operator putting the work down, and the permission chain
  spends that: every request from a shelved card is refused unanswered. A launch onto one either freezes the
  runner it just started, or quietly overturns the one status somebody chose by hand. So it is neither, and
  the refusal names unshelving as the way through. Unshelving still works, because the board moves the card
  out of shelved before it asks for the runner. This also closes the adopt path, which reached shelved cards
  through `AdoptableTask` and started onto them silently.
  Starting a second runner onto a card that already has one is refused for the reason `adopt` already refused
  it: two processes in one directory, and the card describes whichever spoke last.
  The reaper is the other half, and it now runs in both directions. A card filed `dead` while atrium still
  owns its runner comes back, which catches every path that forgets, including ones written later. It asks the
  supervisor rather than the card's pid, because the operating system recycles pids and a stale one on an old
  dead card is true about somebody else's process. It also asks the supervisor rather than the board, because
  the sweep archives a dead card within the minute and the board cannot see it any more, which was the
  symptom. Liveness is now settled before the sweep runs, so a card about to be revived is not archived in the
  same tick for a status one call away from being corrected.
  One test came along for the ride. `TestALiveRequestIsNeverOrphaned` waited for the permission row to reach
  the store and then deleted the orphan grace period, but the request handler writes that row before it
  registers the waiter, and the grace period exists precisely to cover the gap between those two. So the test
  raced its own subject and, under enough load, reported the ordinary case as an orphan. It now waits for the
  hub to say somebody is parked on the request, which is what it was always claiming to test.
- **`atrium peers` answers which of these want me.**
  Sixteen agents ran for hours and the operator found out which had finished by asking them, one at a time.
  Everything needed was already written down and nothing read it: `atrium finish` files a recap on a card,
  `atrium ask` puts the question on it, `waiting_reason` records that a question was asked at all, and the
  activity tracker knows whether atrium has heard from a session in the last quarter of an hour.
  The list is now grouped by what each card wants from a human, most wanting first, with the four states that
  kept being confused reading differently: a session that finished and filed a recap, one that stopped and
  asked (with the question printed), one that asked with `--working` and is carrying on regardless, and one
  that has gone quiet with nothing recorded, which is the one nobody knows to look at. `--fleet` adds the
  sessions that have finished, which the plain list leaves out because they cannot be told anything.
  No new state and no migration. The classification is a read of facts that already exist, and the quiet case
  comes out of the in-memory activity tracker, so nothing live is written down. After a restart everything
  running reads as quiet, which is true: atrium has not heard from any of them.
- **A session that stopped to ask something was filed as a turn that merely ended.**
  `atrium ask` moved the card to `needs-input` through `SetStatus`, which writes an empty waiting reason, and
  an empty waiting reason means "a turn ended". So a session that deliberately stopped with a question landed
  in the same bucket as one that ran out of things to do, and the board could not rank them apart. It now
  records `asked`, which is the reason that constant exists and what the asking-tool path already wrote.
- **Cards cannot be dragged any more, and the text on them can be selected.**

  **What the board no longer does.** A card cannot be dragged between columns to change its status, and it
  cannot be dragged within a column to reorder. There is no drag gesture on a card at all. If your gesture
  stopped working, this is why, and the replacement is on the card's own menu.

  **Why it went.** `draggable` on an element is not a hint that a drag is available. It takes the pointerdown,
  which means a sweep across a card starts a drag instead of a selection. A card carries a path, a branch, a
  wire name and an error, and every one of those is something you want on the clipboard. Copying one off a
  card is a daily thing. Moving a card by hand is rare enough that a menu entry is the right weight for it,
  and dragging things between swimlanes was the kanban idea rather than a thing this board turned out to need:
  status changes come from the card menu and from the agents themselves.

  **What replaced it, on the card menu.** **Move it to…** files the card in another column, and applies the
  rule the drop target did: `needs permission` and `ready` are not offered, because a card is in them because
  an agent said so, and filing one there by hand would claim a request or a wait that never happened. `done`
  is reachable from the board again, which matters more than the rest of this: it is the one state only a
  human declares, it was taken off the menu when dragging existed, and removing the drag without replacing it
  would have left no way to declare it.

  **Move it up or down** sets the card's place within its column. That is what `rank` is for, it is the
  operator's own order, and dragging was the only thing that ever wrote it. Up and down move past exactly one
  neighbour and take the midpoint of the two ranks either side, which is the arithmetic the drop used: a move
  renumbers nothing else. Pin is still there for when what you want is the top.

  **A card with text highlighted on it belongs to the browser, not to atrium.** Neither button opens the card
  menu while a selection touches the card. The left one because a click is how a sweep across the text ends,
  and a menu drawn over what you just highlighted is the same bug by another route. The right one because that
  is where the browser keeps Copy: atrium's menu has no copy entry of its own, so drawing it over a highlighted
  path takes the clipboard away at the moment you reached for it. Standing aside means returning before
  `preventDefault`, which is what lets the native menu through. One click on empty space collapses the
  selection and the card is a control again.

  **A card group keeps its `data-status`,** which is now its reconciler key rather than a drop target's answer.
  This is the trap in removing a gesture: the attribute was put there to say what status a card dropped on the
  group becomes, and by the time the drag went it had quietly acquired a second job. `morphKey` matches a group
  across a repaint by it, and a group with no key is destroyed and rebuilt on every paint, taking the scroll
  position and any selection inside it along. So the entry above, about a selection surviving the poll, rests
  on an attribute that reads like drag machinery and is not.

  **`check-morph.js` rule 4 no longer names `wireDragging`.** It was the one pass that wired listeners onto
  board nodes after a paint, and it carried a per-node flag so the reconciler could not double its handlers.
  The pass is gone with the drag: every handler a card has is an attribute in its own markup now, which comes
  back with the card and cannot double. The rule is kept and aimed at the shape instead, because the hazard
  belongs to the reconciler and not to dragging: anything that walks `#board` after a render and calls
  `addEventListener` with no per-node guard fails it.

  **Dropping files onto a terminal is untouched.** Different target, different feature, in daily use. So is
  the handle that sets the terminal list's width and the skin lab's floating panel, both of which are pointer
  drags on a grip rather than a card being dragged.

  **`scripts/check-cards.js` holds all of it,** nine rules, and runs as part of `scripts/check-board.sh`. It
  fails if a card becomes `draggable` again, if anything wires `dragstart` to a card, if a `user-select: none`
  rule lands on a card, if the menu's selection guard goes or is ordered after the `preventDefault` it has to
  beat, if a card group loses its reconciler key, if there is no menu path to a status or to `rank`, and if the
  terminal's file drop or either grip is deleted by a future pass at removing something named drag. The
  ordering rule matters as much as the presence ones: a `preventDefault` ahead of the menu guard kills the
  browser's Copy while leaving everything else looking right.

- **Two windows on one terminal fought over its size, and the loser was unreadable.**

  A pseudo terminal has one size. Every attached browser sent its own, and the daemon passed each straight
  through, so the last window resized set the width for everybody. That is not a cosmetic mismatch: the runner
  wraps its output for the size it was told, the other viewer draws those already-wrapped lines against a
  different grid, and the screen fills with torn text, duplicated status lines and rows that never clear.
  Dragging a shared window resized somebody else's terminal.

  The smallest attached viewer now decides, which is what every multiplexer settled on for the same reason.
  Sizes are held per attachment and given back when one detaches, so a phone that opened the board once does
  not hold the session at forty columns for the rest of the day. The cost is unused margin in the larger
  window instead of a screen nobody can read.

- **`POST /v1/launch` can be told there is already a session in that directory.**

  The guard existed and lived in the wrong place. `startFixture` adopts a card already in its directory and
  refuses outright when a runner is up on it, so a fixture never doubles. `Launch` underneath it asked
  nothing, so anything else calling the endpoint would put a second runner in a directory that had one, and
  the two write to the same files while neither knows about the other.

  `if_running` on the launch body now answers it, and the question moved into `Launch` where every caller
  reaches it. `skip` hands back the card that is there and starts nothing. `adopt` continues that card
  instead of making a second one. Absent means start anyway, which is what the board's launch dialog does
  and has to keep doing: two sessions in one repo is something an operator asks for on purpose.

  `adopt` with a live runner degrades to `skip` rather than to starting. A second process on one card is not
  continuing the work, it is two processes in one directory with the card describing whichever spoke last.

- **Every help bubble inside a dialog drew its tooltip underneath the dialog.**

  A modal dialog renders in the browser's top layer, so nothing outside it draws over it at any z-index.
  `#tip` is a top-level element, so hovering a bubble in settings or on the runners page rendered the panel,
  positioned it correctly, and put it in the wrong layer. The board already solved this for toasts by moving
  the host into the open dialog. The tooltip now travels with them.

- **The zrok instance toggle could not work, and said nothing about it.**

  Enabling issues a token against one instance, so the daemon refuses to move an enabled environment. The
  board offered the toggle anyway: one side was already selected and did nothing, the other was refused into
  a toast that had gone by the time anybody looked. An enabled environment now states what it is enabled
  against and how to change it, and the toggle is only there before enabling.

  Underneath it, the address was written to the settings BEFORE any of that was checked, so a refused change
  was stored anyway and a no-op change cleared it. The setting said one instance while the environment
  answered another.

- **The zrok panel asks a question instead of naming a mechanism, and a public board share is always
  reserved.**

  `whose zrok account` described the implementation to somebody who already understood it. It is now **how do
  you want to use zrok?** with two answers, `use this machine's zrok` and `give atrium its own`, and a `?` on
  each carrying the paragraph that makes the choice answerable.

  `zrok instance` was a text box that almost everybody had to know to leave alone. It is a toggle: **the
  public zrok**, or **somewhere else**, which reveals the box. Nothing is saved for the second until an
  address is typed, because an empty custom endpoint silently means the public one again. The toggle is not
  offered for the machine's own environment, where moving it means disabling the one every other tool here
  shares. That case is reported instead.

  The configuration fold now opens by default. It held the account and instance choice, so a machine that was
  already enabled opened the gear onto a summary with no visible way to change any of it.

  **The board's public share reserves its own address.** It used to be three presses in order, and skipping
  any of them meant zrok invented an address, the board handed it out, and the controller deleted it at the
  next stop: a dead link with nothing saying so. Starting a public share now reserves whatever name is
  configured, or invents one, writes it back, and reserves it again on every start. The `reserve it` and
  `reserve and share` buttons are gone, and the name field is only for choosing a memorable hostname.

  One bug fell out of reading it back: saving a zrok instance read `own` off a hidden input's `checked`, which
  a hidden input does not have. It was always false, so saving an address on a machine with no zrok of its own
  moved atrium off its own environment every time.

- **Right click in a terminal pastes.**

  The clipboard is loaded and the hand is already on the mouse, so the browser's context menu is the wrong
  thing to get: nothing on it applies to a terminal, and `ctrl-v` means leaving the mouse for one keystroke.
  Shift or ctrl with the right button still gives you the browser's menu, which is the convention every site
  that overrides it already follows.

  It goes through `pasteIntoTerm`, the same path `ctrl-v` takes, so a screenshot on the clipboard is uploaded
  and the runner is handed a path rather than nothing.


- **A restart could refuse forever, and asking a session to stop was what kept it busy.**

  `busyAgents` treated a supervised card as working unless it was `needs-input` or `needs-permission`. A card
  that is `done` is neither, and never moves back to `needs-input` on its own, so it counted as busy for the
  rest of the daemon's life.

  The loop that made is worse than the miscount. Each attempt asks every busy session to stop. The session
  replies, replying is a turn, a turn writes activity, and activity is what the check reads. **Asking it to
  stop is what made it busy**, so three attempts produced three park messages, three polite acknowledgements,
  and three refusals.

  Three guards now, and the deadlock needs all three to fail.

  `done`, `dead` and `shelved` are terminal and are not sessions mid-tool. **Stale activity is not activity**:
  it is written when a tool STARTS and nothing writes when a turn ends, so a session that stopped an hour ago
  still reads as `thinking`. And **once a session has been told, thinking stops counting**, which is the guard
  that actually breaks the loop: after a park message the question narrows to whether anything is HALF
  WRITTEN. A session mid-tool may have a file open. A session thinking has nothing on disk it would lose, and
  the message it was sent says in as many words not to start another tool, so a session that obeyed is exactly
  one that is not mid-tool.

  The conservative case is unchanged and still deliberate: a supervised card that is running with no activity
  at all is still counted, because "I cannot tell" is not "it is safe".

- **`atrium ask`: a session can say it is stuck, and what would unstick it.**

  The other half of `atrium finish`. That one let a session say its work was over. This lets it say the
  opposite and say WHY, which is the part nothing else could carry.

  A stuck session already reached `needs-input`, but by INFERENCE: a hook fires at the end of a turn and atrium
  concludes nobody is typing. That answers "this session stopped" and never "what for", so the board could say
  a card was waiting and not what it was waiting on, and the operator had to open the terminal and read back
  through the scrollback. The ask lands on the card's `why`, which the board already draws under a title.

  **Stopped and working are different and are treated differently.** By default an ask means the session has
  stopped and the card moves to waiting. `--working` records the question without filing the card, because the
  status column is a bucket of human attention and putting a working session in it makes the count that drives
  every alert lie.

  A command rather than a tool, for the reason `finish` gives: it is the one channel every runner already has,
  and it has to work for a bare shell as well as for something with an MCP surface. A shelved card is not
  dragged back, a long ask is truncated rather than refused, and an agent atrium has never heard of gets `ok`
  and nothing recorded, because failing here would mean a session could fail at the moment it asked for help.

- **A popped-out window stops announcing what you are watching.**

  It was testing `inForeground`, which is visible AND focused. A popped-out terminal on a second monitor is
  visible and not focused, so the window fired a desktop notification about a session the operator was looking
  at while it happened.

  Focus answers which window has the keyboard. That is a different question from whether you can see it, and
  WHICH ONE TO ASK depends on what the document is for. The board is a tab among many, so being visible does
  not mean you are reading it and it keeps the stricter test. A popped-out window holds one session and exists
  to be looked at, so visible is enough.

- **The configuration export has a button, and both open CSS nits are fixed.**

  `back it up` in the gear saves the file and reads one back. Reading back is a DRY RUN first, always: it lists
  what would change, and applying is a second press made after reading it. A refusal, which is what happens
  when something in the configuration looks like a credential, is shown whole rather than summarised, because
  that sentence is the entire product of the failure.

  **The group-heading hover was wrong on every light skin, and the reason generalises.** It raised the name's
  lightness to 85%, which reads as more prominent on a dark board and nearly invisible on a light one: on
  `paper` it went pale yellow against near-white. It blends toward `--head` now, so it goes darker on a light
  skin and lighter on a dark one. A hover that changes lightness in a FIXED direction is wrong on half of
  twenty skins, and anything meaning "more prominent" has to move relative to the skin's own text colour.

  **Glyph buttons in a terminal's bar stopped wearing word-sized padding.** `.term-bar button` is tuned for
  `ctrl-c` and `exit`. The `copy` button the nit originally named is long gone from that bar and the same
  defect had moved to the folder, the cog and the up arrow.

- **Tab completes a path in the browser terminal, and gets out of the way when it cannot.**

  Tab belongs to whatever is running in the pty, so this works from outside somebody else's input line, and
  the whole design is about giving up early rather than being clever.

  **What cannot be known is where the cursor is.** The runner redraws, wraps, recalls history and rewrites the
  line whenever it likes, so anything that inserts text on an assumption about cursor position eventually
  corrupts what somebody typed. **What can be known exactly is what atrium sent**: every keystroke goes through
  one place, so the buffer is a record rather than an inference.

  So: track what was sent, ABANDON the moment anything ambiguous happens, and offer nothing while abandoned. An
  arrow key, an escape sequence or a paste sets it back, and only Enter restores it. A `/` or a `\` in the token
  is what makes it a path rather than a word, which handles Windows mixing both separators.

  **Tab passes through unless atrium is confident**, so claude's own completion keeps working everywhere this
  does not apply. Confident means a believable buffer, a token that looks like a path, and candidates found.
  The key is swallowed optimistically and the Tab sent afterwards if nothing could be completed, because doing
  it the other way round is two inputs for one press, which is the doubled-keystroke class of bug.

  **Only the missing characters are sent.** Never a whole line, never backspace-and-retype: both assume the
  line is what atrium thinks it is, which is the assumption this refuses to make. Candidates come from
  `/v1/browse`, which lists the daemon's filesystem and is already bounded to the browse roots, and that is the
  correct source because the paths being typed are on the runner's machine rather than the browser's.

  Two invariants added, both of which fail if the give-up or the pass-through is removed.

- **The published board can ask who you are. This reverses a documented rule, narrowly.**

  `CLAUDE.md` and `docs/overlays.md` both said authentication is out of scope and that reaching the board from
  elsewhere is an overlay's job. That held while the board was only ever on loopback or behind a private share,
  and stopped holding when a reserved public address made handing out a link comfortable. A public URL with no
  login, in front of something that reads files, answers permission prompts and types into terminals, is not a
  line worth defending on principle.

  **Atrium still owns no credentials, which is the part that rule was protecting.** No user table, no
  passwords, nothing to hash. Identity is delegated to an OIDC provider, atrium verifies what that provider
  signed against its published keys, and the session cookie proves a completed verification rather than
  standing in for a password. A design review flagged the first plan, which included username and password, as
  contradicting the settled decision. It was right and that half was dropped.

  **It wraps the published handler and nothing else**, which makes the boundary structural rather than a rule
  somebody has to remember. The overlay listener is a different `net.Listener` on a different `http.Server`, so
  loopback is untouched, every hook and the CLI and the MCP server keep working, and a lent session keeps its
  own rule that the address is the credential. `TestTheLocalBoardIsNotWrapped` fails if that stops being true,
  and it would otherwise break quietly, because a hook that fails is designed never to fail a session.

  Decisions worth knowing: an empty allow list means NOBODY and is refused at save time, because "anybody the
  provider authenticated" on a provider with open registration is the whole internet with an extra step. The id
  token is verified rather than decoded. The cookie is signed and not encrypted, since nothing in it is secret
  and what must be impossible is editing it. An API call is refused rather than redirected, because a login
  page where JSON was expected reads as a corrupt response. A login state is single use.

  Not built: PKCE, refresh, and roles. Everybody who gets in gets the whole board, which is the same grant a
  share has always been.

- **`atrium room`: one board, many machines. Stage one of `docs/federation-design-v2.md`.**

  A room keeps its own daemon, its own database and its own terminals, and tells a hub what is on it. The hub
  holds that IN MEMORY AND NOWHERE ELSE, which is the design rather than a shortcut: a room's cards are that
  room's state, and a second durable copy on the hub would be a source of truth that is wrong whenever the room
  is unreachable. What is held is a cache with a timestamp, and a room that stops talking goes stale, and then
  disappears rather than persisting as a claim about a machine nobody can reach.

  Every check-in REPLACES that room's cards rather than merging them, or a card deleted on the room would live
  forever on the hub.

  **Terminals do not federate and the row says so.** A pseudo terminal cannot leave the machine that made it.
  What travels is cards, their status, and what each is waiting for. Each room reports where its own board is,
  and that link is the row's main affordance: everything the hub cannot do for a remote card is done there.
  Remote cards are drawn as a list rather than as cards, because giving them a card's menu would promise
  actions that would fail.

  **Which end dials is a reachability question, not a design one.** The design assumes the leaf is behind NAT.
  On these machines it is inverted: the rooms are public cloud instances and the hub is a desktop behind NAT.
  So `--service` and `--identity` dial the hub over an OpenZiti service and neither end has to be reachable
  from the other, and `--hub` remains for the ordinary case.

  **Proved on two real machines**, `cdzrok` and `cdaws`, cross-compiled and copied over: both ran their own
  daemon, both dialled the hub over ziti with no port forwarding at either end, and both appeared on the hub
  with a live card on them.

- **Atrium's configuration goes out to a repository and comes back, and no credential goes with it.**

  `GET /v1/config/export` writes harnesses, fixtures, sources, actions, rules, the skin, the timers and the
  sharing options as one indented JSON document, named and dated so a file in a checkout is identifiable in
  six months. `POST /v1/config/import` reads one back.

  **What must not leave is defended twice, and the two fail differently on purpose.**

  The allowlist is the STRUCTURE. Nothing marshals a stored struct and strips fields out of it: every value is
  copied across by name into a type declared in `export.go`. A field added to `ZrokConfig` next year is absent
  because nobody wrote a line to include it, which is the safe direction and needs no rule to remember. A test
  compares the two types by reflection and fails when the stored one grows a field nobody has decided about.

  The result is then SEARCHED. An allowlist cannot help with a credential typed into a field that is
  legitimately exported, and a source is an operator-authored command line, which is exactly where a token
  goes. The finished document is scanned for the shapes credentials have and a hit refuses the whole export,
  naming the field without echoing the value. Refusing rather than redacting: an export that silently drops
  something is one somebody restores from and finds half a configuration.

  Never exported: the zrok account token, share tokens, reserved addresses, the ziti identity path, and the two
  overlay blobs wholesale. `global_auto` is on that list for a different reason, and it is the interesting one.
  It is not a secret, it is a STATE. Whether this machine is approving everything right now is not
  configuration to restore onto another one, and an import that silently turned it on is the worst thing an
  import could do.

  **Importing never overwrites silently.** Anything already set is kept and reported with how to override it,
  and the default is a dry run that answers what WOULD change. The overlay configuration is merged rather than
  replaced, which is the subtle one: the exported shape has no token in it, so writing it over the stored
  config would erase the account token with a value that was deliberately absent, and the machine would look
  configured and refuse to share.

  Proved against a COPY of the operator's live database rather than against fixtures. It exported cleanly, with
  no secret-shaped content, and the legacy `mode` on that real configuration migrated to the new flags
  correctly.

- **The zrok panel stops offering edits it cannot make, and public and private stop being one choice.**

  **Enabled means locked.** While the board is on a zrok share, every setting is what that share started with
  and the fields say so. Half of this was already true and unsayable: the endpoint refused to move an enabled
  environment, so the form accepted the edit and reported a failure about something the operator could not
  see. The rest was worse, because it was accepted, stored, and then ignored until the next start. One rule
  now, stated at the top of the panel: stop sharing to change it.

  **Public and private are two checkboxes and both can be on.** The select was a leftover from when a share
  was one thing, and it made two independent capabilities look like one exclusive choice. A machine may
  reasonably want a public link for the board and a private share for a lent session.

  `mode` survives as the thing the start path reads, because a listener is one thing and has to pick, and it
  is derived rather than typed. `normalise` reconciles the two in both directions, and the upgrade is the part
  that mattered: every install that exists holds a `mode` and neither flag, and reading that as "neither" would
  have silently switched sharing off on every configured machine. Five tests cover it, including that
  normalising twice changes nothing, since it now runs on both the read and the write path.

  **Whose account is a real toggle, and it only offers what exists.** The left-hand option is the token already
  on this machine, which is not a choice on a machine that has none: offering it there is a button whose only
  outcome is a refusal. The daemon reports whether the machine has an environment separately from whether the
  selected one does, which is what makes the difference sayable.

  **The account token is a password field and says it is required.** It is reusable on every machine, it grants
  everything that account can do, and it was sitting unmasked in a textarea on a board that gets screenshotted
  and popped out into windows on other monitors. The ziti side stays a textarea: an enrollment JWT is single
  use and people want to see it.

  **`the address to keep` is now `what share name would you like to use to access the dashboard`.** It
  described the mechanism to somebody who already knew it and said nothing to anybody else. What it answers is
  which hostname people type.

- **A popped-out window closes itself when its runner exits, which it has never actually done.**

  Three fixes were made to this and none of them ran. `markTermDead` tears the terminal bar down to a single
  close button and is the only caller of the countdown that closes the window. It was defined, it was correct,
  and **nothing called it**. The branch that runs when a runner exits wrote `you can close this window` and
  returned, so the teardown, the countdown, the broadcast that has the board close the window through the
  handle `window.open` returned, and the fallback button were all unreachable.

  That branch calls it now. `check-terminal.js` gains the rule that would have caught it: a teardown function
  that is defined and never called fails the check. Nothing else could have. The file parses, the function is
  syntactically fine, and a diff reader sees a definition and assumes a caller.

- **A paste scrolls all the way to the bottom, not to where the bottom was.**

  `sendInput` scrolled when the bytes were SENT, which is before the runner has seen them. What moves the
  bottom of the buffer is the echo coming back and the prompt redrawing around it, and by then the scroll had
  already happened. A twenty line paste landed about twenty lines short, which is worse than not scrolling,
  because it looks like it worked.

  The view now follows the output down for a moment after input is sent, and then stops. It has to stop: a
  terminal that scrolls to the bottom on every write can never be scrolled up while a runner is producing
  output, which is why xterm does not do this by default. The follow goes in `term.write`'s CALLBACK rather
  than after the call, because `write` queues the bytes and parses them later, so scrolling on the next line
  scrolls a buffer that has not grown yet.

  The invariant that was supposed to cover this passed throughout. It asserted that `sendInput` scrolls, which
  was true and insufficient, so it has been replaced rather than added to.

- **CI runs on Windows as well as Linux, and four tests that had never left Windows now pass on both.**

  The workflow landed and failed on its first run, which is CI earning its place on day one. Two of the
  failures were assertions encoding Windows path semantics: `filepath.ToSlash` is a no-op on Linux and a
  backslash is a legal filename character there, so the production answers were right on both platforms and
  only the tests were wrong. Those are gated on Windows now.

  The other two delivered a megabyte of test payload in an environment variable. Linux caps a single
  environment string at 128KB, so the spawn died with `argument list too long` before the output bound they
  exist to check was ever reached. The payload moves to a file, which has no such ceiling on either platform,
  so the bound is checked everywhere rather than skipped where it is cheapest to skip.

  **The matrix now includes `windows-latest`,** which matters more than any of the above: ConPTY, the
  rename-aside binary swap, the logon task and every path rule are Windows behaviour, and a Linux-only matrix
  built none of it.

- **Atrium can keep its own zrok environment, against its own instance.**

  **The problem.** A machine has one zrok environment, `~/.zrok2`, holding an account token and the identity it
  was issued. Pointing atrium at a different instance meant disabling the environment that the `zrok` command
  and every other tool on the machine depend on, enabling against the other one, and doing it again to go back.
  That is a large price for atrium wanting to talk somewhere else, and it is paid by everything that was not
  asking.

  So there are two roots now. The machine's, which is the default and unchanged, or atrium's own, kept beside
  the database so a second daemon on a second database gets a second environment. Turning it on and giving an
  address is one decision and one request: the endpoint is written to whichever root is now selected, and only
  while that root is not enabled, because moving an enabled environment leaves a token issued by one instance
  being sent to another and fails in a way that reads as a broken token.

  **Every zrok call goes through one loader.** The SDK finds the environment through a package-level global,
  `environment.SetRootDirName`, not a parameter. A direct `LoadRoot` is correct exactly until something else
  moves that global, and the failure is silent: the call succeeds against the wrong account. All eight call
  sites now go through `zrokRoot`, which holds a mutex across the load and puts the global back afterwards.

  **Enabling and disabling are native.** They used to shell out to `zrok`. The command can only ever write to
  the machine's root, so driving it could never enable atrium's own. Both are now the same API calls the command
  makes, four for enable and two for disable, which also means zrok no longer has to be installed for either.
  The environment is described as `atrium@<host>` rather than `<user>@<host>`, because two environments from one
  machine sit next to each other in `zrok overview` and the new one has to be identifiable without counting
  rows.

  **The readiness the panel shows is the environment atrium will actually use.** It read the machine's, which
  after this would be the wrong one in the direction that matters: offering to share from an environment the
  daemon is not going to touch. The refusal when nothing is enabled says which one, because `zrok enable` at a
  terminal enables the machine's and following that advice would leave the panel saying the same thing after the
  command appeared to work.

- **A shared session keeps its address. A restart no longer takes the link with it.**

  **What was wrong.** A share lived exactly as long as the process that made it. The daemon released every one
  during wind-down, so the link you handed somebody stopped working the next time atrium restarted, and nothing
  said so. The code defended this at length: the address IS the credential, since a lent session has no login,
  so reserving one would mean handing out the same guessable address forever.

  That defence conflated two independent properties. **Unguessable and durable are not the same thing.** A name
  generated once at random, held by the controller, and asked for again on every start is both.

  **What happens now.** Sharing a card reserves `atrium-<twelve random characters>` in the public namespace and
  records it. Sixty bits, from an alphabet with no `l`, `o`, `0` or `1`, because the address gets read aloud
  and a confusable character turns an unguessable link into a support question. The `atrium-` prefix buys
  recoverability: a leftover share is identifiable on an account that also holds shares from four other tools.

  **A shutdown unbinds. Stopping stops.** These used to be one operation, which is why every share died at the
  restart. Wind-down releases the SHARE and keeps the NAME, leaving a row that says this card should be lent
  out. Stopping releases both and is the only thing that gives an address up, so it now asks first and says
  plainly that it cannot be undone.

  **Three ways back up, one function.** `bindCardShare` is the only place a share is put up: first time, again
  after a stop, on the way back from a restart, and when a runner starts on a card that was already lent out.
  They differ in what they have already decided, not in what they do, and the one time they were separate the
  restart path forgot to record the new token.

  **A share with no terminal is shown, not hidden.** `/v1/shares` merges what is served with what is recorded
  and marks each `live` or not. A card waiting for its runner used to look identical to a card that had never
  been shared, while somebody was holding an address for it.

  **Orphans are swept, and only atrium's own.** A pruned card leaves a name reserved on the account that nothing
  will ever ask for again, and nobody can see it, because the board draws cards and the card is what went. The
  sweep joins the share table against `task` and releases what is left. It never reads the account and deletes
  what it does not recognise: this machine's zrok account is not atrium's.

  The private mode is weaker on purpose and says so. A private share has no name, its token is the address, and
  deleting the share puts the token back on the shelf, so the rebind asks for the same one and usually gets it.
  Nothing holds it in the meantime, so `usually` is the accurate word and it is not dressed up as a guarantee.

- **Sharing says what it is doing, and offers only the ways out this machine has.**

  **The wait had no shape.** Creating a zrok share is several seconds inside somebody else's API, and for all of
  them the board showed the button unchanged. When the instance answered `500 shareInternalServerError` with an
  empty body, what arrived was a paragraph about a call the operator had no idea was being made, attached to a
  control that had looked inert since it was pressed.

  So the daemon narrates it. `shareStep` broadcasts each stage as it reaches it, and a dialog opens the moment
  the request leaves, listing the three real steps with the one in progress spinning: reading the zrok
  environment, asking the instance for the share, opening the listener that answers it. A failure marks the step
  it died on and keeps the message whole, with `try again` beside `close`, because the usual cause of a 500 from
  the instance is the instance.

  **The dialog does not depend on the events.** The POST still carries the answer, so a window that receives
  none of them ends in the same place with less to read on the way. The events make a slow step legible, they do
  not make the flow work. Escape is refused while one is in flight: there is no way to cancel a call already
  inside the SDK, and a share that landed after the window was dismissed would be live, unlisted, and news.

  **The address lands in that same dialog** rather than in a second one, under the finished steps, which are
  worth a glance in the moment the link appears as much as they were while it was awaited.

  **One section per overlay, and only the ones that are ready.** `share this session` is a flyout now: a zrok
  group when the zrok overlay is ready, holding the public link, the private command, or `stop sharing` when one
  is up, and an OpenZiti group when that one is. Offering a zrok link on a machine with no zrok account was
  offering a button whose only outcome was a paragraph about accounts. Lending one session over OpenZiti is
  named there and not built: there is no link to send, a guest needs an identity, and issuing one is the line
  `docs/overlays.md` says atrium does not cross. Named rather than omitted, because a machine with ziti up and a
  menu that mentions only zrok is a machine whose operator cannot tell whether they missed a setting.

  The mode is picked in the menu instead of in the dialog. Public and private are two different things to hand
  somebody, not two settings of one thing, and a select inside a confirmation made them read as a detail of a
  decision already taken.

  **The board's own share button gets the same treatment.** `start sharing` under the gear had the identical
  problem and produced the identical complaint: press it, watch nothing happen, and eventually receive a toast
  about a 500. `overlayStep` narrates that one too, and the button is REPLACED by the step it is on rather than
  disabled, because a greyed-out button says you may not and what is true is that you already have. The busy
  state is set when the request leaves rather than on the first event, so a window that receives no events still
  looks like it heard the click. Both endings are left to the POST, which carries the new state and the error
  text, and clearing the line on the event would blank the panel a beat before there is anything to replace it
  with. The ziti path has two steps rather than three: a ziti listener is bound rather than created, so there is
  nothing to ask a controller for and nothing to release afterwards.

- **Atrium starts by itself on all three operating systems, and the packages that carry it were actually
  built.** `docs/packaging.md`, rewritten.

  **The packaging groundwork had been written and never run, and running it broke most of it.** Four things were
  wrong, and none of them was findable by reading.

  `scripts/atrium-autostart.ps1` could not execute at all. It had `[CmdletBinding()]` and a parameter named
  `$Db`, and CmdletBinding adds the common parameters, one of which is `-Debug` carrying the alias `db`.
  PowerShell refused to bind every invocation of the script, including `-Remove`, with a message about an alias
  nobody had written. The script had been written, reviewed and documented, and had never once been run.
  `[Parameter(Position = 0)]` does the same damage for the same reason, because either attribute turns a script
  into an advanced function, so neither is used now and a parameter's position comes from where it is declared.

  The nfpm config had three defects, all found by building the real `.deb` and unpacking it. `type: doc` is not
  an nfpm content type and an unrecognised type is dropped in silence, so both documentation files were missing
  from a package that reported success. nfpm expands environment variables nearly everywhere and NOT in
  `contents.src`, which fails as `glob failed: ${BINARY}: no matching files` and reads as a wrong path, so the
  binary is staged to one fixed name instead. And `type: config` on a file under `/usr/lib` would have made
  dpkg stop and ask about a conffile on every single upgrade.

  **The daemon now starts at login on Linux, macOS and Windows, as you, in your session.** That last part is the
  whole design and it is one decision in three spellings: a systemd USER unit, a launchd LaunchAGENT, and a
  logon task. A system unit, a LaunchDaemon or a Windows service has none of your PATH, shell configuration, ssh
  agent or Claude Code configuration, and it cannot open a pseudo terminal you can attach to. It would install a
  version of atrium that comes up, serves the board, and supervises sessions that are useless, with nothing
  anywhere to say so.

  Windows was the one worth writing down rather than asserting. A real service running as the logged-in user
  needs a password in the LSA secret store, still runs in session 0 where it is not really you, and would need a
  service control handler compiled into `cmd/atrium` or a third-party wrapper to answer the SCM at all. A gMSA
  removes the password by being a different account, which defeats the reason to run as the user. So it stays a
  logon task, and the cost is said out loud instead of hidden: it stops when you log out, and Windows has no
  equivalent of `loginctl enable-linger`. Linux can buy its way out of that trade. Windows and macOS cannot.

  **A deb or an rpm now enables the unit for the person who ran the install,** which reverses what this
  repository said when nothing had ever been installed. Three obvious ways to enable a user unit from a root
  postinstall are all wrong and `packaging/postinstall.sh` names each: `systemctl enable` reaches for a system
  unit that does not exist, `systemctl --user enable` talks to root's own manager, and `systemctl --global
  enable` decides for every account on somebody's server. What is correct is `SUDO_USER`, then lingering, then
  `runuser` into that account with `XDG_RUNTIME_DIR` and the bus address named explicitly, and a fallback that
  writes the enable symlink by hand and admits it will only start at the next login. An unattended install with
  no human to name enables nothing and says why. `ATRIUM_NO_ENABLE=1` and `ATRIUM_NO_LINGER=1` are the ways out.

  The old rule, that installing says put this here and starting a daemon is a different sentence, is a good rule
  and is wrong for atrium. Atrium's job is to still be running when you come back to it, and an install that
  leaves you to start it by hand leaves you exactly where this repository started, with a daemon somebody ran
  once by hand months ago that nothing would bring back.

  **`scripts/atrium-service.ps1` and `scripts/atrium-service.sh` are the one obvious command,** with install,
  uninstall, start, stop, restart and status, every verb idempotent. Stopping calls `atrium stop` before it
  touches the init system, because a kill is not a stop: closing the daemon's pseudo terminals takes every
  attached runner with them. Status asks the daemon over `/v1/health` rather than believing the scheduler, since
  a task reporting Running with `conhost --headless` in front is reporting conhost.

  The Windows script carries a `selftest` verb that registers a second task under its own name, on its own
  ports, against its own database, recording itself in its own location file. Every one of those four would
  otherwise be shared with the daemon somebody is actually using, and sharing any of them turns a test into two
  daemons on one database or a stolen location file that every hook on the machine reads to find a port.

  **Linux publishing goes to GitHub Releases carrying the deb and the rpm.** An apt and yum repository is the
  thing that buys `apt upgrade`, and it costs a GPG signing key that has to be generated, stored outside a
  repository, put in CI, published for people to trust, and eventually rotated by somebody who remembers how.
  GHCR was considered and is a poor fit twice over: a container image of the daemon cannot supervise the user's
  own sessions, which is the same constraint as the LaunchDaemon one, and an OCI artefact holding a `.deb` is a
  channel with no clients, because nothing on a Linux machine reaches for `oras` to install software. All three
  are built from the same artefacts, so overruling this changes only the last step.

  **There is a `.github/workflows/` now, and there is no logic in it.** Two files that check out code, install a
  toolchain and call a script. Everything they run runs here, today, with `bash scripts/ci.sh`, which covers
  gofmt, vet, build, tests, the board, the skins and a parse of every packaging script in both shells. The
  release workflow checks out with full history, because `git describe --tags --exact-match` on a shallow clone
  fails and silently stamps the binary `dev`, and a release that calls itself `dev` is one no package manager
  will ever offer an upgrade over.

- **`atrium version`, and everything packaging needs that does not need a certificate.** `docs/packaging.md`.

  **The binary can say what it is.** Nothing could answer that before, which is the first question every
  packaging format asks: scoop compares it to decide whether an update exists, deb and rpm refuse to install one
  package over another without it, Homebrew names the bottle with it. It also answers a question that comes up
  with no packaging at all, which is what this daemon running since some morning weeks ago actually is.

  Stamped by the linker, not by a constant somebody edits, because a hand-edited constant is wrong between the
  edit and the tag and wrong again on every branch build. An unstamped build says `dev`, and there is a test
  asserting it: the tempting alternative is the nearest tag plus a commit count, which reads like a version and
  is not one, and a package manager comparing that string would believe a working-tree build is a release.

  **`scripts/release.sh` builds all five platforms, archives each in the shape its ecosystem reads, and writes
  one checksum file.** Run and verified: `windows/amd64`, `linux/amd64`, `linux/arm64`, `darwin/arm64`,
  `darwin/amd64`, from a Windows machine, with the Linux build confirmed as a statically linked ELF. That last
  part is what a deb and an rpm depend on, and it is free because `modernc.org/sqlite` is pure Go, which is why
  `CGO_ENABLED=0` is set explicitly rather than relied on: a machine with a C toolchain would otherwise quietly
  produce a binary that needs one.

  The checksum file is the piece that matters most and is the easiest to leave for later. A manifest carrying a
  hash that does not match its archive is the most common packaging failure there is, and it is only ever found
  by a stranger.

  **A scoop manifest, an nfpm config for deb and rpm, and a systemd user unit,** all written and none published.

  The systemd unit is a USER unit, and it is the same decision `scripts/atrium-autostart.ps1` makes on Windows
  in being a logon task rather than a service. A system unit runs outside your login session, cannot open a
  pseudo terminal you can attach to, and has none of your PATH, shell configuration, ssh agent or credential
  helpers, so every runner it started would inherit none of them. It would install a version of atrium with its
  main feature missing and nothing to say so. The two files are one design on two platforms.

  Nothing is enabled or started on install. Installing says put this here; starting a daemon that opens two
  listeners and begins supervising processes is a different sentence.

  **Named as unsolved rather than left to be discovered:** `restart_atrium` renames a staged binary over the
  running one, and under any package manager that file belongs to the manager. A rename behind its back leaves
  it reporting a version that is not installed, and the next update quietly puts the old binary back. The answer
  is probably that a packaged atrium refuses the swap and names the upgrade command instead, which means the
  build has to know how it was installed. `docs/reload-design.md` assumes the swap always works.

  **What is unverified is most of it, and the document says so per file.** `nfpm` is not installed here, the
  unit has never been loaded, and the scoop manifest points at a release that does not exist. Signing,
  accounts, moderation and publishing are all yours.

- **The grouping expression rule is decided, and refused at the boundary.** The board compiles two
  operator-supplied functions with `new Function` to group cards. That has always been safe, for one reason
  nobody had written down: `groupingPrefs()` reads `localStorage`, so the code running in a browser was typed
  into that browser by whoever was sitting at it, and somebody who can write it can already open dev tools.

  Safe by accident is not the same as safe by decision, and the accident is one commit from ending. Grouping is
  board-wide and today it is lost when you open a different browser, so somebody will reach for the obvious
  field, it will work perfectly on the first machine, and it will ship. A daemon-side expression is one machine
  typing code that another machine runs. These functions have full page scope: `fetch` is in hand, and the board
  can read the daemon's filesystem, every card title, every worktree path and the audit log. It stops being a
  grouping rule and becomes an exfiltration primitive the moment somebody other than the operator supplies one.

  **The rule: an expression may be stored where it was typed.** `POST /v1/settings` now refuses `group_by` and
  `group_order` with the reason and a pointer to the argument, checked against the raw body rather than a struct
  field, because what is being guarded is a field nobody has added yet. It sits ahead of every write in the
  handler, so a body carrying an expression beside a real setting is refused whole rather than half applied.
  Three tests, one of which asserts the refusal explains itself: a comment can be deleted, and a failing test
  names the decision at the moment somebody makes the mistake.

- **`docs/scm-design.md`,** which is a design and not a feature. Source control and ticketing, asked for
  together and separated in the first paragraph because they share nothing but the word integration.

  **Inbound** is the pull to `docs/intake-design.md`'s push. Sources on a timer fill an inbox with work aimed at
  you; this is having a pull request open in front of you and wanting a card. The prior art is `git-worktree.ps1`
  on this machine, whose URL dispatch already recognises GitHub, Bitbucket and GitLab pull requests, issues,
  security advisories including the temporary private fork the fix lives on, Zendesk tickets and Discourse
  threads. Four of its properties are worth taking exactly and one is worth refusing: it does the work inside the
  recogniser, which is why it is six thousand lines.

  A recogniser is a ROW, not code, like a runner and a source and an action. It holds a pattern with named
  captures and templates over them. It holds no credential and never will: `fetch` is an argv the operator wrote,
  and `gh` already has a token in the keyring it already uses. It produces a filled-in launch dialog and stops
  there, because starting work is a decision. It does not learn what a repository is, does not clone, and does
  not own a worktree layout, because `gwt` already does all three and a second worse copy is not an improvement.

  **Outbound** is the half nobody asks for until it hurts: five runners, the fixtures, the sources, the actions
  and the standing permission rules live in one SQLite file with no history and no diff. A permission rule is a
  policy decision about what an agent may do unasked, and there is no way to see what it looked like last week.
  The answer to "any SCM, not just atrium's" is files on disk, one per table, because git, Mercurial, Subversion
  and a USB stick all handle a directory identically.

  Reviewed before it was written down as anything more, and the review changed three things. Naming whole tables
  as exportable is not a policy: every one of them holds fields true only of this machine, so an exporter could
  satisfy every stated rule and still produce a file that is useless on the second machine. There is a per-field
  table now, with `exported`, `local` and `flagged`, and a rule that can be checked mechanically: no absolute
  path leaves this machine. The recogniser's `prepare` command is in the row shape rather than described in
  prose. And `docs/intake-design.md`'s deduplication key, open long enough that a reviewer could pick four
  different answers out of the document, is decided: `source` plus `external_id`, never the URL, because an id is
  a name and a URL is a route.

- **`expose the board`, and a share that dies tells you.** The overlay panel was named for a goal rather than an
  action, which mattered more once there were two kinds of exposing: it now pairs with `share this session` on a
  card, and each says which one it is.

  Two thirds of the backlog entry for this turned out to be already built. The collapse behind `configure
  zrok...` and the remembered open state both shipped some time ago and the entry had gone stale, which is worth
  recording because it is the third stale entry found this week.

  What was actually missing:

  **A share that stops on its own now alerts.** It was the one state change in the panel that nobody pressed a
  button for. The listener drops, `err` is set, and the only way to find out was to open the gear and look,
  which nobody does because there is no reason to. It matters because the address is the thing you handed to
  somebody else: a dead public share is a link that has stopped working for a person who is not in the room.
  Only the running-to-stopped edge, only with a reason attached, and never on the first event of a page's life,
  so a board opening onto a share that died an hour ago does not announce old news.

  **Reserving a name and sharing it is one press.** It took three: type a name, `reserve it`, `start sharing`.
  Nobody reserves a name in order to admire it, and the middle step exists because the two calls are separate on
  zrok's side, which is zrok's business rather than yours. Both buttons stay, because claiming a name for later
  without publishing anything now is a real thing to want. `reserve and share` goes through the same start path,
  so the confirmation that a public share puts a board with no login on the open internet is still asked.

  The field is called `the address to keep` rather than `reserved name (public)`, which was the state you want
  rather than the thing you are typing.

  **Not built, and recorded rather than dropped:** the panel still cannot say what your zrok account allows and
  how much of it is used before you press a button that fails. That needs another REST call and a shape nothing
  else in the daemon has, and the error classifier remains the floor rather than the finish.

- **A shell beside a wedged agent.** A card can hold two terminals now: the runner's, and a plain shell in the
  same directory. `agent | shell` on the terminal bar, and `open a shell here` on the card menu.

  It was the only `no` in the comparison table in `docs/backlog.md`, and the row every other remote-agent
  product also fails. Atrium owns a terminal per supervised card and that terminal belongs to the runner, so
  when the agent stopped answering and the question was `git status`, or what is holding that lock, the answer
  was to walk to the machine. Which is the thing the board exists to stop.

  **Two terminals, not N.** A list needs naming, ordering, closing and a picker, and none of that is the
  problem. One shell is a tool for looking at something; three is a terminal multiplexer, and a good one is
  already installed.

  **A shell is not a runner, and it lives in a second map to prove it.** `supervisor.get` is said fifteen times
  across the reaper, the park, shelving, actions, messages and the exit recorder, and every one of them means
  the process doing the work. Reusing that map is the obvious simplification and it would have marked a card
  dead the moment somebody typed `exit`. There is a test named for exactly that.

  The design was reviewed before any of it was written, and came back `needs_changes` on the one thing the prose
  had left out: how a client asks for one terminal rather than the other. Answered as `?kind=shell` on the
  existing attach route, absent meaning the runner so every caller written before tonight keeps its meaning, and
  an explicit `POST /v1/tasks/{id}/shell` to create one. A POST rather than spawning inside the websocket
  upgrade, because a shell fails to start in ways worth reading and a close code cannot say "there is no such
  directory".

  It carries `ATRIUM_TASK_ID` and deliberately not `ATRIUM_AGENT_NAME`: a shell that claimed the agent's name
  would have anything started from it filing activity against the card as though the runner had done it.

  **A writable share could have reached it, and cannot.** A lent session allows one route, `<id>/attach`, and
  without a guard a guest could have appended `?kind=shell` and got a general purpose command line on this
  machine. That is lending the machine rather than lending a session, which is the line `docs/overlays.md` says
  atrium does not cross. Refused explicitly rather than quietly rewritten to the runner, with four tests naming
  what a share hands over.

  A shell closes when its card is deleted, when the daemon stops, and after thirty minutes with nobody attached.
  The close waits for the process to actually go, because on Windows a directory cannot be removed while
  anything sits in it, and deleting a card is very often followed by removing the tree.

- **A pseudo terminal could be closed twice, and the process died with no output.** Found while building the
  above. `windDown` closes a runner's terminal when it will not take the hint, and `awaitExit` closes it again
  when `cmd.Wait` returns, which is what closing it caused. On Windows that is a double close of a handle, and
  it is not a Go panic: the process disappears silently. It survived this long because the two were usually far
  enough apart in time. Shells made it reliable, since closing one is immediate. Closing is guarded by a
  `sync.Once` now, which fixes it for runners too.

- **The runners page is five panes instead of one scroll.** `start`, `runners`, `fixtures`, `sources` and
  `actions`, with a spine down the left.

  It had grown to six groups stacked in one column, separated by a heading and twenty six pixels. Reaching
  sources meant scrolling past launchers, runners and fixtures, nothing on screen said where you were, and the
  page had no way to be arrived at pointing anywhere in particular.

  **The settings dialog had this exact problem and solved it,** so the answer was to use that rather than write a
  second one. `buildSettingsNav` is now `splitIntoPanes(host, headingClass, key)` with two callers, and the CSS
  it uses lost its `s-` prefix in the process, because those classes were never about settings. The property both
  keep is the one that makes it maintainable: the markup stays ONE FLOW of headings and their content, read top
  to bottom in the file, and the nav is built from the headings at runtime. Adding a section is still a heading
  and its content, in order, with nothing else to keep in step.

  Two things the runners page needed that the dialog did not. Its headings carry help bubbles, so a nav built
  from `textContent` would have put an entire tooltip inside a button: headings now name their own label with
  `data-pane`. And the page scrolls in `main` rather than in itself, so switching panes had to scroll the element
  that actually moved, or landing halfway down a short pane reads as a rendering fault.

  Panes rather than a sixth top-level tab, because these five belong together: they are the things atrium runs.
  Panes rather than an accordion, because an accordion is right when the common case is closed, and nobody opens
  this page except to do one of these five things. `configuration` is called `runners` now, since a pane label
  has to say what is in it.

  `scripts/check-runner-panes.js` asserts the partition against the real markup, the way the settings one does:
  the headings are direct children, every one is labelled, no two share a label, and every list `renderRunners`
  fills is on the page. None of those is a syntax error, and all of them come out as a page with one giant pane
  or a list that silently never appears.

- **Ten skins for the board, and a setting to wear one.** `graphite`, `oxide`, `moss`, `plum`, `ember`,
  `glacier`, `noir`, `vapor`, `sandstone` and `abyss`, beside the navy one the board came with.

  **A skin is not a terminal theme,** and the two are only confusable in speech. A terminal theme is sixteen
  ANSI colours handed to xterm.js, it belongs to one terminal, and two terminals side by side may reasonably
  differ. A skin is the chrome around them, there is one of it, so it is a daemon setting and it follows to
  another browser and to a phone. The picker for it sits under `the board`, not on the terminal bar, and says
  which of the two it is.

  **The reason this was not a two hour job is that the palette was only half a palette.** Every colour in the
  page was a variable, and about sixty tints of those colours were a SECOND copy written out as `rgba(...)`,
  because CSS cannot take a hex and add alpha to it. Overriding the variables therefore repainted the solids and
  left every tint behind at the old hue, which came out as a dozen surfaces staying navy under a black skin. So
  the definition is now the channel triple and the solid is derived from it, `--bg-2: rgb(var(--sink-rgb))`, and
  a skin sets one value per colour with both forms following. The one hex in the stylesheet with no name at all,
  a button's hover, has a name now.

  What a skin may move is the palette and nothing else. The type ladder, the space ladder, the radii and the
  shadow geometry are layout: `--uiscale` and `--density` already answer the size question and they belong to the
  operator, not to the skin. All ten are dark, because `--lift` and `--hairline` are white at low alpha and every
  shadow is near black, so a light board needs a second set of those rather than a different palette. It is a
  separate piece of work and it is written down as one.

  Three things could have named a skin: the stylesheet, the daemon's validator, and the picker. The picker is
  built from what the daemon sends, so there are two, and `scripts/check-skins.sh` fails when they disagree. It
  also asserts every skin sets the same twenty seven variables, because a skin missing one does not fail: it
  falls through to `:root` and leaves a single element wearing the old palette, which nobody finds for a month.

  An unknown skin is refused at the endpoint rather than stored, unlike every other setting beside it. The
  others store what they are given because a browse root that does not exist yet is a list somebody is preparing.
  An unknown skin can never become right, and it fails silently: it saves, it goes on the body element, no rule
  matches, and the board looks exactly as it did. The refusal names the ones that would have worked.

  The chosen skin is also remembered in the browser, which is not the source of truth and exists only to kill the
  flash of navy on every load while the daemon is asked. Popped-out terminals get told over the same broadcast
  channel that carries a restart, since one window in the old colours beside one in the new reads as a bug in
  both.

- **Priority on a card.** The one judgement on a board where everything else is a fact: what a runner is doing,
  how long it has waited, which repository it is in. Some work cannot be got rid of and has to stay top of mind,
  and nothing could say so.

  `high`, normal, `low`, set from the card menu. Three levels rather than a number, because three never need a
  tie break and a number turns into something to fiddle with that nobody can read a week later.

  **It never sorts.** Sorting by priority would bury a `needs-permission` card under a high-priority one that is
  doing nothing, which inverts the entire point of the first column: a blocked agent is blocked whatever you
  think of the work. It is weight on the card, plus `!high` and `!low` in the stack search, and clicking the chip
  filters to it the way a tag already does.

  **It fades after a week.** Something marked high a month ago and untouched since is not high, it is forgotten,
  and a priority that never expires becomes a field where everything is high. Read in the browser from
  `priority_at` and never written back: nothing acts on priority, so there is no moment for a server-side expiry
  and no third timer beside the two in `sweep.go`.

  **An agent cannot set it,** and it turned out there was nothing to refuse: the agent listener has no route
  that patches a card. A `suggested_priority` on an offered item was designed and dropped, because an offered
  item IS a card and the only place to put the suggestion was the thing it must not touch. A source that wants
  to say something is urgent uses a tag, which a human then reads and decides about.

  It is not `pinned` and the two stay apart. Pinned is a boolean meaning "always show me this", which is right
  for a fixture; five pinned cards are five equals. Bolting an ordering onto it would quietly turn pinned into
  priority-1 and lose the fixture case.

- **Three defects in `scripts/atrium-autostart.ps1`.** They only bite at a reboot, which is why they lasted.

  It defaulted to `build.claude\atrium.exe` in the repo, a path that moves with the checkout, is rewritten by
  every build, and cannot be written at all while the daemon is running from it. It now looks for `atrium` on
  PATH, falls back to `~\.atrium\bin\atrium.exe`, and refuses to register a task naming a binary that is not
  there rather than registering one that fails silently at the next logon.

  Both the trigger and the principal took `$env:USERNAME`, a bare name with no domain. On a domain-joined
  machine or a Microsoft account the identity is `DOMAIN\user` or `MicrosoftAccount\you@example.com`, and a bare
  name either fails to register or registers against nothing and never fires. It happens to work on a local
  account, which is why it survived. It now uses the identity Windows itself reports.

  A comment claimed `-WindowStyle Hidden` and `New-ScheduledTaskAction` has no such parameter, so a console
  window appeared at every logon and sat there waiting to be closed by accident, taking the daemon and every
  supervised runner with it. It launches through `conhost.exe --headless` now, and says so plainly when that is
  not available rather than pretending.

  A fourth, found while fixing those: `-Force` replaced the registration and left a running instance alone, so
  re-running the script while atrium was up registered a task that would start a SECOND daemon on the same
  database at the next logon. The task is stopped first.

- **`make check`** runs the tests, `scripts/check-board.sh` and `scripts/check-powershell.ps1` together. All
  three already existed and nothing ran them as a set, so the two that are not `go test` were easy to skip. The
  parse checks matter here because neither thing they cover can be verified by trying it: the board is one
  embedded HTML file with no build step, and the scripts register a scheduled task and reach a network.

- **Hooks stop drifting away from the daemon that is running.** The board reported six wired, working hooks as
  `points elsewhere`, and it was right: they named `build.claude/atrium.exe` while the daemon ran from an
  installed copy. The cause is self-inflicting and was in three files at once. `atrium hook install` resolved
  the path with `os.Executable()`, which is the binary YOU TYPED, so installing from a fresh build wrote the
  build directory's path, and the next rebuild put the drift straight back.

  `os.Executable()` is correct in exactly one place: inside the daemon, where it is the daemon. So the daemon
  now records its own binary in the location file it already writes at startup, beside the port every hook
  already reads to find it, and `claudeconf.HookExe` resolves `ATRIUM_HOOK_EXE`, then that, then the caller's
  own path. The three duplicated resolvers call it.

  `points elsewhere` also now says both paths in its tooltip, since the old one named neither and the only way
  to act on it was to open settings.json and compare by eye.

  An `atrium install` subcommand was written for this and then removed before shipping. Copying a file to a
  fixed path is the shallow half of installing: no version, no uninstall, no PATH entry, no upgrade, and a
  home-directory convention invented here that no packaging format would have agreed with. Proper packaging is
  on the backlog. Nothing in the fix above depends on it: a daemon reporting its own binary is correct however
  that binary got onto the machine.

- **The event log answers with the newest events.** `GET /v1/tasks/{id}/events` selected `ORDER BY at ASC
  LIMIT ?`, which is the OLDEST N: on a card with a thousand events a limit of two hundred answered with the day
  the card was created. It is useless for the question the endpoint is for, which is what just happened, and it
  had produced three wrong conclusions before anybody looked at the query. The limit now applies to the newest
  end and the window is reversed in the store, so the wire shape is unchanged and the timeline needs no edit.

- **The directory picker is bounded.** `GET /v1/browse` applied `filepath.Clean` and nothing else, so every
  directory the daemon's user could read was listable. On loopback that is a directory picker and it was fine.
  Over a share it was an unauthenticated recursive listing of the machine, because a share terminates here and
  every request over one presents as loopback, so nothing can be decided by address.

  It now resolves through `internal/safepath` against a root set: symlinks followed on both sides, comparison on
  a separator boundary, and one answer for outside, missing and unreadable so the refusal is not an oracle for
  what is on the machine. The default set is your home directory plus every directory a card, fixture, source or
  harness already names, which is where the picker is actually opened; `browse_roots` under settings widens it.
  With nothing open it offers those roots instead of the machine's drive letters.

  Two things went with it. `useBrowsed()` always wrote into the launch form's box and ignored which field opened
  the picker, so choosing a directory for a fixture, a source or a harness filled in the wrong one. And the two
  platform files that listed drive letters had no caller left, so they are gone rather than waiting to be
  restored by somebody reading their absence as a bug.

- **Lend one session to one person.** Right click a card, or the terminal's cog: `share this session…`. Public or
  private zrok, and whether they can type or only watch. What comes back is an address to send somebody, and a
  public one carries the card in its fragment so the link opens straight into the terminal.

  The part that matters is what a guest CANNOT reach. The daemon serves a separate restricted handler on that
  share rather than the board with a filter over it, and the surface is an ALLOWLIST: the page and its assets,
  `/v1/health`, a GET on that one card, its attach socket, its icon. Everything else is 403, including endpoints
  that do not exist yet. `/v1/tasks`, `/v1/events`, `/v1/permissions`, `/v1/settings`, `/v1/browse` and file
  read/write are all named in the code as refused on purpose, so nobody has to work out whether they were
  forgotten.

  Read-only is enforced on the SOCKET, not by hiding a control. A guest owns their copy of the page and can send
  whatever frame they like, so the only thing that decides is the end that reads them. Permission prompts stay
  with you whichever mode you pick.

  The address is deliberately not reserved. The board's address is one you keep and bookmark; a lent session's
  address IS the credential, since there is no login, so it is fresh every time and dies when you stop the share
  or when atrium restarts.

- **What zrok said, turned into what to do about it.** A zrok account has caps on shares, on environments and on
  transfer, and the free tier is reachable in an evening. What came back through the generated client was often
  the HTTP status and nothing else, so the board showed `unexpected response 429`. Limits, a revoked token, a
  name already taken, an unreachable instance and the instance's own 5xx each get a sentence about the next step,
  with the original always appended, because the classification is a guess.

- **A restart parks every other agent first.** Restarting closes every terminal atrium owns and kills the process
  in each one. The session that asked for it signed up for that; nothing else did. So `restart_atrium` asks the
  board what is supervised and working, excluding itself via `ATRIUM_TASK_ID`, queues each one a message saying
  what is about to happen, and waits up to ninety seconds. Anything still working means NOTHING is scheduled and
  the busy sessions come back named. `force` overrides.

  `running` is not the test: a card sits in `running` from launch until something moves it. It asks the live
  activity the daemon tracks, treats `needs-input` and `needs-permission` as already parked, and counts "I cannot
  tell" as busy.

  The restarter also installs a staged `atrium-control`, not just a staged `atrium`. Swapping only the daemon
  meant a change to the control server staged forever: new daemon, new claude, and claude spawns the OLD control
  binary.

- **One alert per event, and a popped-out window that comes back on its own.** Three bugs with one shape.

  The rule "a card with its own window is announced by that window" was written down and applied in exactly one
  place: the desktop notification was suppressed and the chime and the toast beside it went ahead anyway. Both
  documents rang for every event.

  The board also asked who was popped out and then did its first poll and its restore without waiting for the
  answers, so a restart, which reloads every document at once, had it ringing for cards whose windows were about
  to ring, and taking a terminal back out of a window that owned it. A popped-out window now claims its card as
  its FIRST act rather than after a round trip and a WebGL context, and the board waits for the answers.

  And a popped-out window no longer tears itself down when its runner exits. The wind-down stops supervised
  runners while the HTTP listener is still up, so "is the daemon there" answers yes during the first second of a
  restart and the exit reads as final. That window IS that card: it retries, and a watchdog on the poll attaches
  whenever the pane is empty and the card has a terminal, which needs nothing to have survived.

- **Compacting no longer leaves a card in `needs-input`.** It used to stay put, on the reasoning that compacting
  says nothing about whether a human is wanted. That is wrong in the one direction that matters: compaction
  happens because a session is BUSY. The card was left in the column that means "answer me" until the next tool
  call moved it.

- **The session list is a title again, and it takes the room you give it.** Three things landed on the terminal
  view's left column.

  The name in each entry had been rendering grey, monospaced and clamped, which is what a path looks like, so the
  list read as a column of directories with no titles at all. `.term-entry span` matches the name as well as the
  path, and was handing every span in the card the dim monospace the path wants. The name now declares its own
  font and colour, with a note that anything named on `.term-entry span` has to be answered there.

  The column is **draggable**, between 150 and 520 pixels, and it has two collapsed modes past that. `mini` keeps
  the runner's mark, eight characters of `repo:branch`, and the colour of the left edge, which is what actually
  carries "this one wants you". `off` gives the terminal the whole pane and leaves a strip to hover. In that mode
  the list slides OVER the terminal rather than pushing it, so a pointer crossing the left edge never re-fits
  xterm, which measures itself in characters and repaints the whole grid to do it. All of it is localStorage: a
  width is a fact about this screen, and one synced from a 32in monitor is wrong on a laptop.

- **"raised it for you" now raises it.** A board that had reloaded held no handle to a popped-out window, so it
  asked the window to focus itself over the broadcast channel. That never worked: `window.focus()` in a
  background tab has no user gesture behind it and every browser refuses it, silently. The board is the one
  holding the click, so the board raises the window by NAME, with `window.open("", name)`, which returns an
  existing window without navigating it and keeps the scrollback. A free name opens a blank window instead, so
  the answer is checked and an unwanted one closed again. `popOutTask` reports whether it raised or opened, and
  the toast says which, because a claim can be a heartbeat stale.

- **`docs/reload-design.md`**: how a new daemon gets installed by an agent the daemon is currently running. The
  scheduled restart, why detaching is not `run_in_background`, the rename-aside binary swap that Windows permits
  when a delete would fail, and why the board reloads itself on a hash of its own HTML.

- **A pasted screenshot no longer has to live in your repository.** Two settings under "this machine": a pasted
  file either stays in the card under `.atrium/incoming`, which is what it did, or goes to a scratch directory
  beside the database that is emptied every time the daemon starts. The path is what reaches the agent either
  way, and the words in front of it are yours to set, defaulting to "check out the image here: " rather than a
  bare path dropped into the line.

  What is NOT on offer is handing the bytes straight to the model. A pseudo terminal carries input characters
  and an image is not one; Claude Code gets a pasted image by reading the clipboard itself, on its own machine,
  which is exactly what a browser on another machine cannot do. The file has to exist somewhere, so the only
  real question is where and for how long.

  The scratch directory is cleared on the way UP, not on the way down, because a daemon that was killed never
  runs its own cleanup and the guarantee wanted here is that the pictures are gone.

- **The terminal draws on the GPU, and the board tab went from 26% of a CPU to about 7%.** xterm has no renderer
  of its own beyond a fallback, and the fallback is the DOM: a `<span>` per styled run per row, rebuilt every
  frame, plus a generated stylesheet of 256 color rules. Only `xterm.js` and the fit addon were vendored, so
  that fallback was what every terminal had been using.

  The version was not knowable from the bundles, which carry no version string, so it was established by
  downloading candidates and hashing them: the vendored files are byte-identical to `@xterm/xterm@5.5.0` and
  `@xterm/addon-fit@0.10.0`, which makes `@xterm/addon-webgl@0.18.0` the matching pair. `vendor/VERSIONS.md`
  records all four with their hashes, and the trap: the check is not "does it work", because the DOM fallback
  works too.

  Two more sources went with it. `refresh()` was bound straight to the event stream, and a working agent
  publishes a task event on every tool start and end, each costing three fetches and a rebuild of every card's
  markup; those coalesce into one refresh per burst now. And four infinite CSS animations ran forever, two of
  them on every matching card at once and two of them animating box-shadow, which forces a full repaint rather
  than compositing. What is left is one opacity pulse and one header icon, and reduced motion now names the
  animations in one block rather than the chips somebody remembered.

- **Resuming asks which conversation, when there is more than one.** A card carries the last session atrium saw
  on it, which is the right default and not the whole truth: a directory accumulates conversations and the one
  worth picking up is often not the most recent. The picker shows the title Claude Code generated, the age and
  the size, and marks the one the card would have taken on its own. One conversation is not a question and
  resumes straight through; zero is not either.

  Conversations can be forgotten from the same list. Deleting the transcript is the whole operation, and a card
  pointing at one that has gone starts fresh instead.

  `askUser` grew a real `select` for this. Building it on the existing datalist reproduced the defect that made
  the theme picker unusable: a datalist filters on what is already in the field, so a pre-filled value hides
  every other option, and here the values are uuids.

- **A page notices when the daemon is serving a board it is not.** `Cache-Control` already stopped a stale
  load; this is the other half, which is that a page ALREADY OPEN keeps the JavaScript it loaded. A restart
  replaces what is served and touches nothing running, so a popped-out terminal left open since before a fix is
  still executing the old code, and the symptom is a fix that works in every window opened afterwards. The
  binary hashes the board it carries into a build id, `/v1/health` returns it, and a page that sees a different
  one reloads. Reloading a popped-out terminal costs the scrollback and nothing else.

- **A file can be edited in the board, and a download can be a selection.** `get all` was the wrong shape:
  "everything under here" is a guess usually wrong by a build directory, and it made the case anybody has, four
  files out of two hundred, unreachable. Tick what you want and the button says how many. The zip endpoint takes
  repeated `path` parameters and names entries relative to the deepest directory they share.

  The editor is a textarea, for a short list of extensions, refusing anything that is not UTF-8 or is over 2 MiB
  rather than mangling it. **A write carries the hash of what was read and the daemon refuses if the file moved
  on**, which is what makes editing a file an agent is also editing safe to offer: without it a save is
  last-write-wins at machine speed and the loss is silent. Optimistic, not a lock.

- **`swapStaged` renames the outgoing binary aside instead of deleting it.** Windows refuses to delete a running
  executable and permits a rename within the same directory, which is how every self-updater on this platform
  works. Without that, installing a rebuilt daemon meant a shell outside atrium with the daemon stopped, which
  is the chore `restart_atrium` exists to remove.

- **Smaller:** the terminal bar keeps the board's colors while the pane takes the session's theme, and the
  scrollbar takes the theme's own selection and cursor colors; a `tightest` density; pinned cards are their own
  group at the top of a column rather than sorted within a project group they also appeared in.

- **`atrium control`: an MCP server that can restart the daemon from inside a session the daemon is running.**

  The chicken and egg. A supervised runner cannot restart the daemon, because the daemon owns that runner's
  pseudo terminal and closing it takes the runner with it. Whatever does the restart has to outlive both, so it
  is neither of them: a stdio MCP server, usable as a subprocess of a claude session, as an mcp-gateway backend,
  or both, with two tools.

  **`restart_atrium` schedules, it never restarts immediately, and that is the load-bearing decision.** A tool
  call that kills its own caller never returns: the session is resumed later holding a tool call with no result,
  a state nothing has tested and the model cannot reason about. So it spawns a detached process, answers
  `scheduled`, and the turn ends normally before the floor goes away.

  The database is captured while the daemon is still up, because `atrium stop` deletes the address file on its
  way out. Reading it afterwards would find nothing and the new daemon would take its database from whatever
  environment the detached process inherited, which is the exact confusion `--db` exists to end. The restarter
  then waits for the port to actually close rather than sleeping a guessed amount, since the wind-down gives
  supervised runners ten seconds and starting inside that produces a second daemon that cannot bind.

  **A side effect worth more than the feature:** the daemon it starts is detached, with no console and no parent
  lifetime, so it survives the session that asked for the restart. One restart is enough to stop the daemon
  dying with whatever terminal happened to start it.

  `atrium_status` answers without a daemon too, which is the case worth being able to see: `running:false` plus
  where the last one was, rather than an error.

- **The board can tell a session that ASKED you something from one that merely finished.** Both landed in
  `ready` and read identically, so a question put to you two minutes ago sorted below twenty sessions that had
  run out of things to do overnight.

  The signal was already arriving and was being thrown away. Two hooks reach the same handler: `Stop` fires when
  a turn ends, and `Notification` fires when Claude Code is blocked on you. The code flattened them with a
  comment saying they were the same thing to a board. They are not: one is an agent that stopped and will sit
  there costing nothing, the other is work that cannot continue until you answer.

  A second, independent signal for the same fact: asking is a TOOL CALL, so `PreToolUse` sees `AskUserQuestion`
  and `ExitPlanMode` by name before the turn ends, and the mark is recorded at the moment the question is asked.
  `SetStatus` carries it forward, because the two facts arrive in the wrong order: the tool call happens while
  the card is still running, and the hook that causes the wait lands afterwards with nothing to say about it.

  The card says `asked you` instead of `ready`, the notification says so, and `waiting on you` sorts questions
  above duration. Leaving a waiting state clears it, so an answered question stops being reported.

- **Groups are blocks, not headings with a bar.** A tint of the group's own hue plus an outline, from two
  variables in `:root` so the strength is one decision made in one place: `--group-fill`, `--group-edge`, and
  `--card-tint` for the much weaker trace carried by the cards inside, which must not compete with the left edge
  that carries status. The stack uses the same treatment; it had only the bar, so the two views coloured one
  grouping differently.

- **Durations pad to two digits** on every unit but the leading one, so `6h09m` and `21h04m` put their `h` and
  `m` in the same place down a column. Chips and the stack's age column ask for tabular figures for the same
  reason: a proportional `1` made every pill breathe as the minutes ticked.

- **The terminal switcher shows a session's whole address**, the same one the bar and the popped-out window
  show. It said `main:dotfiles` beside a terminal headed `github/dovholuknf/dotfiles:main`, which reads as two
  sessions. The star, the runner mark and the name are one row rather than three, and hovering no longer lifts
  the entry: a card that moves as you reach for it is one you click the wrong part of.

- **Column tooltips rewritten.** Shorter, no references to the `gwt` session ledger, and two stale facts gone:
  `running` claimed cards move to finished after three hours, and `finished` described `dead` as any process
  that is gone rather than one whose directory has gone too.

- **British spellings out of authored prose**, across 41 files. `centre` and `grey` deliberately left: both
  appear inside CSS keywords and identifiers where a blind rewrite renames a variable.

- **The terminal's horizontal gap is a margin, not padding.** xterm fits to `#t-screen`'s content box and draws
  its viewport, scrollbar included, across the full width of it. Horizontal padding sits inside that box, so
  the bar landed on the last column of text and the right-hand characters read as running underneath it. A
  margin is outside the box: xterm gets a narrower element, fits it exactly, and the gap is still there.

  Found in devtools against a live popped-out terminal, which is the only way anything about xterm's geometry
  gets settled here. Three guesses from the code missed it.

  The same shorthand was hiding in `@media (max-width: 900px)` as `padding: 6px`, so the fix was undone for
  every window narrower than 900px, which is every popped-out terminal. That block also carried
  `.term-bar strong { width: 100% }`, written for a phone, where an extra row on a bar costs nothing. At 894px
  it pushed every button onto a second row, and a bar that grows a row changes the terminal's height under a
  grid xterm has already measured in characters. The name truncates instead.

- **The stack's date column follows the sort.** It was `created_at`, when atrium first saw the card, while the
  list was ordered by last activity, so it read `today, today, yesterday, today`: correct about two different
  facts and indistinguishable from a broken sort. It now shows the moment the big number is counting from, so
  one says how long ago, the other says when, and the order is that same moment.

  With seconds, for today and yesterday. The order resolves to the second, and two rows a few seconds apart
  both reading `10:23` looked like the sort had given up when the sort was right and the clock was rounding.

- **Every scrollbar on the board is styled by one rule.** Three attempts at this styled the boxes somebody had
  noticed, and there are twenty-odd: the stack, the perms list, the runners page, each board lane, the file
  lists, the session switcher, every dialog body, the overlay log, every wide code block, the nav on a narrow
  window. The sixteenth kept arriving as the operating system's grey strip. A new scrolling box inherits this
  now without anybody remembering to come back.

  Two things make it safe to write universally. The terminal is carved out by name, because the xterm fit addon
  measures its viewport's scrollbar to decide how many columns fit and anything that changes that width makes
  it draw rows the text then runs underneath. And the selectors use `:where()`, which scores zero, so the three
  places that hide their bar on purpose still win: a universal rule is a default and has to lose every
  argument.

- **The file browser is shaped like a repository listing.** One directory at a time, a breadcrumb across the
  top, folders first, and the size and the time in their own right-aligned tracks so the eye can run down
  them. A tree and Miller columns both want horizontal room, and the place this matters most is a popped-out
  window at 900px.

  The breadcrumb replaces the `up` button as the way back. `up` said nothing about where it went, and getting
  back from three deep was three presses of it. It stays as an arrow beside the crumbs, since one step back is
  the commonest move and should not need aiming at a word. The trail is trimmed to the card's own directory:
  everything above the worktree answers `403` anyway, and drawing `C:` as a step you could press would offer a
  walk that always fails.

  Six file glyphs, drawn here rather than vendored. An icon theme is several hundred SVGs for extensions this
  board will never see, and the board has to work offline, which is the same reason xterm is vendored rather
  than fetched. The question a glyph answers in a listing is "which of these is the code and which is the
  readme", and color carries most of that: telling `.ts` from `.tsx` at 11px is not something an icon can do.

  The two actions appear on hover but hold their place in the layout, so a row's contents never move under the
  pointer.

- **The stack's date fits.** The track was 74px, `yesterday 16:06` needs about 98px, and a fixed track narrower
  than its own content is the one width that cannot work: the text has nowhere to go and the row clips it.
  Sized in `ch` now, so it stays right when the text size setting moves.

- **The board paints its own scrollbars.** The stack, the perms list, the runners page and every board lane
  arrived as the operating system's grey strip, which on a dark board is the one part of it that is not the
  board. The terminal has been painting its own since it got a viewport; this is the same treatment for the
  rest, set in one place so they cannot drift apart.

  With room reserved for it. A scrollbar is drawn inside the padding box, so a row's right border was sitting
  under the bar and the two mushed together. The negative margin gives that space back to the layout, so
  nothing shifts when a list first grows long enough to need one.

- **Starting the fixtures says one thing, and only when something went wrong.**

  Every fixture that comes up produces a card that is ready, and a ready card is announced, so booting was one
  notification per terminal for an event with no content: you configured them to start, and they started.

  Two halves to the fix. A card that is ready BECAUSE IT JUST STARTED never rings, whoever launched it, which
  is the general rule the fixtures case is an instance of. And the batch reports the other half once, since the
  absence of a card is not an event and the board cannot notice it: `3 fixtures did not start`, naming them
  when there are few enough to name, silent when everything worked.

  **A fixture that failed now says why, on its own row.** Fixtures start in the background so a slow runner
  cannot hold the board up, which meant a failure had nowhere to go but the daemon's log and the only symptom
  was a terminal that was not there. The same shape `source` already uses: the row that failed carries its own
  reason, so the page listing them is the page that answers why one is missing. It stays up until a start
  works.

- **The terminal bar wears the runner's mark and hides its settings behind a cog.**

  The mark goes in front of the name, the way a card carries it. A `claude` pill among the chips said the same
  thing in the place the eye goes last, and read as one more fact rather than as whose terminal this is.

  Behind the cog: the palette, the notification mark, whether selecting copies, and how big this card's window
  opens. `copy on select: off` was a permanent five word sentence taking the same room as `exit`, for a setting
  changed about once. The bar was running out of room and worse in a popped-out window, which is narrow by
  design and is exactly where per-window settings belong.

  **A popped-out window's size is set on purpose now**, from that cog: remember it for this card, or make it
  the default for every card. It used to save on every resize, so nudging an edge to see something behind the
  window silently became the size every card opened at. A preference nobody expressed is worse than no
  preference, because it is indistinguishable from a bug.

- **The card menu, again, against what the entries actually do.**

  **Attach is one row with two destinations**, `in terminals` and `in its own window`, on a flyout. They are
  one intention with a different endpoint, so two rows both beginning "attach" was the same word twice.

  **Auto mode reports what is happening, not what is configured.** A card read `off` while the header said
  APPROVING EVERYTHING, because the board-wide switch and the per-card one both end in approve and the menu
  was describing a field rather than an outcome. It says `on`, noted `board-wide` when it is not this card's
  doing, since otherwise turning this card's switch off appears to change nothing.

  **Resume is offered only when it can resume.** An entry that says "cannot resume" underneath itself is a
  menu explaining why it is there, which is a question it raised.

  **Start says what it will do.** With nothing running it puts a runner on this card. With a session already
  going it cannot, so it says `start a NEW session here` and opens a second card in the same directory: a
  different conversation, the same files, two agents editing them. Occasionally what you want and never what
  you want by accident.

  **Shelve is dimmed rather than dropped** on a card atrium does not own, with the reason on the row. An entry
  that disappears on some cards and not others is a rule you have to infer.

  **`details…` is `settings…` and sits last**, before terminate. It is the one entry you go into rather than
  press. The notification mark moved inside it, next to the bell, since those answer the same question in the
  two senses: which agent when you cannot see, and which agent when you can. It is drawn as you type, because
  a glyph that renders as a box there will render as a box on the notification.

  **Forget left the menu** for that dialog. It is not something done often, and it was sitting under the
  pointer next to things that are.

- **A session that has just come up no longer claims to have finished work.**

  `needs-input` is reached two ways that mean opposite things. A session that has just started is ready because
  it has done nothing yet; a session that has handed a turn back is ready because it did what was asked. Both
  said "finished its turn and wants your next instruction", so launching a runner announced work it had not
  begun. A new `waiting_reason` on the card carries which, set by the session hook, empty everywhere else, so
  every card written before this reads correctly with no backfill.

  A column rather than a read of the event log: the waiting list is polled every five seconds and the answer
  is one fact per card, so asking `event` what happened last before each status change is a query per card per
  poll to learn something the status change already knew.

- **A card can wear its own mark on a desktop notification.**

  Beside the theme and the tone, for the same reason both of those live on the card: telling sessions apart
  without reading only works if the answer is the same tomorrow and in another browser. A notification arrives
  with the operating system's own chrome around it and one small image, and every one of them carried the same
  A, so the picture said only that atrium sent it.

  Free text, not a list from here. A letter, a digit, an emoji, anything that renders in one glyph, which
  makes the set of marks as big as the emoji keyboard rather than as big as whatever could be drawn in this
  file. A fixed set would be atrium deciding what a project may look like, the mistake `tags` already refuses.

  The kind of alert is still carried, by the field the mark is drawn on: amber for an agent that is blocked,
  dark for one that is merely ready. So the notification now says which session AND how urgent, where it said
  neither. Whatever is on the card is drawn to a canvas and sent as a PNG, never inserted, so an icon cannot
  be markup however it was set, including by a source that filled the card in.

- **The card menu is ordered by what you came to do, and says what is on.**

  Look at it, get to its terminal, run something in its directory, change what it is, then the dangerous ones.

  **Start a session here** and **resume the conversation here**, on any card with a directory. This is what
  adopting an existing session actually amounts to: a runner atrium started in a terminal cannot be taken
  over, because a pty cannot be adopted and `docs/supervision-design.md` records that there is no reattach on
  Windows. What can happen is a new runner, owned by atrium, in the same place and onto the same card. A card
  with a worktree and no session is exactly the case for it.

  **Auto mode is drawn as a toggle**, label plus a lamp, rather than flipping between "auto mode" and "stop
  auto mode". The old wording asked you to work out which of two sentences described now and which described
  the click, and it was least readable in the case that matters, when the answer is already yes.

  **Shelve is offered where there is a runner to stop.** On an unsupervised card it promised something atrium
  cannot do: set the standing block and leave the process running. Unshelve stays available on any card,
  because a shelved card refuses every request its session makes and has to be liftable wherever you find it.

  **Done is gone from the menu.** Dragging a card into the column still files it.

- **The stack's date sits last, in a fixed track.** The chips vary in number and width from row to row, so a
  date carried along at the end of them landed somewhere different on every card and the eye could not run
  down the column. Fixed width for the same reason: `Thu 4 Sep` and `Mon 12 May` differ by a character and
  would shift the whole track. It is the first thing dropped when the row is narrow.

- **A popped-out window opens at the size you last gave one, and stops announcing itself on arrival.**

  **It rang on open.** The first poll compared "is this card ready" against a starting value of `false`, so
  every rising edge included the very first one, and a card that had been sitting ready for an hour announced
  itself the moment you got there. Which is why you popped it out. The first poll now only learns, matching
  what `alerting.check` has always done on the board. The state still reaches the title bar, since a window
  that opens onto a blocked session should say so; nothing is played and nothing is put on screen.

  **The toast spanned the whole window.** A popped-out window is around 900px, which falls inside the phone
  breakpoint, so a rule written for a phone stretched the toast edge to edge across a window whose entire
  content is one terminal. Narrow is a shape, not a screen. It is also narrower here than on the board,
  because this window carries the alert in its title bar and what is on screen only has to be legible.

  **Size is remembered, per card, by resizing the window.** Not a settings field: the size you want is
  something you find by dragging an edge until the terminal looks right, and having found it there is nothing
  left to ask. A card popped out for the first time opens at whatever you last sized any window to, so a card
  you keep tall and narrow beside an editor stays that shape and everything else inherits a sensible default.
  Kept in `localStorage` rather than on the card, because it is a fact about this screen: the same card on a
  laptop wants a different window, and a size synced from the desktop would be wrong on arrival.

  **Pop out is on board cards too**, next to `attach`, the same mark as on the stack.

- **A stack row can pop a terminal straight out, and stops saying the same number twice.**

  Three things about one row.

  **Pop out sits next to attach**, as the box-with-an-arrow mark that means "new window" everywhere else.
  Attaching first and then popping out was two steps to reach one window, and it made the board briefly the
  owner of a terminal it was about to give away. Popping out a card from the stack now leaves whatever the
  board already has attached alone, which an unconditional detach did not.

  **The state chip drops its duration when the big number on the left already is it.** Under the default sort
  those are one measurement: the left column shows `wait_seconds` for a waiting card and `idle_seconds`
  otherwise, and the chip printed exactly that again. It is compared rather than assumed, because the two come
  apart under the other sorts. Sorting by activity makes the left number "how long since it last did anything"
  for every row including the waiting ones, and there the wait is a fact nothing else on the row carries.

  **The date is centred with the pills** instead of trailing the card's name. The name is the first of up to
  three lines in its column while the chips are centred against the row, so the date sat on line one and the
  pills on the middle line. Each half was centred correctly on its own terms and the row still read as
  crooked.

- **A popped-out window stays on its terminal, and alerts for it there.**

  Two fixes to the same window, one of which was a defect and one of which was missing.

  **It could wander back to the board.** A popped-out window has no tabs, so nothing looked like navigation,
  but three paths reached `switchView` anyway: a tag chip calls `filterByTag`, the in-terminal permission block
  offers "open in perms", and a launch that lands on an unsupervised card falls through to the board. Any of
  them turned the window you alt-tabbed to into a second board. `switchView` now refuses anything but the
  terminal while the window is popped out, and the other views are hidden in the stylesheet as well, which also
  removes the flash of board while the card is being fetched.

  **It said nothing when its own session needed you.** It never opened an event stream at all. It does now, and
  polls for ONE card rather than running the board's whole alerting pass, which it would have done from a
  document with no board to click through to.

  **The title bar carries the alert, not a toast.** `(!) github/openziti/ziti:nightly-failures - atrium` for a
  blocked agent, `*` for one that has finished its turn. The whole reason this window exists is alt-tab, and a
  toast lives inside a window you may not be looking at while a title bar is what the switcher shows. Coming
  back to the window clears the mark, because that IS reading it. The toast is still shown, but only when the
  window is already in front of you.

  **Which document speaks is decided by ownership, announced over a `BroadcastChannel`.** A popped-out window
  claims its card and the board holds back its own desktop notification for it, or one event rings twice: by
  its own foreground test the board is behind the popped-out window and therefore entitled to notify. The board
  keeps that card's badges and counts, because routing a card off the board entirely would mean a window
  buried on another desktop silently ate every alert for it. A board that starts later asks who is out there,
  and windows that are gone do not answer.

- **A terminal is named by its whole address.** `github/openziti/ziti-tunnel-sdk-c:nightly-failures` rather than
  `ziti-tunnel-sdk-c/nightly-failures`. A card in a column has the board around it saying which machine and
  which project; a window in alt-tab has nothing, so it needs the host and the org too. The same label is now
  on the terminal bar, since the switcher beside it lists sessions whose short titles collide.

  Anchored on the **repo name**, not on path depth and not on the last path segment. Depth varies, and the last
  segment is the worktree DIRECTORY, which is not the branch: one card here sits in
  `.../desktop-edge-win/fix-app-version` while actually being on `promote-2.11.3.1-and-beta`, because somebody
  switched branches inside the worktree. The directory is the stale half, so the branch is what gets shown.

  `scripts/check-terminal-titles.js` runs the rule against a live board, and earned itself immediately: two
  cards with no repo recorded came out as `openziti/ziti/discourse-6036:discourse-6036`, naming the branch
  twice. Neither was visible by reading the function.

- **Something starts the daemon now.** Nothing did. The one on this machine had been started by hand once and
  left running, so `atrium stop` looked like losing everything, and starting it again from a different terminal
  opened a **different database**, because which one you get depends on `WORKTREE_ROOT` in the shell you were
  in.

  `scripts/atrium-autostart.ps1` registers a logon task that pins one command line and one database, and
  `-Remove` takes it away. The `--db` is passed explicitly on purpose: leaving it out means the task inherits
  whatever the environment happens to be at logon, which is the thing that caused the confusion.

  A logon task rather than a Windows service, deliberately. A service runs as SYSTEM in session 0, which cannot
  open a pseudo terminal a person can attach to, and supervision is most of what the daemon does. No execution
  time limit either: the default is three days, after which the task host stops it and the board vanishes for no
  visible reason.

  Also `scripts/check-powershell.ps1`, because the two source scripts shipped last night had never been parsed
  by anything. They do parse. Nothing was checking.

- **An approval you can answer from a phone, without atrium growing a push service.** `docs/charon.md` calls
  this the gap that matters most: a gate you cannot answer from away is a gate you turn off.

  **Web push was the plan and it was dropped**, because it breaks three written rules at once. `CLAUDE.md` puts
  authentication out of scope and says reaching the board from elsewhere is an overlay's job. `docs/overlays.md`
  says atrium never issues an identity, and a VAPID key pair is one it would mint and hold. And it would be the
  first secret this daemon keeps, where `schema.go` says of the source table that there is nowhere in it to put
  a credential.

  The answer that needs none of that is the one already built: the phone opens the board over the overlay that
  already reaches it. So what was actually missing was that this screen was unusable at 390 pixels.

  A breakpoint aimed at the permissions queue rather than the whole board, because reading a kanban on a phone
  is not something anybody wants and answering a blocked agent from the sofa is. The four buttons go two by two
  at full width rather than being shrunk to fit one row: approve and never side by side at eight millimetres is
  how the wrong one gets pressed. The tabs scroll instead of wrapping, and the two widest things in the header
  are hidden, neither of which is what you came for.

  A pending request also says **how long it has been frozen** rather than the wall-clock time it asked. "asked
  14:22:01" needs you to know what time it is and do the subtraction, which is exactly the work nobody does on a
  phone at three in the morning. Not a countdown: atrium refuses approval timeouts, so nothing is running out.

- **Sessions can address each other.** `CLAUDE.md` said atrium had no answer for this at all, and
  `docs/charon.md` ranked it first of six things worth taking.

  `atrium peers` lists who is reachable, with what each is working on and how much is already queued for it, so
  a session that is already buried is one to leave alone. `atrium tell <handle> <message>` queues one.

  Everything it needed already existed: the handle is `wire_name`, the transport is the agent listener every
  hook posts to, the delivery is the `message` table drained by the permission and Stop hooks. So a peer message
  lands whether the target is working or idle, exactly like one of yours.

  **It is queued, never typed, even when atrium owns the terminal and could.** That is the whole difference from
  Charon's version, which injects as though the human had typed. That works there because their sessions are SDK
  turns with nobody at a keyboard; atrium owns a real terminal a person may be mid-command in, and
  `docs/supervision-design.md` settled that input is not fanned out. The tempting simplification is to reuse
  `handleMessage`, which does type, and there is a test whose only job is to catch that.

  The envelope says who sent it and says it was not the human, because a model that reads a peer's request as
  yours acts on it with an authority that session does not have.

  Guardrails, each about ten lines and each the reason Charon's works: eight thousand characters, twenty sends a
  minute per sender in a moving window, no messaging yourself, and nothing to a session that has ended. An
  unknown handle answers with the list of ones that would have worked, which is how discovery survives being a
  bare command rather than a tool whose description can make listing mandatory.

  On the agent listener, and `docs/overlays.md` still says never publish that port. A peer bus is the first
  feature that gives anybody a reason to want it reachable, and the answer is still no: two machines talking is
  the forum's job.

- **zrok is proved end to end. OpenZiti is not, and the docs now say so.** "Atrium drives an overlay" was a
  claim whose two halves were not equally supported.

  A private share was started from the board, opened locally with `zrok access private <token>`, and the board's
  real card data came back through the tunnel. Then the share was released and the token stopped resolving. That
  is the whole path: daemon, embedded SDK, zrok service, `zrok access`, HTTP. `docs/test-plan.md` section H has
  it as a scenario to repeat, along with getting a file back out through the same tunnel, which is the case the
  file panel exists for.

  OpenZiti has never been exercised past configuration, because this machine has no enrolled identity and so
  there has never been anything to bind a service as. `docs/overlays.md` says that plainly now instead of
  implying both halves work.

  **A start that has not been set up is refused before it is attempted**, in atrium's words, naming the next
  step. Without it the attempt goes ahead, the library fails, and what reaches the board is zrok's or ziti's own
  message about a thing that was never configured. Those are accurate and they answer a different question: they
  say what broke, not what to do. A configured identity whose file has since gone says "it is not there", which
  is the usual state after a machine is rebuilt and is worth telling apart from never having configured one.

- **A terminal can be popped out into its own window.** The habit this competes with is alt-tab, which beats a
  click into an app and then a click onto a tab, and which nothing could beat while a session was a pane inside
  a page. The window's title bar carries the session name, which is the entire point: it is what alt-tab shows.

  **The same page in terminal-only mode**, addressed by `#term=<id>`, not a second HTML file. `CLAUDE.md` calls
  `index.html` "the whole board, one file", and a second page would be a second copy of the xterm wiring, the
  resize handling and the attach lifecycle. Everything that is not the terminal is hidden rather than removed,
  so the switcher, the card dialog and the settings all still exist and are simply never shown.

  Popping out detaches the board's own pane, because two views onto one terminal both taking input is the
  situation `docs/supervision-design.md` says nothing arbitrates. The window is named per card, so pressing it
  twice raises the one that is open rather than opening a second onto the same session. Closing the window
  detaches and stops nothing.

  **Not** handing the session to Windows Terminal, which is the thing actually wanted. That needs atrium to stop
  owning the pty, and with it attach, the activity badge, the liveness check and stop, and there is no reattach
  on Windows to get them back. A browser window is what is available at a price worth paying.

- **The settings dialog has a spine.** It was one column in the order things were added, with four headings as
  the only structure, so finding a setting meant scrolling past every other setting and knowing roughly how old
  it was. The last change made it worse by putting `housekeeping` at the bottom.

  Four panes now: reaching it, alerts, housekeeping, the board. The grouping boxes were the worst of it, the
  tallest thing in the dialog and the least often changed, and they are now behind one click instead of under
  everything.

  **The markup is still one flow of fields.** The nav is built when the dialog opens, by cutting that flow at
  every heading, so adding a section is still a heading and its fields in order with nothing else to keep in
  step. Which is also a way to break it silently, so `scripts/check-board.sh` now asserts the partition against
  the real file: parsing cannot catch a heading nested one level too deep, and the symptom would be one giant
  pane discovered by opening the gear.

  It stays a modal rather than becoming a seventh tab. Settings is where you go from wherever you are and come
  straight back, and a modal returns you there for free.

  The line this nav has to hold, written down because the nav is what will be used to place the next setting:
  **everything in this dialog is a setting for the MACHINE.** A card's theme, its bell and its notes are
  settings for one piece of work and they live on the card.

- **Notes on a card, and sending one when you are ready.** A note is for you. Sending it is a second, separate
  act, and the difference is the point: the box underneath fires as soon as there is anything in it, reaching
  the session on its next tool call. This one goes nowhere until you say.

  What that buys is ordering. Three things thought of during a long turn, sent as one instruction at the end,
  rather than three interruptions in the middle. Claude Code takes input while it is thinking, so the send is
  less urgent than it once was, and the ordering is the half that still matters.

  Saved as you stop typing rather than on a button, because a scratch pad you can lose by closing a dialog is a
  scratch pad you stop using. Held by the daemon rather than the browser, like a card's theme and its bell: it
  is about the work, not about this screen. Cleared only once the message is safely somewhere else, so a send
  that fails leaves what you wrote where you can still see it.

- **A message says who it is from.** `message` gains a `from_peer` column and the banner reads it. Empty means
  the operator, which is what every message written before this was.

  Landed now rather than with the peer bus that needs it, because both touch this one table and shipping two
  migrations against it two patches apart is how a column ends up meaning slightly different things depending on
  when the row was written.

  The wording is load-bearing. A model that reads another session's request as an instruction from you acts on
  it with an authority that session does not have, and the only thing between those two readings is the
  envelope. A message from a peer names the session and says it was not the human. A mixed batch says so and
  labels each one, rather than picking an author and being wrong about half of it. `QueueFromPeer` is a separate
  function from `QueueMessage` and refuses an empty sender, because one function with an optional sender is one
  defaulted argument away from a peer message that claims to be from you.

- **The daemon says when it opened a different database than last time.** This cost twenty minutes of thinking
  every card had been lost.

  Which database you get depends on the shell you started from: `HubDir` returns `$WORKTREE_ROOT\hub` when that
  variable is set and `~\.atrium` when it is not. Start the daemon from a terminal that has it and from one that
  does not, and you get two different boards, both real, both populated, neither obviously wrong.

  The existing guard only covered a database that had to be CREATED, which is the easier half: an empty board is
  obviously empty. Opening a different EXISTING one was silent, and that is the worse case, because a populated
  stranger looks exactly like your own board after something ate most of it.

  `daemon.json` already recorded which database the last daemon opened, so the fix is to read it before
  overwriting it. The warning names both paths and how many cards are in each, which is what makes it act on you
  rather than get read past. The count comes from a **read-only** open, hand-rolled rather than going through
  `store.Open`, because opening the other database properly would run every migration against it, and writing to
  a database the operator did not ask this daemon to touch is the wrong way to tell them they have two.

  Also `--location-file`, which is what makes it possible to run a second daemon to try something without
  stealing the first one's hooks. Found by doing exactly that while testing the warning.

- **Getting a file back OUT of an agent.** Bytes going in already worked. `GET /v1/tasks/{id}/files?path=`
  served one back and nothing called it, because nothing could say what was in there.

  A files panel on the card, folded until you want it. It lists one directory at a time under that card's
  worktree, files and directories both, and every entry has a download link.

  **It works over an overlay for free, which is the reason it is worth having.** The download is the board's own
  HTTP, so whatever already carries the board carries this: loopback, a zrok share, a ziti service. Nothing new
  is published and there is no second transport to configure.

  Deliberately not `browse.go`, which lists the daemon's whole filesystem for the launch picker, takes no card
  and has no containment. This takes a card and resolves everything through `internal/safepath` against that
  card's worktree. **Every path the board uses comes from the server, including where "up" goes**, so the board
  never does path arithmetic, which is where a traversal would come from if one were going to. Walking up stops
  at the card.

  Bounded at 500 entries with the listing saying when it truncated, because a working directory with a
  `node_modules` in it has more entries than anybody is going to read and the answer is a shorter list rather
  than a slow board. Everything outside the card answers `403` whether or not it exists, so this is not an
  oracle for what is on the machine. An unqualified listing starts at `.atrium/incoming` when that exists,
  since the thing you most often want back is the thing something just put there.

- **Four things a review found, fixed.** A code review of everything above, run before any of it was committed.
  All four concerns were real. `.mercurius/s_rBMcLOgwpVAn/round-01/` has the findings and the triage.

  **A stored `javascript:` url on a card.** The worst of the four and the one that arrived with the feature that
  created it. Escaping protects the attribute and does nothing about the scheme, so `javascript:alert(1)` on a
  card's origin link survived escaping intact and would have run in the board's own context, which holds the
  settings, the grouping expression and every card. It matters here because of where the data comes from: a
  source is a script reading GitHub or Zendesk and whatever it prints becomes a link. Now allowed only `http`
  and `https`, checked where a link enters AND where it is drawn, and anything else renders as text rather than
  taking the identifier down with it. An allow list, because the set of schemes a browser will act on is not a
  set this code gets to enumerate.

  **The upload directory was created before it was checked.** The computed destination is the whole security
  argument for shipping upload before any write endpoint, and calling `MkdirAll` before `Contained` undercut it:
  a `.atrium` that was already a symlink elsewhere would have had a directory made outside the card before
  anything refused anything. Resolved first now, and again afterwards, because `MkdirAll` resolves links it did
  not create.

  **The source output limit was checked after the output was in memory.** `cmd.Output()` buffers everything and
  hands it over, so a source dumping a repository was refused having first been read in full, which is exactly
  what the bound exists to prevent. Bounded while reading now.

  **A partial import was recorded as a successful run.** One item failing to land was logged and skipped, and
  the row then said the run succeeded, so nothing ever reconciled. Any failure now fails the run. Items already
  offered are left alone rather than rolled back, because offering is keyed on the pair and the next tick sees
  them as known.

- **Paste a screenshot into a session.** The half of file transfer with no workaround at all: the clipboard is
  on the machine with the browser and the session is on the machine with the daemon, so over an overlay there
  was simply no way to get an image to an agent. Paste, drop and picker are one pipeline, because whatever
  gesture produced the bytes, the bytes go to the same place and what comes back is a path the runner opens with
  its own file tool. No image-specific branch and no base64 inlining, which is why one pipeline covers files of
  any kind.

  **The path is spliced into the stream and enter is not pressed.** Saying something to a session appends a
  newline because a message is a complete instruction. A pasted path is a fragment of an instruction somebody is
  still writing, and submitting it for them is the difference between a helpful paste and a runner that starts
  working on half a sentence.

  Upload takes **no destination**. The caller names a card and nothing else, and atrium computes where the bytes
  land, under `.atrium/incoming` in the card's own directory. That is the whole security argument for the first
  version: a caller-supplied destination needs containment to be right, and a computed one is correct even if
  containment is wrong, because there is no caller input in the path at all. Download does take a path, and is
  the first thing in atrium that actually needed the containment primitive.

  **That primitive did not exist**, which is most of what the design bought. `internal/safepath` resolves
  symlinks on both sides, compares on a separator boundary so `worktree-evil` is not inside `worktree`, folds
  case on Windows and nowhere else, and resolves a path that does not exist yet as far as it does exist. Each of
  those is a hole if it is missed and each has a test that is the way it gets missed.

  Two things the tests turned up. The right-to-left override is not a control character, so stripping everything
  below `0x20` left the oldest trick there is for making an executable look like an image; the whole `Cf`
  category goes now. And every path OUTSIDE the card answers `403` whether or not it exists, so the endpoint
  cannot be used to find out what is on the machine. A path inside the card that is missing answers `404`, which
  leaks nothing: anyone who can ask can already see that directory.

- **Everything that has ever run here, as its own tab.** `ListArchived` had existed for a while and nothing
  showed it. Cards were being archived off the board and going nowhere anybody could look.

  Every card ever created, newest first, on the board or not, searchable across the title, the reason, the
  directory, the tags, the recap and the external identifier, because a person looking for "that thing about
  DNS" does not know which field they are remembering it from.

  The cut that makes it worth having is **written up** against **never written up**, which is only a real
  distinction now that an agent can say it finished. Before that no card had a recap and the filter would have
  been an empty column. A card in the second group is either still worth writing up or was never worth starting,
  and those are worth telling apart.

  Paged from the start rather than after the first machine that has been running for a year finds out the hard
  way, and fetched when you open the tab rather than on every poll: it is a question you go looking for, and a
  query against a table that only grows has no business running every few seconds while you read something else.

- **Actions on a card, written by you.** A card could be terminated, shelved, attached to and messaged, and all
  of those are things ATRIUM does. None of them was the thing a person does repeatedly, which is send the same
  instruction to whichever agent is in front of them.

  A named prompt, offered on every card, optionally limited to a tag or a runner because "run the tests" means
  something different in a Go repo and a docs repo. Delivery is the existing message queue: typed into the
  terminal when atrium owns one, queued for the next hook when it does not, and the toast says which, because
  they are different promises.

  `and exit` is why this is not a saved snippet. It sends the prompt and then the harness's own exit keys, which
  is the "write it up and go away" case. It is best effort and says so: there is a pause between the two so the
  keys do not land before the prompt has been submitted, and a session atrium does not own gets told to wrap up
  with a note explaining that nothing can make it quit.

  Three are seeded, because a feature whose value is "you write your own" starts as an empty list nobody fills
  in. Deleting all three does not bring them back. One of them is **write it up and finish**, and it is the
  answer to the gap the previous entry leaves open: nothing tells a session that `atrium finish` exists, and this
  needs no cooperation from the session at all.

  This is the first operator-authored content atrium stores and hands back, which `docs/backlog.md` flags as a
  cost under the grouping-expression entry. The answer is that the two are different in kind: a grouping
  expression is CODE evaluated in a browser with full page scope, and an action is TEXT delivered to a runner.

- **An agent can say it finished.** The largest hole in what atrium does, and it was a hole in the shape of a
  missing verb. Everything an agent reported landed in `needs-input`, so the board could not tell "finished, go
  and look at the result" from "stuck, answer me", and only a human moving a card by hand ever produced `done`.

  `atrium finish [recap]` moves the card and records what the session says it did. `--hand-back` puts it in
  `ready` instead, which is a different claim: handing the work over without saying it is over.

  **A command rather than a tool, and that is the decision worth reading.** The v2 design named
  `submit(kind="task-complete")` for this. A command is better, because it is the one channel every runner
  already has: an agent that can run `ls` can run this, with no MCP server, no tool description and no
  cooperation from the harness. It works for codex and for a bare shell, not only for the runner that happens to
  have a tool surface.

  A recap is two or three sentences and it is bounded at two thousand characters, because a column with no limit
  is how a card ends up holding a diff. Too long is truncated rather than refused: a session that wrote too much
  still wrote something worth keeping, and failing the call would make an agent retry with something longer.

  The card carries whether there is one, which is the useful cut. A finished card with a recap has been written
  up; one without is either still worth writing up or was never worth starting. A dead card is not marked as
  missing one, because a session that was killed did not decline to write itself up.

  What is NOT built: anything that tells an agent to call it. It exists, it is documented, and no session knows.
  The best answer is a card action that sends "write yourself up and finish" as a prompt, because that needs no
  cooperation from the session at all.

- **Pruning on a timer, and the two timers put next to each other.** Sweeping finished columns was a button, so
  cards accumulated between presses, and archived rows accumulated forever because nothing had ever removed one.

  There are now two settings in the gear under `housekeeping`, deliberately side by side, because they are easy
  to confuse and the difference is the whole point. **Sweeping archives:** a dead card leaves the board and every
  word of its audit log stays, which is what answers "what have I had running this week". **Pruning deletes:** the
  card and its history go, and there is no other copy.

  So sweeping is on by default and pruning is off unless you turn it on, turning it on is confirmed with what it
  destroys spelled out, and it has an hour floor so a mistyped value cannot become "delete everything that
  finished". It takes `done` and `dead`. Shelved is refused by the store whatever it is asked, because shelving
  says the work is coming back. The inbox is left alone too: an offered item nobody started is still work
  somebody found, and deleting it on a schedule would make the inbox quietly lossy.

- **Auto mode for the next hour.** Both switches take a deadline, and the button asks how long rather than
  whether. Until you turn it off is still there and still means it: a switch that could only be temporary would
  just be a shorter lie about the same thing.

  **Nothing enforces the deadline, and that is the design.** There is no timer and no goroutine watching the
  clock. The permission chain is the only moment auto mode means anything, so it is the only moment worth asking,
  and a check made there cannot be missed by a restart. A timer that has to fire is a timer that does not fire
  across a restart, and auto mode surviving one it should not have survived is the failure worth designing
  against.

  A card whose deadline has passed gets its flag cleared on the way through, so the badge stops claiming
  something that stopped being true, but that write is bookkeeping following the answer rather than the thing
  that makes the answer correct. Turning it off always clears the deadline, because "off until Tuesday" is not a
  thing anybody means and a leftover deadline would turn itself back on the next time somebody flipped the
  switch.

  How long is left is on the switch, not behind it. The whole reason a deadline exists is that "approving
  everything" is easy to leave on, and a reminder you have to hover over is not a reminder.

- **Four more hooks, and a subagent count that was being counted twice.** `docs/hook-coverage-spike.md` listed
  what was unwired. Three of the four are now wired and the fourth is deliberately left alone.

  `PostToolUseFailure` reports the same thing as `PostToolUse`, because a failed tool and a finished tool mean
  the same thing to a badge that only says what is running now. It gets its own argument and not its own state:
  a distinct `tool-failed` would put the tool's problems on a board that answers "what needs me", and a failing
  tool does not need you until the model gives up and stops.

  `PreCompact` records the moment a session forgot something. A timeline event, not a status, because compaction
  is a moment and there is nothing for a card to sit in. It answers a question that comes up on its own: why did
  this agent stop knowing something it clearly knew an hour ago.

  `Notification` is wired filtered, and it filters itself rather than relying on a matcher expression in
  somebody's settings file. It fires for around a dozen kinds of thing and most of them are not a card wanting a
  human. An unrecognized kind stays silent on purpose, so a new notification type in a future release does not
  turn into noise on upgrade. `permission_prompt` is excluded even though it plainly wants a human, because
  atrium's own gate is what put it on screen.

  `SessionEnd` now reads `reason`. `clear` and `resume` are followed immediately by another start in the same
  place, so the card no longer dies and comes back a second later. Checked in the hook AND in the daemon, since
  the daemon cannot assume which version of the hook binary is installed.

  **The bug this turned up:** a `Task` tool call was counted as a subagent starting, which was right when no
  hook said so. `SubagentStart` is wired now, so a gated session counted every subagent twice. It also leaked,
  because the count went up when the call was REQUESTED, including when it was then refused, and only
  `SubagentStop` brings one down. A denied `Task` left a subagent on the card that never existed and would never
  end. The inference is gone.

  Not wired, and this is the interesting refusal: **the permission gate stays on `PreToolUse`.** The spike
  recommends moving it to `PermissionRequest` and then says, in as many words, not to write that hook from the
  document, because two readings of the reference gave two different output shapes and the right one has to be
  found by dumping a live hook's stdin. That is the one item on the list that cannot be settled by reading.

- **Running the tests no longer deletes the running daemon's address.** Found by noticing the file was gone.

  `Run` writes `daemon.json` on start and deletes it on stop, and the delete is guarded on the pid so a daemon
  does not remove a file another one owns. The write was not guarded, so every test that started a daemon
  overwrote the file with its own pid and its own random port, and then the guarded delete matched and removed
  it. Net effect of one `go test ./...`: the machine's real daemon becomes unfindable.

  Nothing broke, and that is the interesting part. A caller that cannot find the file falls back to
  `localhost:7777`, and the daemon this was found on was on the default port. On any other port a test run would
  have quietly unhooked every live session on the machine, with no error anywhere, because a hook that cannot
  reach atrium fails open by design.

  `Options.LocationFile` is the fix: tests point it at a temp directory and a real daemon leaves it empty and
  gets the machine's one true place. Writing the file over one that names a DIFFERENT, still-running process now
  logs a warning as well, because two daemons on one machine is a mistake whose only symptom is every hook in
  every session arriving at the wrong one.

- **A source is a command on a timer.** The inbox fills itself now. A `source` row is an id, a command, its
  arguments, a directory and an interval, and atrium runs it and reads its stdout as intake items. Shaped like
  the `harness` table on purpose and for the same reason: a harness row says how to start a runner without atrium
  knowing what claude is, and a source row says how to find work without atrium knowing what GitHub is.

  **There is nowhere in the table to put a credential**, which is the design rather than an omission. `gh` has a
  token in the keyring it already uses. Atrium has an argv.

  The rules it runs under, each of which is a refusal rather than a truncation: one megabyte of output, two
  minutes, and three consecutive failures switches it off with the reason still on the row. A run either lands
  entirely or not at all, because one unkeyable item in a batch of forty is a source to fix rather than a partial
  import to reconcile, and the same batch arrives again next tick once it is fixed. An empty stdout is a normal
  answer and not a failure: a queue with nothing in it is the state you want. Thirty seconds is the floor on an
  interval, because a source is a child process and one every second is a fork bomb with a settings screen.

  `run it now` sits next to save, because a source is a script somebody just wrote and the question they have is
  whether it works. Waiting fifteen minutes to find out that a path was wrong is how a feature goes unused. It
  runs a disabled source too, since pressing the button is the operator saying to run it.

  `scripts/sources/` has two working examples and a README. They are deliberately not symmetrical: the GitHub one
  suggests a directory, derives a branch and writes an imperative prompt, and the Zendesk one does none of those
  and explains at length why. A support case names no repo, so there is nothing to suggest, and it carries
  somebody else's data, so it deliberately does not copy the subject line onto a card.

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
  every failure path prints a plain continue and exits 0, `stop_hook_active` is honored so it cannot block
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
  than transcribed, so the colors are the ones already in daily use and a session looks the same in the board
  as it does in a terminal window. The repo-to-theme map came across too, so a session picks up the color
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
  seconds, set under the gear. Choosing "never, until answered" restores the old behavior on purpose. The
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
  a retry after a daemon crash is recognized as the same question instead of being asked again.

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
- **`rank`** orders cards within a column, with midpoint insertion so reordering never renumbers neighbors.
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
