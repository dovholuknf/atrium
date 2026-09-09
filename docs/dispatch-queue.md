# Queued work, not yet dispatched

Work that has been decided on and deliberately NOT given to an agent yet. `docs/backlog.md` is what is left to
do at all. This is the narrower question of what to hand out next, and to how many.

**Grouped on purpose.** Sixteen agents running at once made the machine sluggish, and most of that cost bought
nothing: several of the items were one file apart and would have been one session. An entry here is an
AGENT-SIZED unit of work, which means one surface, one set of tests, and one thing to look at when it is done.
Splitting by backlog number is how you get four sessions editing `index.html`.

**Nothing here starts without being asked for.** The rule that produced this file: do not launch new agents
while the operator is using the machine.

---

## NEXT. Send the work to another machine

**This is now the top of the list, and the reason is not architectural.** Thirteen runners saturated one
desktop for half an hour. That is fine overnight and not fine at 2pm, and there are four other machines
sitting idle: a laptop, an M1 mini, cdaws and cdzrok. The operator wants to hand a backlog item to one of them
and keep orchestrating from here.

Rooms already carry cards INWARD. What is missing is everything that makes a room somewhere you can put work.

### The verb that does not exist

A room reports what is on it. Nothing says "start this there". `POST /v1/launch` starts a runner on the machine
it is sent to, and that is the whole of it.

The shape that fits what is already here: the hub does not push. **A leaf dials out and asks whether there is
work for it**, which is the same posture `atrium room` already takes for its heartbeat and the same reason it
works from behind NAT. The check-in already happens every twenty seconds and already gets a reply. A launch
request can ride the reply it is already receiving, which means no new connection, no inbound reachability, and
nothing to configure on the machine that is behind the awkward network.

Decide what a queued launch is: a row on the hub with a room name on it, or an offer the leaf claims. Two
rooms must not both take one item.

### The part atrium must not solve, and must not ignore

**A remote machine does not have the worktree.** Every launch here names a directory that already exists,
because `gwt` made it. cdaws has no `D:/worktrees/github/dovholuknf/atrium/check-contrast` and never will.

Atrium does not learn git. That rule holds and is worth more than this feature. So the answer is one of:

- The launch carries a **prepare command** the room runs first, and whoever queues the work supplies it. The
  harness table already has a `Prepare` field that runs before a runner starts, and `docs/architecture-v2.md`
  explains why it is there.
- The room is configured with a **workspace root** and refuses launches that do not resolve inside it.
- The remote card is launched into a directory that room already has, and preparing it was somebody else's job.

Pick one, say why, and say what happens when the directory is not there: a card that fails immediately with a
readable reason is a good outcome, and a card that silently starts in the wrong place is not.

### What comes with it, in order

1. **Permission requests from a room.** Already dispatched as backlog 1. A remote agent WILL hit the gate, and
   until this lands a blocked card on cdaws is invisible unless somebody opens cdaws's own board. The leaf
   holds the channel, so the hub forwards the decision and the leaf unblocks its own request.
2. **Attach by redirect.** A pty cannot leave the machine that made it. The row already carries a board
   address, so this is a link that knows about `#term=`.
3. **A room that survives a reboot.** Today it is `atrium room` under `nohup`. It should be the same service
   the packaging work installs, or the machine you offloaded to is gone after an update.
4. **Getting the work back.** Nothing here says how a branch on cdaws returns. Probably a push, probably not
   atrium's problem, and it has to be answered before this is usable rather than discovered afterwards.

### Why the ziti half matters more than it did

Two of these machines are on the internet and two are not, and neither end can reach the other without
something. `atrium room --service --identity` already dials the hub over an OpenZiti service and neither end
has to be reachable. That path exists and has been proven between two Linux machines. What it has never done
is carry work outward.

---

## AFTER THAT. The board throws away where you were

**The one to hand out the moment the running agents stabilise.** Operator's words: "super fucking annoying...
it invalidates my position in the window, so if I had scrolled down it rerenders the full fucking dom and
forces my current view back to the top... it's horrible".

Every update repaints wholesale. `setHTML` replaces `innerHTML` for the whole list, so every node is destroyed
and rebuilt, and a browser has nowhere to put the scroll position of elements that no longer exist. It goes to
the top. This fires on every SSE event, which on a board with sixteen live agents is constantly, so scrolling
down is something you cannot finish doing.

Scroll is the loudest symptom and not the only one. The same swap drops text selection mid-drag, moves focus,
closes anything anchored to a node that has gone, and restarts CSS transitions, which is why the board can look
like it is flickering under load.

Two shapes of answer, and the second is the real one:

- **Preserve around the swap.** Record `scrollTop` before, restore after. Small, and a lie: it fights the
  browser every frame, still destroys selection and focus, and goes wrong when the content above the viewport
  changes height.
- **Stop repainting what did not change.** Reconcile rows by card id: update the ones whose data moved, add
  and remove the rest, and leave every untouched node alone. Then scroll, selection and focus survive because
  nothing was destroyed.

The second is more work and is the fix. The board already has the identity needed for it: every row carries
`data-id`, which is the whole precondition for reconciling by key.

**Two tiers, and the second one is what makes it hold.** The operator's framing: only replace a card if the
card changed, so the content updates and the container does not.

1. **The container is never rebuilt.** Rows are added, removed and reordered by id. Anything untouched keeps
   its DOM, so scroll position, text selection and focus survive because nothing that held them was destroyed.
2. **A row is not rebuilt either, unless its STRUCTURE changed.** The fields that tick constantly are the age,
   the activity chip and the status class, and those are written into the nodes that are already there. Only a
   real change rebuilds a row: the title, the tags, the `why`, the card arriving or leaving.

Skipping the second tier swaps one bug for a quieter one. The age updates every second, so the card being
destroyed is the card being read, and the selection dies anyway while the scroll position now survives, which
is a fix that looks complete and is not.

Watch for the thing that makes this deceptive: it will look fixed on a quiet board. Test it with the list
scrolled down, mid-selection, while cards are actually changing.

---

## ALSO NEXT. A switcher, on a keystroke

**Asked for more than once and never landed.** With sixteen sessions the terminal list is a list you have to
read, and the thing wanted is the one every chat application has: a key, a fuzzy filter, the session, gone.

**The keystroke is the whole design risk and it has to be settled first.** `ctrl-k` is the one everybody
means and browsers have been taking it: it focuses the address bar in Chrome and Chromium, and it is search
in others. Preventing default on a browser accelerator works in some and is silently ignored in others, which
is the worst outcome because it works on the machine it was written on. `ctrl-shift-k` is the operator's
suggestion and is not free either: it is the Web Console in Firefox.

So: **it is a setting with a default, not a constant.** Pick a default that survives Chrome and Brave, name
in the settings dialog which browsers take it away, and let it be rebound. Anything else ships a feature that
works for one person.

What it has to do:

- Open over whatever is on screen, filter as you type against the card title, the directory and the tags,
  Enter to go, Escape to close. Nothing that needs a mouse to dismiss.
- **Work in a popped-out window, and that is the interesting half.** A solo window shows one card. Switching
  there means changing which card that window IS, which is the `#term=` hash, a re-attach, and a re-claim on
  the solo bus so the board stops believing the old card is popped out. Read `bootTerminalOnly` and the
  `atrium-solo` channel in `internal/api/web/index.html` before designing it: claims are heartbeats with a
  fifteen second life, so a switch that forgets to release leaves a card claimed by a window that has moved on.
- Remember the last few, most recent first, so the common case is the key and Enter.

Not a new pane and not a new screen. The terminal list already answers "what is there", and this answers "go
there now".

## A. Publishing atrium

**Blocked on accounts, not on code.** Backlog 4. Six commands written out at the end of `docs/packaging.md`,
Scoop first because it exercises the release shape end to end. An agent cannot do this: it needs credentials
that belong to a person.

What an agent COULD do first, and would be worth one session: the release script and its dry run, so the
publish itself is one command with nothing left to work out at the time.

## B. Multi-tenant, and what it costs

**A decision before it is code.** Backlog 3. Two of the three objections moved: there is authentication now,
and rooms are federation's cheaper half. The supervisor is what has not moved, because the daemon owns a pty
per runner in the operator's own logon session and a cluster has no logon session.

Not agent work until somebody decides fork or flag. Worth an hour of reading and a written recommendation,
which IS agent work, and is one session.

## C. Postgres

Backlog 10. The schema is written for it and has never run there. Nothing needs it until B does. One session,
and the honest output is "here is what broke", not a migration.

## D. Many boards, one machine

`atrium preview` shipped: a second daemon on a copy of the cards, with its own port and its own address file,
started PASSIVE so it serves the board and acts on nothing. It has no backlog entry and no section in
`docs/overlays.md` or `docs/supervision-design.md`, which means the next person to want two boards will invent
it again.

One session: write it up, and answer the question that produced it. Two atriums on one database is not blocked
by sqlite, which allows it. It is blocked by both of them ACTING: two daemons both start every fixture, both
re-bind the same zrok name, and both write the address file that every hook on the machine reads. Postgres
changes none of that. `Options.Passive` is one writer and any number of readers, and whether that generalises
is the thing to answer.

## E. Watching the fleet

The gap is not in atrium, it is in how it was being used: sixteen agents ran for an hour and the operator found
out which ones had finished by asking. The board already knows. `atrium finish` files a card, `atrium ask` puts
the question on it, and nothing was reading either.

Smallest useful thing, and it may not need code at all: a single command that answers "which of these want me",
and what a dispatcher should be looking at between them. Decide whether that is `atrium peers` grown up, a
saved filter on the board, or a script.

## G. Context and rate limits on a card

**The board cannot see either, and there is exactly one place that can.** Claude Code hands its statusline
script the richest per-session JSON it exposes: context used and window size, the five hour and weekly limits,
the transcript path. Atrium's hooks never see any of it. So the statusline is the only source, and a card
saying "92% context" is the single most useful thing to know when deciding which of sixteen agents to
interrupt, because that one is about to compact.

Proposed by the dotfiles session, which also named the objection correctly: **the statusline renders many
times per second**, and it was already rewritten once to cut forks because it cost about a second per render.
A curl per render brings that straight back, floods atrium, and a hung curl stalls the bar.

Their three mitigations are right and are the price of admission: throttle with a stamp file to roughly one
post every ten to fifteen seconds, background the curl with a hard `--max-time 1` so the bar never waits, and
send ONLY what hooks cannot already report, keyed by session id.

**Two constraints that come from this side and are not in their proposal:**

- **It is live state, so it does not go in the database.** `docs/activity-design.md` says what a runner is
  doing now is never stored, because it becomes a lie the moment the daemon restarts. Context percentage is
  the same shape. It belongs in the in-memory activity map beside the rest of it.
- **The endpoint follows the hook posture, and `/activity` is the exact precedent.** Answer before doing any
  work, swallow unknown sessions, one second timeout, every failure ignored, no retry. A session must never
  stall because atrium was not listening.

Needs an endpoint on this side, so it cannot start there. Same account, loopback, no cross-account problem.
One session, and it should build the endpoint and the statusline change together or neither is testable.

## F. Small board nits

