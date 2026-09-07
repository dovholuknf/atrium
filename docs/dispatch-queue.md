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