Kept together deliberately. Each is minutes of work and they all live in `internal/api/web/index.html`, so
they are one session and not five. Adding a fifth one here is cheaper than starting a fifth agent.
- **The whole group heading toggles the accordion, and nothing says so.** Operator: "it's still REALLY not
- **DONE.** **Scale the text in a terminal without scaling the board around it.** Operator: "i need to be able to scale
  the font IN the terminal separate from the browser so that it doesn't scale the ui elements with the text.
  some sort of setting cog menu per terminal and keep it short lived so it's tied TO the terminal."

  Browser zoom is the only tool today and it scales everything: the tabs, the card strip, the bar, the chrome.
  What is wanted is xterm's own `fontSize`, per pane, from the cog that is already on the terminal bar.

  **SHORT LIVED, and that is the unusual part.** Every other per-terminal choice on this board is remembered:
  the theme is on the card, the popped-out window size is in `localStorage`, the skin is on the daemon. This
  one is deliberately none of those. It dies with the pane, because it is an answer to "I cannot read this
  right now" rather than a preference. Say that in the comment or the next person will helpfully persist it.

  **The thing that makes this more than a font setting: changing the size changes the SIZE OF THE PTY.** xterm
  measures in cells, so a font change is a `fit()`, and `fit()` hands new rows and columns to `resize()`. Three
  consequences, all already load bearing elsewhere:

  1. **It resizes the runner for every other viewer.** `setViewport` agrees on the SMALLEST attached viewport,
     so somebody bumping their font up in one window shrinks the columns the agent is drawing into everywhere.
     With the second-view refusal in place that is one viewer most of the time, and not over a share.
  2. ~~**It can discard the carried scrollback.**~~ **No longer true, and the correction is worth keeping.**
     This said a buffer was replayed only when the width matched, so a font change right after attaching would
     throw away the history that just came back. `SnapshotAt` is gone. `ringBuffer.Replay` hands over
     everything it holds with the widths alongside it, and `attach.go` says which above the replay, because
     deciding on the reader's behalf that imperfect output is worth less than no output is wrong: misplaced
     text can be read, scrolled past and searched, and an empty pane cannot. A font change costs nothing here.
  3. **It must not move the viewport.** `check-terminal.js` rules 9 through 12 exist because a `fit()` that
     changes the row count clamps the view to the bottom, which is the scroll bug that took five attempts to
     fix. A font change is a deliberate resize and has to restore the position the same way `onTermResize`
     does.

  So the setting is two lines and the correctness is entirely in what a resize already means here. Read those
  rules before touching it, and add one for this path.

- **A shell atrium opens should prefer `pwsh`, then `powershell`, then `cmd`.** Operator: "shell should use
  pwsh if available then powershell then cmd in that order".

  Two places this bites, and they are not the same code:

  1. **The card's own shell**, opened from the terminal bar beside the runner. It exists for the moment the
     agent has wedged and there is nowhere to type `git status`, so it should be the shell somebody actually
     types in.
  2. **What a harness runs when nothing names a shell.** A harness that spawns a command has to pick one, and
     picking `cmd` gives a person a prompt they stopped using years ago.

  The order matters more than it looks. `pwsh` is PowerShell 7 and is a separate install; `powershell` is 5.1
  and is on every Windows; `cmd` is the floor. Falling straight to `cmd` when `pwsh` is missing skips the one
  that is always there.

  RESOLVED ONCE, NOT PER SPAWN. `internal/claudeconf/whichexe.go` already has the pattern for this and the
  header explains why it is not `os.Executable()`. Resolve at startup, log which one was chosen, and let a
  harness override it, since a machine where the answer is wrong should be able to say so without a rebuild.

  Seen on SG3 while setting up a room: the ssh default shell was pointed at a `pwsh.exe` that is not installed
  there, and the session came up on 5.1. A chooser that checks rather than assumes would have said so.

  obvious that clicking anywhere on that whole row collapses and expands the view. that should be a hover or
  something... i keep clicking all that white space thinking it's safe but it collapses or expands the
  accordion".

  A `<summary>` is clickable across its full width by default, and the heading is mostly empty space, so the
  safest looking part of the row is the part with the largest hit area. Making the triangle a proper target
  (fixed in round 9) helped the aim and did nothing about the surprise: the glyph now looks like the control,
  which makes the rest of the row look like it is not one.

  Two ways, and they are opposite:

  - **Say the whole row is the control.** A hover treatment across the entire heading, the way a list row that
    opens something already gets one. Cheap, keeps the big hit area, and makes an accidental press an informed
    press.
  - **Make only the triangle the control.** Stop the summary's default toggle and put the handler on the
    glyph. Then the white space IS safe, which is what the operator expected. Costs the easy target that was
    just added, on a row people also want to click quickly.

  The first is more likely right, since the big target is worth keeping and the complaint is about surprise
  rather than about wanting the space inert. But note the collision: the heading is ALSO the right-click
  target for recolouring a group, so whatever hover is added has to leave room to say that too.


- **A toast about a popped-out window appears in the wrong window.** Attaching to a card that is already
  popped out raises that window and says "it is in its own window: raised it for you". The toast is drawn in
  the window you clicked in, which is the one that now does not have the terminal. It belongs in the window
  that was raised, which is where you are about to be looking. `raiseToasts` already moves the toast host
  between elements, so the machinery for putting a toast somewhere specific exists.

- **The restart banner does not survive the restart.** `restart atrium` puts up "atrium is restarting", and
  then the daemon comes back, the page reloads on the new build id, and the banner goes with it. So the one
  moment it exists for, the gap between the old daemon dying and a runner actually being back, is the moment
  it is not on screen. It should hold until the terminal is attached again, not until the page reloads.

  Worse in a popped-out window, which is a terminal and nothing else: there is no board around it to read as
  "something is happening", so a reloaded page with a dead terminal in it looks like the terminal broke.
  It should be MODAL there, over the pane, until the socket is back.

  The reload is the thing to work around rather than remove: reloading on a new build id is how a stale board
  stops being served. So the state has to be written down somewhere a reload does not clear, and taken down by
  the attach rather than by the load.

- **The zrok account block does not say why anybody should care.** Operator, looking at it for the first
  time: "seems fine. i'm not sure what i'm looking at". Three numbers in a row, environments, shares open,
  reserved names, with a paragraph under them explaining that they are counts rather than fractions.

  Every fact in it is right and none of it answers the question somebody has in front of it, which is whether
  starting a share is about to fail. The counts are the evidence for that answer, drawn instead of the answer.
  It reads as a readout because it is one.

  What it is missing is a verdict on top: one line saying whether this account looks fine, and the counts
  underneath as the working. The limited case already does exactly that, and it is the only branch of this
  block anybody understands on sight.

- **A dialog scrollbar inside a dialog scrollbar.** `.dlg-body` is `max-height: 78vh; overflow-y: auto`, and
  the fields inside it include textareas that scroll on their own. On a short window that is two vertical
  scrollbars a few pixels apart, and the outer one is the one nobody expects. Operator: "it's got a strange
  slider bar in a slider bar (vertical)".

  The recogniser editor is where it shows worst because it is the longest dialog in the board: id, label,
  pattern, rank, cwd, title, prompt, tags, branch, window, theme, kind, three fetch fields, then the switch.

- **A recogniser cannot be turned on without scrolling past everything.** Operator, having filled the whole
  form in: "i don't see any way to 'enable' or 'on' the rule". It is the last field in the dialog, under the
  three fetch fields, and it is a checkbox labelled `ask this row when a url is pasted`.

  Two problems and they compound. It is BELOW THE FOLD, after the fields somebody writing their first row will
  leave empty, so the one control that decides whether the row does anything is the one hardest to reach. And
  it does not read as a switch: every other on/off on this board says on or off, and a row that says "ask this
  row when a url is pasted" is a sentence, not a state. The word it is missing is the one somebody looks for,
  which is `enabled`.

  It belongs at the top, beside the id, where the answer to "is this row live" is visible without reading the
  form.

- **The recogniser row is missing the two controls it is read for.** Operator: "the on/off toggle should be
  available in the recognizer table and ... i like the try it function that should also be a button from the
  table".

  Both exist, and both are inside the editor, which is a fourteen-field form. So the two things somebody does
  repeatedly, turning a row off and asking a row what a url resolves to, each cost opening a dialog, scrolling
  it, and closing it again. `tryRecogniser` in particular is how a row gets debugged, and debugging a row is
  the whole reason the list is ordered.

  On the row: an on/off, and a `try` that opens the same box the editor has. The editor keeps both; it stops
  being the only way to reach them.

- **`ask this row when a url is pasted` is a bad name for a switch.** Operator: "the name is fucking
  horrible". It is a sentence describing a behaviour where a state belongs, and it is the reason the control
  was not found at all. Inside the editor it should read as a toggle button that says what the row IS, on or
  off, at the bottom where a decision belongs, not a checkbox with a clause after it.

- **TESTS: check what the recogniser surface actually covers before adding any.** Deliberately NOT written
  yet, at the operator's instruction. Today: `internal/store/recognisers_test.go` 15 tests,
  `internal/daemon/recognise_test.go` 9, `internal/api/recognisers_test.go` 5. That is the matching, the
  templates and the endpoint.

  What no test touches is the part being changed above, which is the board: nothing asserts that a row can be
  turned on from the list, that the try box answers from the list, or that a saved row comes back enabled. The
  board has no test harness of its own beyond the `scripts/check-*.js` parsers, so the honest options are a
  check script over `index.html` for the controls existing, and API-level tests for the state actually
  changing. Decide which before writing either.

---

- **A 27 line paste arrived as one fragment.** The operator copied a `tasklist | grep atrium` block out of
  Notepad++ and pasted it into a terminal on loopback. What reached the session was `23,276 K` -- the tail of
  the LAST line -- and nothing else. Twenty six lines vanished with nothing said. The source used CRLF
  throughout, which he confirmed.

  **`sendPasteText` normalises before it decides whether to bracket, and that is the defect:**

      let body = String(text).replace(/\r\n/g, "\r").replace(/\n/g, "\r");
      const bracketed = term && term.modes && term.modes.bracketedPasteMode;
      if (bracketed) body = "\x1b[200~" + body + "\x1b[201~";

  Turning every line ending into a bare `\r` is the behaviour the UNBRACKETED case wants, because there a
  newline should read as Enter. It is applied unconditionally, so the bracketed path carries twenty seven
  carriage returns where it should carry newlines. A bare `\r` returns the cursor to column zero without
  advancing a line, so the lines overwrite each other and what survives is a tail.

  The point of bracketed paste is that the RECEIVER decides what a newline in the block means. Rewriting the
  newlines first takes that decision away from it and hands it the one thing it cannot undo.

  **The fix has two halves and only one of them is the regex.** Inside the markers the line endings should go
  as they came, or normalised to `\n`. Outside them the `\r` conversion stays, because that is what makes a
  small paste behave like typing. So the normalisation belongs on the unbracketed branch, not above the
  branch.

  **THE MECHANISM IS NOT ESTABLISHED, and the exact input is kept so it can be.** The source was saved
  verbatim: 2084 bytes, 27 lines, CRLF throughout, ending `23,276 K\r\n`. What arrived was the last EIGHT
  CHARACTERS, not the last line, and the last line is about seventy characters long. That rules out both of
  the obvious readings:

  - If each `\r` submitted, the result would be twenty seven messages, or one carrying the whole final line.
  - If each line overwrote from column zero, the buffer would hold the whole final line.

  Something is keeping only the text after the final run of whitespace. Whatever that is has not been
  identified, and the `\r` normalisation above is a defect on its own merits whether or not it is the cause.
  Reproduce with the saved input before changing anything, because a fix that makes a short paste work still
  proves nothing here: short pastes already worked.

  **Why this shipped:** `check-terminal.js` rule 5 asserts the markers exist IN THE FILE and cannot assert
  anything about what sits between them. Add a rule that the conversion is inside the unbracketed branch, since
  that is a shape a parser can see.

  **And every paste tested so far was short.** Round 6 check 6.4 was a multi-line paste that worked, over a
  share, through the paste box. This was loopback, direct, twenty seven lines. Three different paths and only
  the short ones were exercised. Any test that replaces this one has to use a block long enough that
  overwriting is visible.

## S. Handing somebody a session, which is the moment the whole feature is judged

Six findings from one screen. This dialog is the entire product of lending a session: everything before it is
machinery and this is what a person sees.

- **THE COPY BUTTON COPIES THE WRONG THING. A defect, and the cause is one missing escape.** The button is
  written as:

      onclick="copyText(this, ${JSON.stringify(s.address).replace(/'/g, "&#39;")})"

  `JSON.stringify` emits the address WRAPPED IN DOUBLE QUOTES, and that sits inside a double-quoted HTML
  attribute. The attribute ends at the first inner quote, so the handler the browser parses is not the handler
  that was written. Only `'` was escaped; `"` was not.

  What the operator got in the clipboard was the paragraph under the box. What they wanted was
  `atrium-b85qy7smrqke.shares.zrok.io/#term=01a06dc7-...`.

  **Do not fix this by escaping harder.** This board already learned that lesson on the file list: "a filename
  with an apostrophe in it breaks an onclick attribute, and HTML escaping does nothing about that", and the fix
  there was to WIRE the handler after rendering rather than to inline it. Same fix here, and the same reason.

- **The address has no scheme.** It reads `atrium-b85qy7smrqke.shares.zrok.io/#term=...`, so pasting it
  somewhere that turns text into links usually will not, and pasting it into an address bar is a guess about
  http versus https. It is https. Say so.

- **It is a link and it is not clickable.** Operator: "that link should 'be a link' that i can click WITH the
  copy icon for the 'copy button' instead". A read-only input with a word beside it is a form field; this is an
  address somebody is about to open or send. An anchor, plus a copy ICON rather than a copy WORD, since the
  word is what makes it read like a field.

- **`zrok token` is a frightening label for something harmless.** Operator: "what is 'zrok token'??? is that a
  leak????" It is the SHARE token, zrok's id for this one share, and the comment beside it says why it is
  shown: it is what releases a share left behind on the account.

  It is not the account token, which is the actual credential and lives in `~/.zrok2/environment.json`. But
  "zrok token" is the phrase everybody uses for the account one, so a label that makes the operator ask whether
  their board just leaked a credential has already failed, whatever the answer is.

  **DECIDED: TAKE IT OFF THE SCREEN.** Operator: "that share token is 'just implementation details' then to me.
  it should be hidden and not shown and just referenced / used when needed". So it is not a labelling problem
  after all. It is on this dialog because releasing an orphaned share needs it, and that is atrium's job rather
  than the operator's: the sweep in `SweepDeadCardShares` already releases names atrium reserved, and anything
  it cannot reach is a bug to fix rather than a string to paste at somebody.

  Remove it here. If a human ever genuinely needs it, it belongs on the shares list beside the share it
  identifies, where somebody cleaning up would look, and not in the flow of handing a link to a person.

- **"Whoever has this drives the session. It survives a restart and stops only when you say so."** Operator:
  "makes NO fucking sense". Three facts about three different things in two sentences: who can use it, what
  happens on a restart, and how it ends. Each matters and none of them is what somebody reads at the moment
  they are about to send a link to a person.

  The sentence that belongs here is the warning: anybody with this link has the terminal. The rest is
  reference, and reference belongs where the share is listed rather than in the flow of handing it over.

- **`stop sharing, for good` stops it with no confirmation.** Operator: "i clicked that but again -- no modal
  while it STOPPED the share. i need that". Stopping gives the address up and it does not come back.

  The code KNOWS this. The comment on that menu row says the wording is a warning because "a flyout row cannot
  carry a help bubble, so the explanation is in the confirmation instead" -- so a confirmation was designed,
  named in a comment, and never wired. `confirmUser` is right there and is what `killTask` uses for a smaller
  loss.
- **THE SECOND-VIEW REFUSAL DOES NOT WORK OVER A SHARE, WHICH IS THE CASE IT EXISTS FOR.** Operator, opening a
  lent session's address while the board held the same terminal: "i do NOT get the same behavior i expected. i
  expected to learn 'hey you can only have one share open take it anyway' like the other thing did".

  Round 9 built the refusal on the `BroadcastChannel` the popped-out windows already used. That channel is
  scoped to an ORIGIN. `atrium-b85qy7smrqke.shares.zrok.io` and `localhost:7778` are two origins, so the two
  windows cannot hear each other at all: the roll call goes out, nobody answers, and the guest correctly
  concludes the card is free.

  So the fix covers two windows on the operator's own machine and does nothing for a guest, which is the whole
  reason two views on one terminal matters. The pty still sizes to the smaller viewer and the wider one still
  redraws wrapped lines on top of itself.

  **The arbitration has to move to the daemon.** `setViewport` already holds a map of every attached viewer per
  runner, keyed by attachment, and the websocket is the one thing both origins talk to. That is the only place
  that can see across origins, across machines, and across a browser that has never heard of the other one.

  Three things to settle when it is written:

  1. **What the second attach gets.** A refusal on the socket, before any output flows, saying the terminal is
     in use. Not a silent close, which reads as the share being broken.
  2. **What "take it anyway" becomes.** In the browser-only version the holder yields over the bus. Across
     origins the daemon has to do it: drop the existing attachment and tell that viewer why. That is a stronger
     act than the local one, since the viewer being kicked may be a person on another machine.
  3. **Whether a guest may take it from the operator at all.** The local case is one person with two windows.
     This one is two people, and the operator lent the session deliberately. Kicking the owner off their own
     terminal because a guest opened the link is not obviously right, and neither is refusing the guest the
     thing they were just handed.

  The browser-side refusal is still worth keeping for the same-origin case: it is faster, it explains itself in
  the window somebody is looking at, and it stops the common accident. It is just not the containment.

- **The guest page works and reads like a bug report.** It correctly recognises a trimmed link now, and the
  operator's verdict on the words was "that page shows me this garbage". What it says:

  > one terminal
  > this link is one terminal. nothing else here is shared.
  > This address ends in #term=<card> and that fragment is what picks the session. It never reaches the server,
  > so a link with it trimmed off arrives here. Ask for the whole link again.

  Four problems, and they are one problem:

  1. **The heading is `one terminal`**, which is a fact about the share and not a description of what happened.
     What happened is that this link is incomplete.
  2. **The daemon's refusal sentence is reused as the opening line.** It was written to answer a request, where
     it is right. As the first thing a person reads it answers a question they did not ask.
  3. **Three sentences of mechanism** -- what a fragment is, that the server never sees it, why the request
     arrived here -- before anything they can do.
  4. **The one actionable sentence is last.** `Ask for the whole link again.`

  Invert it. Say the link is incomplete, say to ask for the whole one, and put the fragment explanation
  underneath for whoever wants to know why.

  **This is the same defect as the zrok account block and the switcher key hint**, both already in this file:
  a verdict is owed first and the working goes underneath. Worth doing as one pass over every explanatory block
  on the board rather than three separate fixes, since three fixes will not stop the fourth being written the
  same way.


---

## R. Reading a card's questions, which is now possible and unpleasant

An ask became a row so a second question stops destroying the first, and the board grew a count and a list to
show it. The data is right. Everything about touching it is wrong, and the operator's verdict on the first
attempt was "the experience sucks".

- **THE DIALOG DOES NOT LIST THE OTHER QUESTIONS. A defect, not a preference.** `withAskCounts` was wired to
  `listTasks` and `waiting` and NOT to `getTask`, so the card dialog reads a card with no `asks_open` on it,
  `paintMoreAsks` sees a count below two, and returns before drawing anything. The row says `+1 more` and the
  card it opens shows one question. Fix `getTask` first: everything below is judged through it.

- **`+1 more` cannot be read without opening the card.** Operator: "worth noting that the '+1 more' won't let
  me copy the text without opening the card -- super fucking annoying". The chip is a count, and the thing
  wanted is the questions.

  Cheapest honest answer is the `title`, so hovering the chip shows the rest as text. Better is drawing them:
  the row already carries one question on its own line, and a second line for a second question is the shape
  that stops needing a chip at all. Decide whether a stack row may be three lines tall before choosing, because
  that is the real constraint and it is why the count exists.

- **Clicking the question drops you into "say something to it".** Operator: "when i click on the question,
  equally stupid is it brings me directly to 'say something to it'".

  That was deliberate and it is wrong. The reasoning was that saying anything answers the question, so the
  fastest path from reading it to answering it is the box. What it misses is that pressing a question is
  mostly how you go and READ it, and being dropped into a text box with the caret blinking is being asked to
  answer something you have not finished reading.

  Open the card, put the question in view, and leave the caret alone. Answering is a button press away and
  should stay one.

- **Escape does not close the card dialog, and there is no cancel.** The only button is `save and close`.
  Every field commits when it loses focus, so there is nothing to cancel, and that is exactly why the missing
  escape reads as being trapped: the dialog behaves like a form and offers one way out that sounds like a
  commitment.

  This is the other end of the round 9 change that renamed `close` to `save and close`. That fixed "the button
  lies about what it does" and created "the button is the only way out". Both halves want answering together:
  escape closes it, and the button says what it says.

  **Check `data-guard` before writing anything.** Several dialogs on this board carry it and something is
  consulting it on `cancel`. If the card dialog is deliberately guarded, the fix is to say WHY on screen rather
  than to remove the guard.

**DESIGN THE FLOW BEFORE WRITING ANY MORE OF IT.** Operator: "it needs to be discussed so leave it for now.
it's working 'like shit' imo so we need to design the flow together next".

The wiring that produced the four faults above is UNCOMMITTED on purpose, in `internal/api/api.go` and
`internal/api/web/index.html`. It is a count on the row, an endpoint, and a list in the dialog, and it was
built by working outward from the data rather than from what somebody does with it. That is why every fault
above is about the doing rather than about the data.

The questions to settle first, before any of it is touched again:

1. **What is a question, on a row?** One line of the oldest with a count is the current answer. The
   alternatives are every question on its own line, or no question at all on the row and a count that opens
   the card. The constraint that decides it is how tall a stack row may be, since that is the whole reason a
   count exists.
2. **Where do you read them?** The dialog is the current answer and it is a form with a question bolted into
   it. A flyout from the row is the other shape, and it is the one that does not make reading a question cost
   opening a form.
3. **How is one answered?** Today ANY message to the card answers ALL of them, which is right when there is
   one and wrong the moment there are two. The store already supports answering one (`AnswerAsk`), and nothing
   reaches it. Decide whether a reply is aimed at a question or at the card, and if at a question, what the
   affordance is.
4. **What happens to a question nobody answers?** They accumulate to a cap of ten and the oldest is retired
   with a note. Nobody has looked at whether that is the behaviour wanted, or whether a card with four
   outstanding questions should be shouting rather than counting.
5. **Does the operator ever want the answered ones?** They are in the event log and nothing draws them. A
   session that asked six things over an afternoon has a history that might be the useful artefact, or might
   be noise.

Answer those and most of the four faults above stop being separate items.

---

## Q. Sending work asks you to remember things atrium already knows

From walking round 8. The queue, the claim and the handout are sound; the dialog in front of them is a form
somebody has to already know the answers to.

- **The room is typed from memory, and it should be picked.** The field is a text box reading `the name that
  machine reports itself under`. Operator: "i should KNOW the rooms and PICK the room not guess and not type".

  The hub already holds every room that has checked in, with its name, whether it takes work, whether it is
  busy, and its workspace. That is a list, and a list is a picker. Typing a name that has to match exactly, on
  a page that is already displaying the correct spellings a few hundred pixels away, is the operator doing
  string matching for the machine.

  A name for a room that has never checked in is still worth allowing, since queueing for a machine that is
  switched off is a stated feature. So: pick from the known ones, with room to type an unknown one, and say
  which it is when it is unknown.

- **The whole form is drawn before a room is chosen, and most of it depends on the room.** Operator: "these
  are all there but they are premature. we don't HAVE a room to send to, we don't KNOW if that room has claude
  etc. most of this should be coming from after choosing the room".

  Every field under `room` is a question about THAT room:

  - **runner** is prefilled `claude` and hinted "this board's runner list says nothing about what is
    configured over there, so it is typed rather than picked". True today, and the room could simply say. A
    room already reports its version, its host and its cards; adding its runner names is one field on the
    check-in, and then it is a picker too.
  - **working directory** is only meaningful for a room started with a workspace, which the room already
    reports. For a room without one it is a field whose only outcome is a refusal, and it should not be drawn.
    For a room with one, the root is known and should be shown rather than described.
  - **title** and **first instruction** are the only two that are about the work rather than the machine.

  So the dialog is two steps: which machine, then what to run on it. The second step is drawn from what that
  machine said about itself.

- **`send work to a machine` is pressable with no rooms at all.** The rooms pane says `No other machines are
  reporting in` and the button beside it opens a form to send work to one of them. It should be disabled, or
  it should open saying there is nowhere to send anything yet and pointing at `add a room`.

- **A test step that says "type `nowhere`" is the symptom, not the workaround.** Written into the walkthrough
  for this round because there was no way to exercise the dialog without inventing a room name. Operator:
  "uh ...no thanks". A form that can only be tested by lying to it is a form that will be used by lying to it.

- **`ctrl-shift-v` opens the box even on loopback, where nothing needed a box.** Operator: "let's stop using
  ctrl-shift-v when you're LOCAL to the machine. i'm finding myself pushing ctrl-shift-v a lot from habit.
  i'm local it should just paste without making me do more work".

  The box exists because reading the clipboard from script is a permission granted per ORIGIN, and every share
  is a new origin whose prompt never gets answered. On loopback that permission was answered once and is
  remembered, so the read settles immediately and the box is pure ceremony.

  `pasteIntoTerm` already races the read against a timeout and falls back to the box, which is the correct
  shape for right click. `ctrl-shift-v` skips the race and opens the box unconditionally, so the one path that
  could just work never tries.

  Make `ctrl-shift-v` take the same path as right click: attempt the read, and open the box only when it does
  not settle. On loopback the paste lands with no box; on a share the box arrives a beat later, which is the
  behaviour that already exists. Nothing needs to detect "am I local" — whether the clipboard answers IS the
  test, and it is the one that stays right on a browser nobody anticipated.

- **The card dialog has no save, and `close` saves anyway.** Operator: "there is no save button just a close
  and that is confusing. i feel like a save and exit button should be floated at the top of that modal. close
  seems to have saved it afaict".

  It did save. Every field patches on its `change` event, which fires when focus leaves it, and pressing
  `close` moves focus, so the write lands on the way out. Correct, invisible, and indistinguishable from
  having lost the edit.

  Two things wrong, and the second is the one that will bite somebody:

  1. **Nothing acknowledges the write.** A field that looks identical typed-in and saved is one you close
     without meaning to, which is the exact sentence already written next to the file editor's `not saved`
     state. The card dialog has no equivalent.
  2. **`close` is the wrong word for a button that commits.** It reads as the discard half of a pair, and on
     this dialog it is the only button there is. Somebody who types into `why`, changes their mind, and
     presses `close` expecting to back out has already saved.

  The operator's fix is a `save and close` floated at the top of the modal. That works and it has to be the
  WHOLE answer rather than a second path: adding a save button while `change` still patches means two ways to
  commit and one of them is still invisible. Either commit on close and say so, or hold the edits and commit
  on the button, and then `close` has to ask about unsaved ones.

- **The card dialog does not show the question.** The stack row draws `this agent has a question` and the ask
  underneath it, correctly. Open that card and the question is not there. Operator: "i DO NOT see that in the
  'settings' of the card and expected to".

  The dialog has `d-why` and a `d-recap-field` that appears when a session has said what it did. There is no
  `d-ask`. So the field that records what a session SAID IT DID has a place in the dialog, and the field that
  records what it is stopped ON does not, which is backwards: the recap is history and the ask is the thing
  wanting an answer right now.

  It belongs beside the recap and shaped like it: shown only when there is one, read-only, because the ask is
  something the session wrote rather than something the operator fills in. With `ask_peer` set it should name
  the peer, the same way the row does, so a card stopped on somebody else reads differently from one stopped
  on you.

  **And the dialog is where answering it should be possible.** Saying anything to a card clears its ask, and
  the dialog is where somebody who just read the question already is. Today they have to close it and find the
  message box.

- **`--working` should be `--continue`.** Operator: "this flag should be renamed to --continue assuming that
  it means 'the agent asked a question but kept going'". That is what it means.

  `--working` names the STATE the session is in, which is the thing the reader already knows, and it reads as
  a claim about being busy rather than as a choice about what happens next. `--continue` names the DECISION,
  which is the only thing the flag actually controls: ask, and carry on rather than stop.

  It also fixes the asymmetry. Today the two calls are `atrium ask "..."` and `atrium ask --working "..."`,
  and nothing about the first says it stops. With `--continue` the pair reads as stop-by-default and carry-on
  by request, which is what it is.

  Rename the flag, keep `--working` as a hidden alias rather than breaking anything already written down, and
  change the wording everywhere it is described: the help text, `docs/user-guide.md` pattern 12, and the
  `blocked` field's own comment in `help.go`, which explains the distinction at length and would otherwise go
  on naming the old one.

- **A second question destroys the first.** `SetAsk` writes one column, so asking again replaces what was
  there. Operator: "it appears to have overwritten the OLD question which is no good. if there's a series of
  questions it should be made clear that the agent has SEVERAL to answer".

  Reproduced in one minute: `atrium ask --working "which of these two schemas is authoritative"` then
  `atrium ask "which branch is base"`, and the first question is gone from the card with nothing saying it
  ever existed. A session that asks two things gets one answered and never learns the other was dropped.

  **This is the same mistake `ask` was built to fix, one level down.** An ask used to be written into `why`
  and destroyed the standing answer to "what was I even doing", so it got a field of its own. A field of its
  own that holds exactly one thing destroys the previous ASK instead. One column, one value, and questions
  arrive one at a time: that shape cannot hold a series.

  It wants a table, the way messages and events already are, keyed by card with a timestamp and an answered-at.
  Then the card draws a count, the dialog lists them, and answering one leaves the others standing. The event
  log already records every ask under `EventSubmitted` with `kind: asked`, so the history exists and only the
  live view is lossy.

  **Decide what an answer answers.** Today any message to the card clears the ask, which is right for one and
  wrong for three. With a list, either a message clears the oldest, or clears all of them, or each is answered
  individually. The third is the only one that stays true with a peer involved, since `ask_peer` is per
  question and today there is one of those too.

- **A question should be answerable where it is drawn.** Operator: "if i click on a question, it feels like i
  should get to 'say' something back on that question?"

  The ask is drawn on the stack row as a static line. Clicking it does nothing, and answering means finding
  the message box elsewhere. Saying anything to the card is ALREADY the mechanism that clears it, so this is a
  path to something that exists rather than new behaviour.

  Same fix as the missing dialog field above, and they should be built together: the question, wherever it is
  drawn, is a control that opens a box aimed at that card. With a list of asks it aims at that question.

- **A human's answer arrives with no question attached.** The session asked `which branch is base`, the
  operator answered `main` from the board, and the session received the word `main` and nothing else.
  Operator: "the response did not carry any of the context from the question".

  **The peer path already does this correctly and the human path does not.** `askEnvelope` wraps a routed
  question with who asked, what they asked, and the command to answer, precisely because the receiver cannot
  work those out. An answer coming back the same way carries the original question quoted. An answer typed
  into the board's message box is delivered as the operator's own words, because that box is a general purpose
  "say something to this session" and knows nothing about asks.

  It costs a session a turn at best. At worst it is wrong: with the multiple-asks item above unfixed, a
  one-word answer to one of two questions is unattributable, and the session picks.

  The fix is not to make every message quote something. It is that a message sent WHILE AN ASK IS OUTSTANDING,
  and which therefore clears it, should say what it clears. Same envelope the peer path uses, minus the
  command to answer, since the human is not being asked to reply.

  **And this is the argument for answering from the question rather than from the message box.** A box aimed at
  a specific ask knows which one it is answering, so the envelope writes itself. A general box has to infer
  it, and inference is what produces "main" with no question on it.

- **The pinned block has no label, and the divider under it names the sort.** Operator: 'change "the rest,
  waiting on you" to a "Pinned" label and "The rest" using the allcaps you have'.

  `stackRows` draws a `.pinbreak` reading `the rest, <sort label>`, and nothing labels what is ABOVE it. So the
  block that outranks the sort is unmarked and the divider explains the sort instead, which is the less
  interesting of the two facts: the reader can see what the sort is, it is a pressed button at the top of the
  screen.

  Two headings in the small caps already used for `THE REST, LAST ACTIVE`: **PINNED** over the pinned block,
  **THE REST** under it. The sort name comes off, since the sort control says it.

  Worth remembering why the divider exists at all, because the fix must not undo it: pinning outranks the sort
  and nothing on screen said so, so three cards at 16m, 2m and 19s sat above one at 4h17m and the only
  explanation was a star twelve pixels wide. Naming both sides is a better answer than naming one.

- **A refusal prints itself three times and then the usage.** `atrium ask --peer nobody-here` answers with a
  written refusal, the five handles that would have worked, and `nothing was asked. run it again with one of
  those, or without --peer to ask a human.` Then cobra prints `Error: no session called nobody-here`, the full
  flag usage, and `atrium: no session called nobody-here`.

  The good part is the part somebody wrote. What follows is twelve lines of flag descriptions for a command
  the operator just ran correctly except for one argument, and the sentence they need is now off the top of a
  short terminal.

  `SilenceUsage` and `SilenceErrors` on the command, and return the error already reported as a sentinel that
  `Execute` does not re-print. Worth doing for every command that writes its own refusal, not just this one:
  `tell` has the same shape, and so does `dispatch`.

- **A card with messages queued for it does not say so on the board.** `atrium peers` prints
  `zrok-research-3500 [1 waiting]`, which is how the operator confirmed a routed question had landed. The
  stack row for that same card shows nothing.

  The count is the answer to "did that arrive", and today it is only available to a model running a CLI. It is
  also the answer to "why is this session about to be interrupted", since a queued message is delivered at the
  gate ahead of every rule and is the second step of the permission chain.

  A small chip on the row, drawn only when the count is above zero. The number already travels: `roster` puts
  it on every `Peer` as `Waiting`, from the same store query, so this is a field the board does not read
  rather than one that has to be computed.

  **The peer list and the board disagreeing about what is known is the thing to watch here.** `peers` grew
  `want`, `note`, `ask`, `recap` and `waiting` because a model needed them in one place. Every one of those is
  a fact about a card, and four of the five are drawn on the row. This is the one that is not.

---

## P. A room has to be worth something with the hub gone

Raised on the way out of the door, and it is the item that decides whether the multi-machine work is worth
- **The operator expects to drive a remote agent from the hub, and the design refuses to.** Asked while
  looking at cdaws's card on the board: "i can't connect to it like i can with you? i'm expecting that this is
  just a fancy 'double stream' of bytes where i talk to you, you stream to hub, hub streams from claude cdaws
  and back, no?"

  No, and the refusal is deliberate. A room posts a SUMMARY of its cards, no bytes cross, and the hub never
  dials a room. Attaching is meant to be a REDIRECT to that room's own board, which is item 2 under `NEXT` and
  is not built. Nor would it work today: cdaws's board is on its own loopback and nothing publishes it, which
  is group P item 1 and also not built.

  So the state is: you can see a remote card, queue work to it, and answer its gate. You cannot type at it from
  anywhere.

  **The expectation is reasonable and the answer should not be "read the design doc".** Two things are being
  traded and only one of them has been written down.

  - **Relaying** puts the hub in the byte path. Then the hub going down takes every terminal with it, which is
    the exact failure group P exists to survive, and the operator asked for discrete shares for that reason.
  - **Redirecting** keeps the hub out of the path and costs every room its own published address, its own
    login, and a link that works from a phone in an airport. That is three unbuilt things standing between the
    operator and a terminal they can already see the card for.

  The redirect is the right answer and it is currently a promise. Until group P lands, the honest thing for the
  board to do is SAY so on a remote card rather than leaving somebody to discover that attach is missing:
  name the room, say its terminals stay there, and say what would make them reachable.

having. Operator: "they need to work AUTONOMOUSLY in situations like this. i want to be able to access them
over a share from anywhere as though i was operating via the hub for when the hub goes offline".

**"Situations like this" is the case to design for**, and it is not a hypothetical: the operator is on a plane,
the hub is a desktop at home, and the machines doing the work are elsewhere again. A design where the hub is
the only way in makes every one of those machines useless the moment one desktop sleeps.

**The good news is that a room is already a whole atrium.** It is not a thin agent. It has its own store, its
own board, its own permission gate, its own supervisor. When the hub goes away a room does not degrade: it
stops checking in and carries on. So this is not "make a room work alone", which it already does. It is
"make a room REACHABLE alone", which it is not.

### What is actually missing

1. **A room publishes its own share, and keeps it.** Today a room reports a `board` address, which is a LAN
   address the hub draws a link to. From a plane that link is nothing. A room needs its own overlay, its own
   reserved name, and a share that comes back after a reboot the way the board's does. Every piece of that
   exists in `overlay_reserve.go` and `RestoreCardShares`; none of it is wired to `atrium room`.

2. **The address has to be knowable when the hub is down.** A share address discovered only through the hub's
   room list is one you cannot look up in the situation this exists for. It has to be somewhere else too: the
   name is deterministic per room, or it is written down where the operator already looks.

3. **AUTHENTICATION STOPS BEING OPTIONAL, and this is the part to think hardest about.** A published board with
   no login is `docs/overlays.md`'s named line: atrium serves a board on an overlay and never decides who may
   connect. A room's board is a machine's whole board, including its terminals, which is a shell. The login
   from round 1 is the answer and it is per machine, so every room needs it configured, and a room published
   without it is a mistake somebody can make in one flag.

   Decide whether `atrium room` may publish at all without a login configured. The defensible answer is no.

4. **The gate has to be answerable, or answer itself.** A blocked agent on a room today waits for somebody to
   open that room's board. With the hub up, `rooms-permissions` forwards the decision. With the hub down and
   the operator on a plane, an agent that hits the gate is stopped until landing. That is either fine, because
   stopping is what the gate is for, or it is what auto mode and standing rules exist for, and a room should
   be able to be started saying which.

5. **DISCRETE SHARES, ONE EACH, AND NO AGGREGATION. Answered, not open.** Operator: "the hub and the rooms
   would all get their own discrete shares. i still would primarily work from the hub but if the hub runs on
   my laptop and the rooms are geographically diverse would want to use my phone to get to the rooms in
   question is all".

   The hub stays where the work is driven from. Every machine including the hub publishes its own address, and
   reaching a room means opening that room's own board, which is a whole atrium and already draws everything.
   Nothing aggregates rooms without a hub, so none of this is federation: it is the share machinery that
   already exists, run in four more places.

   **What it does NOT mean, because it is the shape somebody will build by accident:** the hub does not proxy
   to a room, and a room's board is not embedded in the hub's. Either would put the hub back in the path this
   exists to survive without.

   The phone is the client these addresses are for, which makes group O load-bearing rather than cosmetic: a
   room's board reached from a phone is a room's board at 390px.

### Where this sits

It belongs with `NEXT. Send the work to another machine`, as the thing that makes the sent work retrievable.
Sending work to four machines that are only reachable through one desktop moves the single point of failure
rather than removing it.

---

## O. The board at phone width, beyond the one row that was done

`approvals-from-a-phone` did what its name says: the permission row, the touch targets and the toast clamp.
Everything else at that width was never looked at, and the first pane the operator happened to open was one of
them. Screenshot at 390px, terminals view. Operator: "the view on the whole is 'meh' not great".

What is wrong in that one screenshot:

- **It scrolls sideways.** The tab strip (`stack / board / perms / runners`) and the attached-card strip both
  run off the right edge. A page that scrolls horizontally on a phone is the single loudest signal that
  nobody tried it, and it is the first thing to fix because everything else is judged through it.
- **`nothing attached` is an empty panel filling the screen.** On a desktop it is a placeholder beside a list.
  At 390px it IS the screen, so the whole viewport is given to a sentence saying there is nothing here.
- **The header is over-full.** `1 SESSION SHARED`, the auto-mode dot, the sound toggle and the gear, then a
  second row for the tabs, then a third for the sort control and the card strip. Three rows of chrome before
  any content, on the shortest screen.

**Do not fix this pane by pane.** The permission row was done because somebody sat in front of it; the next
one will be done the same way and the one after that will not. What is missing is a decision about what the
board IS at that width: the whole thing shrunk, or a smaller set of things worth doing from a phone. Approving
a permission and reading what a session asked are worth doing from a phone. Driving a terminal is arguably
not, and `docs/overlays.md` already says lending a session hands out a link rather than making the board
mobile.

Answer that first, then the panes fall out of it. `scripts/check-phone.js` exists now and should grow the
invariants for whatever is decided, starting with "nothing scrolls sideways".

---

## N. The board does not know things until the gear is opened

- **`share this session` says `no overlay is set up yet` on a machine where zrok IS set up.** Reported with a
- **An overlay reports `running` when it is no longer reachable.** The ziti panel said `running: true` since
  `21:35:59` the previous evening. From the other end, a room dialling that same service got
  `service 1OQxriR8fUOF7vwSsQJYVo has no terminators` -- the binding was gone. Stopping and starting the
  overlay brought it back.

  `running` is a flag set when start succeeds and never checked again. It answers "did this start" and the
  board draws it as though it answered "is this reachable". Those are the same fact for about as long as the
  network holds still, which on a laptop that sleeps, roams between networks, or sits behind a travel router is
  not long.

  **The board's own claim about being reachable is the one claim it cannot afford to be wrong about**, because
  the person it is wrong to is somewhere else, and their symptom is silence. This is the same class as the
  round 1 finding that made the board say only what it had checked, and it is the same fix: ask rather than
  remember.

  For ziti, the SDK knows whether the listener is still bound and the controller can be asked how many
  terminators a service has. For zrok, the share either answers or it does not. Neither needs polling on a
  timer: check when the panel is drawn, which is when somebody is asking.

  **And a room that cannot reach the hub should be visible from the hub side too.** Today the hub simply has no
  room, which looks identical to a room nobody ever started. The room knows it is failing and says so in its
  own log, on a machine nobody is watching. That asymmetry is the whole reason `docs/dispatch-queue.md` group P
  exists.

  screenshot: the card menu says to go to the gear and expose the board, while the gear says
  `enabled against https://api-v2.zrok.io/` and the API answers `ready: true`.

  `shareItem` decides from `(overlays || []).some(o => o.kind === k && o.ready)`, and `overlays` is filled in
  by `loadOverlays`, which runs when the SETTINGS DIALOG is opened. The card menu lives on the terminal bar and
  is reachable without ever opening settings, so on a fresh page load the list is empty and the menu reports
  the machine has no overlays. Opening the gear once fixes it until the next reload.

  **`openSkinLab` has the identical bug and already says so out loud**: with no skins loaded it toasts
  `no skins yet: the daemon has not said which ones it has. open the gear once`. That message is a workaround
  written into the product, which is the tell. Two features now, and the next one that needs a daemon fact
  outside settings will be the third.

  The fix is not "call `loadOverlays` from `shareItem`", which makes the menu async and races the flyout. What
  is missing is a place where facts the whole board needs are fetched once on load, beside the cards, and
  refreshed by the SSE event that already fires when an overlay changes. Anything that gates a menu item on a
  daemon fact reads from there.

- **Pressing the side of a toggle that is already on says nothing.** Reported as `use this machine's zrok is
  NOT working`. It is already selected, and the API is already `own: false`. Pressing it POSTs, succeeds,
  changes nothing, and redraws the same thing, so a control that is behaving correctly is indistinguishable
  from one that is broken.

  The `.seg` toggles are drawn with the active side highlighted, which is enough when you are choosing and not
  enough when you are checking. Two ways out, and the second is cheaper: make the on side non-interactive so
  the press is visibly not a press, or acknowledge a no-op change the way every other save on this board does.
  Whichever, the same reasoning applies to every `.seg` on the page, not just this one.

- **Switching account there and back refuses, over a trailing slash.** Press `give atrium its own`, then press
  `use this machine's zrok`, and it says:

  > could not switch account: that environment is already enabled against https://api-v2.zrok.io/. disable it
  > first, then set the address, then enable again

  Operator: "which is dumb". It is, and the address in the message is the address being sent, which is the
  tell.

  **What is actually happening.** `pickZrokAccount` posts `{own, endpoint}` where the endpoint is whatever text
  the instance box currently holds. Choosing WHICH ENVIRONMENT is not choosing WHICH INSTANCE, so that call
  asks to change an address nobody touched.

  Then the two addresses differ by one character. The machine's environment is enabled against
  `https://api-v2.zrok.io/`, with a slash. Atrium's own environment is not enabled, so its endpoint is read
  from the zrok binary's default, which comes back as `https://api-v2.zrok.io`, without one:
  `api_endpoint_from: "binary"` in the overlays payload says so. Pressing `give atrium its own` repaints the
  box with the unslashed form, and pressing back sends it at an environment holding the slashed form.
  `SetZrokEnvironment` compares them with `!=`, they are not equal, and it refuses a change that was never
  requested.

  Two fixes and both are wanted:

  1. **`pickZrokAccount` sends no endpoint at all.** The endpoint travels only when somebody presses `use this
     one` on the instance field. `SetZrokEnvironment` already handles an empty endpoint as "nothing to say
     about where it points", which is the correct meaning of a bare account switch, and the path is already
     proven: posting `{"own":false,"endpoint":""}` succeeds against the same enabled environment that refuses
     the same request with an address attached.
  2. **Compare endpoints normalised, not by string.** A trailing slash makes two identical addresses unequal,
     and so would a case difference in the host. Anything comparing two URLs for "is this the same instance"
     has to do it after normalising, here and anywhere else it is done.

  **The refusal message is also wrong to keep as written**, even after the above. It describes a three step
  procedure for a thing atrium could do, and the operator reading it did not ask to change any address. If it
  survives at all it should name what it thinks is being changed, from what to what, so a one character
  difference is visible rather than invisible.

- **A lent session's address, without its fragment, serves a board that can never work.** Operator, opening
  `https://atrium-j4cf3nq8qv6q.shares.zrok.io/`: "the board ... is NOT correct at all.. is it a shadow/clone?"

  It is not a clone and nothing leaked. `guestHandler` serves the same static `index.html` at `/` and refuses
  `/v1/tasks`, `/v1/events`, `/v1/permissions` and `/v1/rooms` outright, so the page renders its own chrome
  and every list in it stays empty. The address only becomes a terminal because of `#term=<id>`, which is a
  fragment the server never sees and therefore cannot act on.

  The containment is right. What is wrong is that the failure of a guest opening the wrong half of their own
  link is indistinguishable from atrium being broken, and the person best placed to notice mistook it for a
  clone of their own board.

  The page is static and identical for everybody, so it cannot be built differently per share, which rules out
  the obvious fix. What it CAN do is notice: it is already the guest page whenever `/v1/tasks` answers 403,
  and that is a fact it can act on at load. Say what this address is for and what is missing from the link,
  rather than drawing an empty board.

- **Pasting a picture over a lent share fails with a message that hides the reason.** Operator, on the share:
  "i was NOT able to take a screen cap and send it to you getting the 'that did not go up' (stupid error)".

  `guestHandler` refuses `/v1/tasks/*/files` on purpose and says why in its own words: `this link is one
  terminal. nothing else here is shared.` The page throws that away and shows `that did not go up`, so the one
  refusal on this surface that is deliberate reads as a transfer that broke.

  Two things, and they are separable:

  1. **Say the daemon's reason.** The 403 already carries a sentence written for exactly this moment. Anything
     that reports a failed upload should print what came back rather than a phrase of its own.
  2. **Do not offer it at all over a share.** The paste target, the drop zone and the file drawer are drawn by
     the same page whether it is the board or a guest, so a guest is invited to do something that cannot work.
     The page can already tell it is a guest, from `/v1/tasks` answering 403, which is the same signal the
     empty-board item above turns on.

  **Do not "fix" this by opening files to a guest.** The refusal is the containment: `docs/overlays.md` says a
  lent session is that session and not the machine, and the directory behind a card is the machine.

  **AND THIS COLLIDES WITH A DECISION ALREADY TAKEN, which is the part to settle before writing anything.**
  The rule for dragging a file onto a terminal, in the operator's words: "when dragging a file from THE SAME
  COMPUTER then this should just be a 'hey look at this file' and provide the full path ... we should only copy
  the file when the terminal is remote from the ui".

  A lent share is the remote case. It is the one where copying is the wanted behaviour, and it is the one place
  atrium refuses to copy at all. So the two rules meet head on:

  - **The containment rule says no.** A guest holds one terminal, not the machine, and writing a file into the
    card's directory is writing to the machine. `guestHandler` names `/v1/tasks/*/files` in its refusal list on
    purpose.
  - **The drag rule says this is exactly when to copy.** Same-machine drags do not need a copy, so refusing the
    remote case refuses the only case the feature exists for.

  They can both hold, and saying how is the work:

  - A guest who was handed a terminal to DRIVE is not the same as a guest who may put files on the machine.
    Those could be two things the operator grants separately when lending, and today there is one.
  - A file dropped onto a terminal could go to the terminal rather than to the directory: written into the pty
    as a paste, or into a scratch location that is not the card's worktree. `api.ScrapDir` already exists for
    pasted files nobody is keeping, and it is not inside a card.

  Decide which, and write it into `docs/overlays.md` beside the rule it qualifies. What must not happen is
  somebody opening the file endpoints to guests because a drag failed.

### Revisit the share once these land

Round 6 was accepted with four of its six checks unrun, and the reason is worth writing down: every one of
them needed a share, and the share is the surface with the most outstanding items against it. Testing them
now would be testing around three known faults at once.

What has to land before the share is worth walking again:

- **Refuse the second view** (group I). Two windows on one terminal wrecked the rendering in the wider one
  throughout this session, twice, and it is indistinguishable from the terminal itself being broken.
- **The guest page knows it is a guest** (this group). An empty board at the bare address, and `that did not
  go up` standing in for a deliberate refusal.
- **The zrok account switch** (this group). Until it is fixed the board's OWN address cannot be set, so every
  share test runs against a lent session's address instead, which is a different surface with different rules.

Then re-run, on the board's own share and on loopback:

- `ctrl-v` in the terminal pastes with no box (6.1)
- right click pauses about a second and opens the box (6.2)
- the box is empty the next time it opens (6.5)
- NO box on loopback, the paste goes straight in (6.6)

6.3 and 6.4 passed on a lent share and do not need redoing: `ctrl-shift-v` opened the box at once, and a
multi-line paste arrived as one paste.

---

---

## M. Scrollback does not survive a restart, and the restart is atrium's own

Operator: "if i am in a terminal (here) and then you restart, and then i close this window and open it up again
- **Resizing the window costs the whole scrollback, silently.** Operator, mid-session: "i have lost the
  scrollback and can't see it which is super fucking annoying". Nothing restarted and nothing was killed. The
  width changed.

  The ring replays only the run of output composed at the width the terminal is at NOW, and a width nothing was
  ever written at is not a mark, so it returns nothing at all. The rule is correct on its own terms: bytes
  composed for eighty columns rendered into a hundred and twenty are unreadable, and that lesson was learned
  the hard way. What was never written down is the price. Any resize, any zoom, any font change, and the
  history is gone until enough new output accumulates at the new width.

  It is worse than it sounds because the operator does not connect the two events. Resizing a window is not an
  action anybody expects to destroy anything, there is no message, and the buffer is still there on the daemon
  holding output at a width nobody is looking at any more.

  Three ways out, and they are not equal:

  1. **Say so.** One line where the scrollback would be: the history was written at another width, resize back
     to see it. Cheap, honest, and does not get the history back.
  2. **Reflow.** Keep the bytes and re-wrap them for the new width. This is what a terminal emulator does with
     its own buffer and it is the reason xterm can resize without losing anything. Atrium is holding RAW BYTES
     including escape sequences, so re-wrapping means interpreting them, which is most of a terminal emulator.
  3. **Keep a run per width.** The ring already marks widths; keep the marked runs rather than only the last
     one, and replay whichever matches. Bounded by the same ring, costs nothing when the width never changes,
     and gives the history back the moment somebody resizes to a width they used before.

  The third is the one that fits what is already there. The first should happen regardless, because even after
  the third there will be a first visit to a new width.

  **This also explains the carried buffer across a restart.** Round 9 writes one width in the header for the
  same reason, so a restart plus a resize is two ways to lose the same thing. Fixing the live ring and leaving
  the carried file alone would be half an answer.

from the stack page my scrollback is only since i got here not 'forever' back". And: "same for viewing it in
'terminals'". Both surfaces, one cause.

**Where the scrollback actually lives, and why there is none of it afterwards.** There are two buffers and
`internal/api/scrollback.go` says so at the top. The daemon keeps the last N bytes of a runner's output in a
ring buffer, sized at spawn, held by the `runner` in the supervisor, in memory and nowhere else. The browser
keeps the last N lines of what it was sent, in xterm, in the page.

A restart destroys both. The daemon exits, so every ring goes with it. Every pty is closed, so every runner is
killed, and a fixture that comes back is a NEW process with a NEW ring holding nothing. The only surviving copy
of what came before was the xterm buffer in whatever page happened to be open, which is why the window that
lived through the restart still shows everything and a window opened afterwards shows nothing: closing that
window is the moment the last copy is discarded.

So this is not attach losing the scrollback. It is that after a restart, atrium no longer has it to send.

**Why it reads worse than it is.** The conversation itself survives: a fixture comes back on its resume id and
the model still knows what it was doing. It is the RENDERING that is gone, and the operator's own words for
what they expect are "forever back". A board whose whole premise is "what was I even doing" answering that with
an empty buffer is the failure this contradicts most directly.

**This is the concrete demand for the transcript on disk.** `CLAUDE.md` lists "per-agent transcript on disk"
under things atrium might do later, unpromised and unmotivated. This is the motivation. Whoever takes it has to
answer, in this order:

1. **What is written down.** The raw bytes, escape sequences and all, is the only thing that replays into a
   terminal correctly. That means the file contains everything the runner ever printed, including whatever it
   printed a token into.
2. **Where it lives and who can read it.** Beside the database, under the daemon's own directory, and NOT
   reachable through the file endpoints, which are scoped to a card's own directory on purpose.
3. **What bounds it.** The ring is bounded by construction; a file is not. A per-card cap and a sweep, and the
   sweep has to be the one in `reaper.go` rather than a second timer.
4. **What happens on replay at a different width.** The ring already has this problem and solves it with
   `widthMark`: bytes composed at another width are not replayed, because replaying them is what makes an
   attach unreadable. A file spanning many widths needs the same answer, and it is the harder version.
5. **Whether a restart is special.** The cheapest useful version is not a full transcript: it is the daemon
   writing its ring buffers out during the wind-down it already narrates, and reading them back on the way up.
   That fixes exactly the case reported here, is bounded by construction, and does not put every session's
   output on disk forever. It does nothing for a crash, which is the honest limit.

Start with 5 and decide whether 1 through 4 are still wanted afterwards.

---

## L. Settings, the theme editor, and saying what to do next

From walking round 5. Two of these are one bug wearing two faces: a refusal that describes a state instead of
offering the way past it.

- **`nothing was brought in` is a report where an offer belongs.** Importing a scheme whose name is already
  here says that, with `already here. import again with overwrite to replace it` underneath. Operator:
  "is a stupid error message. how about 'This theme already exists would you like to import it with a
  different name' or something like that".

  Overwrite is one of two ways past this and it is the destructive one. The other, keeping both under a
  second name, is the one somebody trying out a palette actually wants, and it is not offered at all: the
  name comes from the scheme and there is nowhere to change it.

  So the refusal should carry a name box and two buttons, `import as <name>` and `overwrite`. And the top
  line should say what happened to the batch rather than to nothing: importing forty schemes of which two
  collided is not "nothing was brought in", and today it reads that way whenever every scheme in the file
  collides, which is what re-importing your own `settings.json` always does.

- **Save does not close the theme editor.** Pressing save leaves the modal open, so the way back to settings
  is the close button, and the settings dialog it came from was closed on the way in. Save should close it and
  return to settings, which is where it was opened from.

  Note the asymmetry it creates today: `bring or edit a theme` closes settings to open the editor, and the
  editor does not put it back. Anything that closes one dialog to open another owes the way back.

- **The settings panes need a rule between their sections.** Operator: "each of the settings needs a
  horizontal divider for the sections, the title is not sufficient". A pane is a run of `.field` blocks with
  an eyebrow label each, and at a glance nothing says where one subject stops and the next starts. `the board`
  is the worst of them: the switcher key, grouping, text size and the terminal's colours are four unrelated
  decisions in one column.

  The stack already solved this once with `.pinbreak`, which is a rule with a word on it that separates
  without titling. Same idea here.

- **The left-hand nav may need subgroups.** Seven panes with flat names, and finding the terminal's colours
  means knowing it lives under `the board` rather than under `this machine`. Operator: "the LHN might need
  subgroupings too to make it easier to find these things".

  Recorded as an idea rather than a plan, and it is worth asking first whether the pane NAMES are the problem
  before adding a level. A search box over the settings is the other shape, and it is the one that scales past
  the next three panes.

- **A brought theme is not offered under `how the board looks`, and the operator expected it to be.**
  Reported as a bug: "it shows up in the theme editor but it does NOT allow me to see it and pick it from
  'how the board looks'".

  It is the documented separation, not a defect. A terminal theme is the sixteen ANSI colours xterm draws a
  session in, it belongs to a card, and two side by side may differ. A skin is the board's own chrome, there
  is one board so it cannot differ, and the list lives in `skins.go` with a check that fails when it and the
  stylesheet disagree. `internal/api/CLAUDE.md` says so at length.

  The finding is that the distinction does not survive contact. Both settings sit in the same pane, both are
  called colours, and the hint under the skin picker already explains the difference, which is evidence the
  labels are doing the work the layout should. The verdict-over-explanation rule from the zrok block applies:
  say what each one paints before saying what it is not.

  A brought terminal theme COULD seed a skin, and that is a different feature rather than this one: a skin is
  twenty one CSS variables including hairlines, sinks and strokes, and sixteen ANSI colours do not contain
  them. Anything that generated the rest would be inventing a palette and calling it yours.

- **Edit the colours while looking at them.** Operator: "fucking sexy backlog idea ... know how the modal lets
  you pick the theme from 'try them' it would be cool to allow that modal to have a toggle to extend to the
  'edit the colors' and preview it all live".

  `openSkinLab` already built the hard half: a small draggable panel that closes the dialog covering the thing
  being judged, steps through the list, and has `use it` / `cancel`. The same shape is what the terminal theme
  editor wants and does not have, because a palette edited in a modal is judged against a swatch grid rather
  than against real output, which is the whole reason the terminal's own picker sits on the terminal bar.

  So: a toggle on the lab that extends it into the sixteen colour wells, applying on input, with `use it` and
  `cancel` already meaning the right things. Two notes for whoever takes it.

  1. **Live means live on the terminal, not on a preview strip.** The value is seeing `git diff` in the colour
     you are choosing. `previewTheme` already writes straight through to xterm, so the machinery exists.
  2. **Cancel has to put back what was there,** including after twenty edits, which the lab already does for
     skins by holding the value it opened on.

- **The skin lab's hint explains its own design instead of saying what to press.** It reads `Arrow keys walk
  the list. Nothing is saved until you press use it.` Operator: "stupid just 'Use the arrow keys to see
  next|previous'".

  Same shape as the switcher key hint above: the second sentence is reassurance about the implementation,
  aimed at somebody worried they are about to break something, and the panel already answers that with three
  buttons labelled `use it`, `use the default` and `cancel`. The one thing the panel does NOT say is that the
  arrow keys work at all, which is the sentence worth keeping.

- **The skin lab does not close on escape.** `cancelSkin` exists, puts the previous skin back, and has exactly
  two callers: the cancel button, and `openSkinLab` toggling itself shut. Nothing is bound to escape, so the
  one key everybody presses to back out of a floating panel does nothing.

  It is a panel rather than a `<dialog>`, on purpose, because it has to sit over a board you are still using.
  So it needs its own keydown while it is open, and that listener has to go when it closes: this board has
  already been bitten by a listener that outlived its panel (see `check-terminal.js` rule 1).

---

## K. The switcher, after using it

Landed in round 4 and it works. Operator: "the switch works pretty well", and on the rebinder, "i like 'the
key that opens the switcher'!! nice". Four things, and the second one is a behaviour decision rather than a
polish item.

- **It is too narrow, and the titles are the thing being read.** At its current width a card reads
  `github/openziti/zrok:...` and `github/dovholuknf/atri...`, which is the org and the repo, which every row
  shares. The part that tells them apart is cut off.

  Operator: "i want it to be wider though ... with some sort of width cap so that it's not 100% of the screen.
  maybe 50vw?". So `min(50vw, ...)` with a floor, not a percentage on its own: on a phone 50vw is unusable and
  on an ultrawide it is a stripe across the middle. The terminal list already solves the same problem.

- **Switching to a card that is already popped out should RAISE that window.** It refuses instead: "atrium
  will not put two views on one terminal. go to that window instead."

  The refusal is the correct rule and the wrong answer. The board can already raise a window it opened, by
  name, and `attachTask` does exactly that: `reopenByName` then `focus()`. The switcher declines and hands the
  work back to the operator, who now has to find the window themselves, which is what the switcher exists to
  avoid. Route it through the same path `attach` uses, so a popped-out card is a card the switcher can go to.

  Keep the refusal for the case `attach` also cannot solve: a window this board did not open, which no page
  may raise. That message is right, and it is not this one.

- **A bound key that the browser swallows says nothing.** `ctrl-shift-l` bound and then did nothing, with no
  message. Operator: "probably swallowed by the browser? but i didn't get any notification that it didn't
  work :(".

  `docs/switcher-design.md` has a theft detector, and it cannot cover this. It works by opening the switcher
  and checking a quarter of a second later whether the document still has focus, which catches a key that is
  delivered TWICE. A key that never reaches the page at all never runs the handler, so there is nothing to
  detect from.

  The place it IS detectable is the moment of binding, and the information is already there: the capture field
  saw the keystroke, so that combination reaches the page under a dialog. Confirm it reaches the page with the
  dialog CLOSED. Bind, close, ask for one confirming press, and refuse the binding if it does not arrive. That
  covers every extension and every browser nobody anticipated, without a list.

- **`ctrl-k` as the default.** Operator: "i was able to use ctrl-k... can that be the default?".

  It was ruled out on purpose and the reason is in `docs/switcher-design.md`: `ctrl-k` is readline's
  kill-line, this is a board of terminals, and the switcher's handler runs in the capture phase with
  `stopPropagation`, so binding it takes kill-line away from every shell on the board. Something typed for
  twenty years stops working in one application with nothing on screen to explain it.

  That is a cost, not a veto, and it is the operator's to accept. Three shapes:

  1. **Make it the default.** Kill-line goes. Say so in the setting rather than leaving it to be discovered.
  2. **`ctrl-k` only when the terminal does not have focus.** Kill-line survives where it is used and the
     shorter key works everywhere else. Harder to explain than to build, and a key that works in one half of
     the window is its own complaint.
  3. **Leave the default and make the rebinder easier to find.** It was found and liked, which is evidence
     this is already close to sufficient.

- **A switcher that looks like the stack, and possibly a choice of looks.** Operator: "i can see at some point
  this actually having a 'stack' look/feel so it feels like that main page maybe ... or maybe even make the
  switcher skinnable/themeable? default, look like stack etc".

  Not polish. The switcher and the stack answer the same question, which is "which of these do I want", and
  they answer it in two different vocabularies: the stack has the status colour, the activity badge, the idle
  clock, the `ctx` chip and the pinned divider, and the switcher has a title and two chips. Somebody who reads
  the stack all day arrives at the switcher and has to re-learn the same list.

  The cheap version is one more row renderer over the same cards, since `stackRow` already exists and already
  draws every one of those. The reason it is not a five minute change is the width: a stack row is written for
  a full-width column and the switcher is a modal, so this and the width item above are one piece of work.

  **The skinnable version is the one to be careful about.** A setting that offers `default` or `looks like the
  stack` is two renderers to keep working, two things every future card field has to be added to, and a second
  place for them to drift, which is the exact failure `termFilesCtx` exists to prevent on the file lists. Pick
  ONE look unless there is a reason both must exist, and if both must, drive them from one row builder with a
  density flag rather than from two.

- **The switcher key hint is too long and asserts what it cannot know.** Today it reads:

  > Press the button, then press the keys you want. Needs ctrl, alt or cmd in it, or it would fire while you
  > are typing into a session. ctrl-shift-k is the default: Chrome, Edge and Brave leave it alone, and a
  > terminal does not use it. Firefox takes it for the Web Console and will not give it back, so rebind to
  > alt-k there. ctrl-k works in every browser and costs you readline's kill-line in every session, which is
  > the trade it looks like.

  Operator: "the text is sorta stupid ... it asserts too much we don't KNOW they leave it alone. just simple
  'bound to ctrl-shift-k'". And: "too wordy and too archeological".

  Both complaints are the same complaint. Naming three browsers as leaving a key alone is a claim about every
  version of three browsers on every platform with every extension installed, and the operator had just been
  bitten by a binding a browser took without saying so. A hint that asserts something the page cannot check,
  and is wrong once, teaches somebody to distrust the rest of it. The Firefox sentence is the same shape plus
  a history lesson nobody in front of the control needs.

  What the page actually knows: what it is bound to, what a binding must contain, and what to do when one does
  nothing. Three short sentences at most. The browser-by-browser reasoning belongs in
  `docs/switcher-design.md`, which already has it.

  **The wider rule this is one instance of.** Several hintlines on this board are written like the commit
  message of the change that added them. A hint is read by somebody with a decision in front of them, and the
  reason a thing is the way it is only earns space when it changes the decision. Worth a pass over all of them
  rather than fixing this one.

## J. The file viewer, once a path in the terminal became clickable

Round 3 made a path a link, and the thing it opens turned out to be the part nobody had used. Six findings
from one sitting. They are one session: all of them are the drawer, the editor, and the two functions that
open them.

- **Closing the editor should put you back where you came from.** Operator: "clicking on a file - on close -
  should return me to here NOT to the file picker UNLESS of course i came FROM the file picker".

  Opening a file from the TERMINAL is a detour, and the way back is the terminal. Opening one from the file
  browser is a step, and the way back is the list. `openFromTerminal` and the browser's own `edit` chip both
  end in `openEditor`, which knows nothing about which of the two it was, so closing always lands on the list.
  The caller has to say, and `closeEditor` has to act on it.

  Note that this is the second bug in as many hours caused by the editor being a panel INSIDE the drawer. The
  first was that it opened invisibly. It may be worth asking whether it belongs there at all.

- **A symlinked file cannot be opened.** `CLAUDE.md` in this repo is a symlink to
  `dotagents/github/dovholuknf/atrium/CLAUDE.md`, which is outside the card, and `internal/safepath` follows
  symlinks on BOTH sides on purpose: a link that leaves the card is how a card is escaped, and refusing it is
  the rule that makes the file endpoints safe to expose. See `docs/file-transfer-design.md`.

  So the refusal is correct and the behaviour is still wrong: `files/probe` says the path is a file and
  underlines it, and `files/text` then refuses it. The link should not be drawn at all, which means `probe`
  has to answer the same question `text` will, or the click has to say why in a sentence naming the symlink.
  Eight CLAUDE.md files in this repo are symlinks, so this is not an edge case here.

- **A line number is thrown away.** `internal/api/api.go:248:1` opens the file and lands at the END of it.
  The suffix is already parsed off to find the name; nothing carries it through. It should scroll to that
  line and mark it until the next keystroke or click. Operator: "i might be asking too much" — it is one
  `scrollTop` and one background colour on a textarea, which cannot mark a line, so this probably decides the
  next item.

- **The editor is a textarea, and it is starting to show.** Asked for: line numbers, and "MAYBE a better
  editor (vscode embedded into atrium?)". Monaco or CodeMirror would bring line numbers, a line to jump to, a
  selection to highlight, and syntax colour, all four of which are now wanted.

  Against it: the board is ONE FILE with no build step and vendored dependencies, and Monaco is neither
  small nor a single file. CodeMirror 6 is modular and still wants a bundler. Whatever is picked has to
  arrive as a vendored UMD build like xterm did, or the no-build-step rule goes, and that rule is why the
  board works offline and over a share. Decide that before writing any of it.

- **Escape closes the editor and not the drawer.** Every other panel on this board closes on escape. The
  drawer is the one that does not, so the key does half of what it looks like it does.

- **The up arrow in the file browser does not read as a button.** Operator: "needs to be more button esque.
  it's not very obvious with the reskinning we did not long back". It is `<button class="icon">` with an
  arrow glyph, and the icon class lost most of its affordance in the reskin. It is the commonest control in
  that pane.

- **A clicked path could open in the LOCAL editor when the browser is on the daemon's machine.** Operator:
  "given that i am local to this machine clicking on a file COULD choose to be opened in my local editor".

  `files/open` already does this and is deliberately unreachable from a terminal link, for the reason
  `check-terminal.js` rule 13 states: over a share it starts an editor where nobody is sitting. That rule
  holds. What it over-corrected is the case where the two machines are the same machine, which is most of the
  time for the person who wrote it.

  So the answer is a CHOICE rather than a default, and the board can tell when the choice is available: the
  daemon knows its own hostname and the board knows whether it is on loopback. Same-machine offers both, a
  share offers only the in-page viewer, and nothing has to be configured. A modifier on the click, or a second
  chip in the hover tip, rather than a setting.

  Do NOT make local-editor the default even when it is safe. A default that changes with how the board was
  reached is a default nobody can predict.

- **A picture refuses instead of being shown.** Clicking an image gives `could not read it: that file is not
  text. download it instead`, which is accurate and is the wrong outcome: the file browser already knows the
  type, `files/download` already serves the bytes, and the pane is a browser.

  An image opens as an image. Anything the browser can render inline (png, jpg, gif, webp, svg, pdf) goes in
  the same panel the editor uses, read-only. Everything else keeps the refusal, and the refusal keeps the
  download link it already offers.

  **SVG is the one to think about before writing it.** It is text, it is a picture, and it executes script. It
  belongs in the viewer as an `<img>` and never inlined into the page, or a file in somebody's worktree gets
  to run on the board's origin.

---

## I. Two views on one terminal, which nothing arbitrates

Found by walking round 1's own test P2, which was supposed to prove the board REFUSES to open a second view.
It does refuse to open one. It does nothing about a second view that is already there, and getting one there
takes ten seconds: paste `<board>/#term=<card id>` into a tab.

**The rendering breaks in the bigger window, not the smaller one.** `setViewport` resizes the pty to
`smallestViewport`, which is right, and the design comment beside it claims the cost is "unused margin in the
larger window". It is not. Nothing tells the larger window's xterm to use the agreed size, so it stays at its
own fit, and every cursor-positioning escape the runner emits is computed for a narrower line than the one it
lands on. A wrapped input line redraws on top of itself: `accepting newlines for some reason` comes out as
`acceptingenewlineshforisomewreason`, one character eating each space at what would have been a wrap column.
The narrow window looks perfect the whole time, which is why this reads as "the window I am typing in is the
broken one".

**REFUSE THE SECOND VIEW. Decided, not open.** The operator's call, against the alternative below, and the
reason is that one terminal with two people in it is not a feature anybody asked for. Sharing a session is
what `docs/overlays.md` lends a card for, and that path hands out a share rather than a second cursor on the
same line editor.

`soloHeld` already records who holds a card and for how long, and `poppedOut` already asks the question. What
is missing is anything acting on the answer. A `#term=` window that opens onto a card somebody else is already
holding should say so and stop, rather than attach and quietly shrink the other window's terminal. The board's
own `attach` already refuses; the pasted URL is the door left open.

Two things it has to get right, because both are how a naive version of this becomes worse than the bug:

- **A claim has to expire.** A window that was closed without releasing must not lock a card out. The roll call
  exists for exactly this and is already used by `attach`.
- **A refusal has to be recoverable in one click.** "That card is open in another window" with nothing next to
  it strands somebody whose other window is behind fourteen others. It needs the raise, or a take-it-anyway
  that detaches the other one.

The alternative, recorded because it will be re-proposed:

- **Let both views work.** The agreed viewport goes back down the socket on attach and on every recompute, and
  a viewer whose own fit is bigger letterboxes to it rather than filling. The daemon already computes the
  number; nothing sends it. More work, and it buys a shape nobody wants.

**And the toast goes to every window holding the card.** `sayInSoloWindow` broadcasts `solo-toast` keyed on the
card, and the receiver draws it if the card is its own. With two windows on one card that is two toasts, one
of them in the window you are already looking at, saying the terminal is in this window. Round 1 fixed the
wrong-window half of this. The already-here half is left: a window that is already showing the card and
already has focus has nothing to be told.

---

## H. How to hand back a parallel run

**Not a feature. A working rule that does not exist yet, and the thing tonight most obviously lacked.**

Twenty-six agents finished at once and the handover was improvised: a stack rank written from what the
operator had complained about that day, then eight worktrees copied file by file into the main checkout. It
worked, it was not repeatable, and two things about it were wrong in ways worth writing down before they are
forgotten.

**Merge cost was decided by file overlap, not by topic.** The batch that landed cleanly was chosen on one
property: nothing in it touched `internal/api/web/index.html`. Eight worktrees, three conflicts, all of them
append-shaped. That grouping is computable rather than judged, from `git status --porcelain` in every worktree
intersected against the others, and it was run AFTER the review had started. Run first, it would have said that
thirteen sessions were queued on one file, which is the single most useful fact about that run.

**Copying files into the tree asks for a trust that a patch does not.** A numbered series applied one at a
time, each applying alone, each green under `scripts/ci.sh` alone, each reverting alone, can be judged. A
directory of copied files cannot. The unattended workflow already produces patch series, so this is a shape
that exists rather than machinery to build.

The candidate rule, to be argued with rather than adopted:

> When a parallel run finishes: survey what every worktree touched and where they collide, before anything
> else. Stack rank for review by what will be felt first, what blocks the operator, and what can wait, saying
> which is which. Group into chunks by conflict surface rather than by topic. Adapt each chunk onto main as a
> numbered patch that applies alone, passes ci alone, and reverts alone. Hand over the table and the order,
> and apply nothing until told.

**Two things it has to answer, because both bit tonight:**

- **The top of a ranking is not derivable from the code.** The first three items were ranked by what the
  operator had sworn at that afternoon. Without that input a ranking degrades to "biggest diff first", which
  is worthless. So the rule needs a way to ask for it, or to say plainly when it is missing.
- **Chunking moves a decision from the operator to whoever chunks.** A bad change rides into a chunk of good
  ones and gets approved with them. The guard is that a chunk is only a chunk if it reverts on its own: two
  things that cannot be separated are one patch, and the description has to say why they could not be.

**Where it lives is also open.** In this repo it is a rule about atrium's own backlog. In `dotagents` it would
apply to every repository the operator runs agents against, which is the direction this is going.
---

## DONE IN ROUND 9

Fixed on 2026-09-07, straight after the round 1 to 8 review that found them. Kept here rather than deleted,
because several of these were fixed once before and came back, and the write-up is the record of what the
mistake actually was.

- **Two views on one terminal** (was group I). A solo window now asks whether anybody holds the card before it
  attaches, using the roll call rather than the heartbeat, and offers `take it anyway` rather than a close
  button that would have lied. The letterboxing alternative was not built, as decided.
- **The raise toast drawn by every window holding the card** (was group I). Falls out of the above: there is
  only ever one.
- **The guest page did not know it was a guest** (was group N). A 403 from `/v1/tasks` is the signal. Paste and
  drop are withdrawn there, and `api()` now keeps the daemon's sentence instead of turning every `http.Error`
  into the word `Forbidden`, which is what made `that did not go up` useless.
- **The zrok account switch, over a trailing slash** (was group N). Endpoints compared normalised, the toggle
  no longer sends an address change nobody asked for, and the surviving refusal names both addresses.
- **Scrollback did not survive a restart** (was group M). Rings are written out during the wind-down and handed
  back per card. Not a transcript. Does nothing for a kill, and says so.
- **A second question destroyed the first** (was in the round 8 findings). An ask is a row now, answered
  individually, capped at ten with the drop recorded.
- **The card dialog did not show the question, and could not answer it** (was group J).
- **The card dialog had no save and `close` saved anyway** (was group Q).
- **`--working` renamed to `--continue`**, with the old name hidden rather than removed.
- **A refusal printed itself three times and then the usage** (was group Q).
- **`ctrl-shift-v` opened the box on loopback** (was group Q).
- **The pinned block had no label and the divider named the sort** (was group Q).
- **css nits 5 and 6**, the restart banner and the group expander. Nit 5's real cause was not the one the nit
  was written about: detaching cleared one theme variable and left two behind. See `docs/css-nits.md`.

**Also fixed, and neither was on any list.** Four sections of `docs/test-plan.md` were all called `L`, and five
branches had numbered their migration `0039`. Renumbered L through U and 0040 through 0043.

**And a test that was passing without running anything.** `TestSetZrokEnvironmentIgnoresATrailingSlash` and its
pair assert that a call returns no error. The fixture wrote `environment.json` without `metadata.json`, so the
zrok SDK loaded a default root, `IsEnabled()` was false, and `SetZrokEnvironment` returned early by a completely
different path. Both tests passed without reaching the comparison they exist for. The fixture writes both files
now and `requireEnabled` asserts the precondition, which is the part that stops it coming back.


## Group T: the restart path cannot stop the daemon

Found on 2026-09-08, and it had been lying about success for at least a day.

`POST /v1/shutdown` answers **403** on a daemon started with a shutdown token. `stopDaemon` in
`internal/cli/stop.go` sends the token it was given, falling back to `ATRIUM_SHUTDOWN_TOKEN` from the
environment. The detached restarter in `internal/cli/control.go` calls it with an empty token, and a detached
process does not inherit the environment of the session that spawned it, so there is no token to fall back to.

What that produces is worse than a failure, because nothing reports it:

1. `runRestart` ignores the error, by design: a daemon already gone is not a failure.
2. It then polls `/v1/health` until it stops answering. It never stops answering, so this burns the full
   twenty second grace.
3. `swapAllStaged` runs anyway, so `atrium.next.exe` is moved into place. The tree now looks like a
   successful install.
4. It starts a daemon which cannot bind the port and exits immediately.

The old daemon is still running the old binary, `atrium_status` says a daemon is up, the staged file is gone,
and every visible signal says the restart worked. Two consecutive restarts landed here.

- **The fix is the token.** `restart_atrium` runs inside the MCP server, which is a child of the claude
  session and can read the token the daemon was started with the same way anything else does. Pass it on the
  spawned command line, or teach the daemon to accept a stop from loopback without one.
- **And the restarter must not step past a refused stop.** A stop that was refused is not a daemon that has
  gone away, and continuing to the swap is what turned a clear 403 into a silent no-op. It should stop there
  and say so.
- **`atrium_status` should be able to answer "is the running daemon the installed binary".** It reports
  `running: true` and where. It cannot say the process was started from a file that has since been replaced,
  which is the exact question after a restart.
