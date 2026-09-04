# Backlog

What is outstanding, why it matters, and what is out of scope. Ordered roughly by value.

Kept here rather than in a conversation, because a list that lives in a chat dies with it.

## Where things stand

The daemon is what gets used. It has durable state, a permission gate with standing rules, a web board, pseudo
terminals it owns and can attach to from the browser, live activity per card, auto mode with a review, launching
with a directory picker, a message channel into a running session, and a wind-down that is not a kill.

`atrium hub` and `atrium serve` are untouched and still work.

The gaps below are ordered by what would change day to day.

## Next

### The grouping expression is safe today for one reason, and that reason is not written down anywhere

Grouping already takes two functions from the operator, compiled with `new Function` in `compiled()`:

```
  group: (task) => string
  order: (a, b) => number
```

This is not a proposal. It exists and it works. The entry is here because the thing keeping it safe is
incidental rather than decided, and the decision has to be made before anything moves it.

**What makes it safe is that `groupingPrefs()` reads `localStorage` and nothing else.** The code that runs in a
browser was typed into that same browser, by whoever was sitting at it, and somebody who can write it can
already open dev tools. The comment above `compiled()` says exactly this and it is correct as far as it goes.

**What it does not say is what changes if that storage moves.** Three things would make this dangerous, and two
of them are already on this list:

- **Storing the expression daemon-side.** It becomes something one machine typed and another machine runs,
  which is the shape of a stored XSS. Reasonable to want: grouping is a board-wide preference and today it is
  lost when you open a different browser.
- **Federation.** `docs/federation-design-v2.md` puts many machines behind one board. A grouping function
  shipped from a leaf and run in the forum's browser crosses a trust boundary that does not exist today.
- **What the function can already reach.** These run with full page scope, so `fetch` is in hand. The board can
  read the daemon's filesystem through `/v1/browse`, and `why`, tags, worktree paths and the audit log all pass
  through it. A grouping function is a general-purpose exfiltration primitive the moment somebody other than
  the operator can supply one.

So the rule to hold: **an expression may be stored where it was typed. Anything wider needs a different
mechanism, not a bigger text box.** If board-wide or federated grouping is wanted, the options in order:

1. **A restricted expression language.** A small evaluator over a fixed grammar (field, comparison, and or not,
   a few string functions) covers nearly every real grouping and cannot express a network call at all. Safe by
   construction rather than by enumeration. Most work.
2. **A worker with no network.** A `Worker` from a blob URL, `connect-src 'none'`, card data structured-cloned
   in and a string out, with a timeout. Cheaper, and it rests on getting a CSP exactly right.
3. **A fixed menu of groupings for the shared case,** with free expressions staying local-only. No evaluation
   crosses a machine, and the local box keeps working as it does now.

### Enrolling a ziti identity with OIDC

An identity comes from a one-time enrollment JWT today, which somebody has to be issued and then paste. The
question is whether a human could instead authenticate to the controller with OIDC and have the identity fall
out of that.

It half fits, which is why it is worth writing down rather than deciding in a sentence. OpenZiti controllers do
support OIDC for AUTHENTICATION, and the ziti CLI can log in that way. But an identity is a keypair and a
certificate, and enrollment is what produces them. Those are two different things: logging in proves who you
are to a controller for the length of a session, and enrolling gives this machine a lasting identity on the
network. An OIDC login could plausibly be used to obtain an enrollment token without anybody emailing one
around, which is the part that actually hurts today, but that is a controller-side capability and atrium cannot
invent it.

So the work splits:

- **Find out what actually exists.** Whether a controller can issue an enrollment token to an OIDC-authenticated
  caller, and whether `sdk-golang` exposes it. This is a research task and it decides the rest.
- **If it does:** a button that opens the browser, completes the flow, and comes back with a token atrium then
  enrolls with. The existing enrollment path is unchanged and this only replaces the paste.
- **If it does not:** say so in `docs/overlays.md` and stop. Atrium holding an OIDC session in order to act as a
  network administrator would put it on the wrong side of the line in `CLAUDE.md`.

The line stays the same either way: atrium may start the flow and report what came back, and it never holds an
identity, proxies traffic, or decides who may connect.

### Everything that has ever run here. Done, with one thing still open.

A `history` tab: every card ever created, on the board or not, searchable, cut by whether it was written up. The
recap cut is real now that `atrium finish` exists; before that it would have been an empty column.

Of the three open questions, two have answers:

- **How far back** is now a setting, under `housekeeping`. What to put in it is still a judgement nobody has
  made, and the entry above says why it should not be a default.
- **Whether an archived card can come back** turned out not to need deciding. The view shows archived and live
  cards together, so there is nothing an unarchive would let you see that you cannot see already.

Still open, and it is the interesting one:

- **Where a recap lives.** There are now two things called a recap. The `/recap` skill writes markdown into a
  history root outside atrium, and `atrium finish` writes two or three sentences onto a card. Those are
  different artifacts serving different purposes, and whether one should feed the other is not decided. The
  options are unchanged: a path on the card, a scan of that root matched by directory and date, or the skill
  posting to atrium when it writes. The last is still the only one that cannot silently drift, and still the one
  that needs the skill changed.

### Actions on a card, written by you. Done.

A named prompt, offered on every card, delivered through the message queue, with `and exit` sending the
harness's own exit keys afterwards. Three are seeded, including **write it up and finish**, which is what makes
`atrium finish` reachable without a session having to be told it exists.

The three open questions have answers now:

- **Stored daemon side**, so they are the same on every board. That makes them the first operator-authored
  content atrium keeps and hands back, and the answer to what that costs is that an action is TEXT delivered to
  a runner while a grouping expression is CODE evaluated in a browser. Different risk entirely.
- **Scoped by tag or by runner**, both, because they answer different questions. Both empty offers it
  everywhere, which is the common case.
- **`and exit` is best effort and says so.** A session atrium does not own gets the prompt and a note saying
  nothing can make it quit. Terminating instead would be a different promise and remains the wrong one.

What is left, and it is the weak part: **there is no signal that a runner has accepted a line.** The exit keys
go into the same terminal as the prompt, so they are sent after a fixed pause, and a runner that is slow to
submit could in principle receive a quit before its instruction. A shorter pause risks that; a longer one makes
the button feel broken. The real fix would be a runner that acknowledges input, which none of them do.

### The settings dialog is one long scroll and needs a spine

Everything is in one column in the order it was added. Three `h3` headings exist (`reach this board from
elsewhere`, `sound`, `the board`) and they are the only structure, so finding a setting means scrolling past
every other setting and knowing roughly how old it is.

What is in there now, which is the argument on its own: two overlays with their own setup blocks and accordions,
volume, two tone pickers, desktop notification permission, notification expiry, alert debounce, the
confirmations you turned off, text size, and the grouping expression with its two code boxes. The grouping boxes
are the worst of it: they are the tallest thing in the dialog and the least often changed.

A left-hand nav with a pane per group. Roughly:

- **Reaching it** - the two overlays, which already fold themselves.
- **Alerts** - volume, tones, desktop permission, expiry, debounce. Nearly half the dialog and one topic.
- **The board** - text size, grouping, and whatever display settings arrive next.
- **This machine** - the atrium name, the hooks, the runners. Some of that lives in other tabs today and may or
  may not want to move.

Two things to decide rather than assume:

- **Whether it should still be a modal.** A dialog with a nav inside it is most of a settings page, and a
  settings page is a view. The board already has five tabs and a sixth is cheap. Against that: settings is
  where you go from wherever you are and come straight back, and a modal returns you there for free.
- **Where per-card settings live.** A card's bell and its theme are on the card, which is right, and neither
  belongs in here. The line is "settings for the machine" against "settings for one piece of work", and it is
  worth writing down before the next setting has to be placed.

The overlay accordions are the pattern to reuse rather than invent past: state visible while collapsed,
configuration behind a fold.

### NetFoundry front door as a third overlay

Named as a want alongside the zrok and OpenZiti work and not specified. It needs its own entry once the shape
is known: whether it is a distinct overlay beside the other two, or a way of configuring one of them.

Everything around it is now in place to make that cheap. `OVERLAY_UI` is a table of fields per overlay, the
card renders from it, and a field can carry an action button, so a third entry is an entry rather than a branch
through the rendering.

The four items that used to sit here are done: the configuration is an accordion, zrok can be pointed at
another instance, the sign-up link is on the setup block, and the "SDK or binary as a toggle" item turned out
to be the wrong shape. There is no toggle to build, because there is no choice: sharing is ALWAYS the embedded
SDK for both overlays, and the executable is used only by `zrok enable` and `zrok disable`. What that item was
really pointing at was a lie in the interface, which is fixed: a machine that is already enabled now shares
with no executable anywhere instead of reporting the whole overlay as "not installed".

### The overlay lifecycle stops short of setting anything up

Atrium can drive a zrok share or a ziti tunneler you already configured. It cannot get you to that point, and
that is the half a human actually needs. See `docs/overlays.md` for what exists.

Most of this is now built. What remains:

- **No service, config or policy creation for OpenZiti.** This one is correct to leave out: a network somebody
  administers is not one a board should be editing. Listed so nobody adds it thinking it was an oversight.
- **Enabling zrok still runs the executable.** `zrok enable` and `zrok disable` are shelled out, and they are
  the only two things that are. Sharing is entirely the embedded SDK. Whether enabling has an SDK equivalent
  worth using has not been checked, and until it is, a machine with no zrok on its PATH can share but cannot
  be set up or torn down from the board. The board says so rather than hiding the overlay.

What got built, and one thing that was wrong for a long time:

- **`sdk.ShareRequest.Reserved` is read by nothing in zrok.** Atrium set it and it did nothing. Reserving in v2
  moved onto the NAME: `create name`, then `modify name --reserved`, and `controller/unshare.go` keeps a
  reserved name when the share is unshared and deletes an ephemeral one. So the share must still be released on
  stop, which is what frees the ziti resources, and the name is what survives. A private share reaches the same
  place differently: its token is requested rather than owned, so releasing it puts the token back on the shelf
  for the next start to ask for again.
- Reserving a name from the board, both steps behind one button, since a name that exists but is ephemeral
  fails identically to one that was never created.
- What a ziti identity may bind, asked of the controller. A service that does not exist and one this identity
  may only dial both come back from the listener as a refusal, and this is the only way to tell them apart
  without pressing start. Bindable services are clickable.
- Pointing zrok at another instance, written before enabling rather than after, because enabling talks to
  whatever it names.
- The configuration folded into an accordion, open while there is something to do in it.

The shape that fits the rest of atrium: report the state honestly, offer the next command, never invent one. The
runner discovery already does this and is the model.

### Notes on a card, and sending one to the agent when you are ready

Two things that look like one. A note is for you: a place to write down what you want next while the agent is
still working, so it is not held in your head or in a scratch file. Sending it is the second, separate act.

The reason to split them is that the queue already exists and does something else. `POST
/v1/tasks/{id}/message` reaches a session through its next tool call or its Stop hook, and it fires as soon as
there is something to deliver. A note is the opposite: written now, sent when you say.

Claude Code takes input while it is thinking, so the send is less urgent than it once was. What it buys is the
ordering: three notes queued during a long turn, sent as one instruction at the end, rather than three
interruptions in the middle.

### Popping a terminal out, and putting it back

Every supervised terminal lives inside the board's terminal view, which means finding a session is a click into
an app and then a click onto a tab. The habit it competes with is alt-tab, which is faster and which nothing
here can beat while a session is a pane inside a page.

The ask is a window per session and a lifecycle around it: pop out, put back, shelve, and whatever else belongs
in that set. The last part is the honest one. Atrium has `shelve`, `unshelve`, `stop`, `kill` and `attach`, and
they were each added when they were needed rather than designed as a set, so what "all that sort of lifecycle
stuff" should contain is not yet decided.

Two ways to do the window, and the difference matters more than it looks:

- **`window.open` on the attach URL.** A browser window per session, which alt-tab does reach. Cheap: the
  attach WebSocket already carries one terminal, so this is a page holding one pane and a name in its title
  bar. What it gets wrong is that a browser window is not a terminal window. It has browser chrome, it is in
  the browser's alt-tab group rather than beside a real terminal, and closing it has to detach without
  stopping anything.
- **Handing the session to a real terminal.** Windows Terminal opening on the same conversation, which is what
  alt-tab actually wants. This is the harder one and it collides with a known limit: atrium owns the pseudo
  terminal, closing a pty takes the process with it, and there is no reattach on Windows
  (`docs/supervision-design.md`). Handing over would mean not owning it, which means giving up the attach, the
  activity badge and the stop.

The second one is the real want and the first is what is buildable today. Worth deciding which is being asked
for before building either, because a browser window that pretends to be a terminal window is the kind of thing
that gets used once.

Related: the detached pty holder in `docs/charon.md` answers the reattach half on POSIX and does nothing for
Windows, which is the machine this matters on.

### Moving files, and pasting into a session. Built.

`docs/file-transfer-design.md`, steps 1 to 4. Paste and drop on the terminal pane, upload with a computed
destination, download bounded by `internal/safepath`, which is the containment primitive that did not exist
and which the design was mostly about discovering the absence of.

What is left:

- **The write precondition.** Step 5. There is no endpoint yet that writes to a caller-named path, so it has
  nothing to guard. It goes in with the first one, and the design records the one correction to make to
  Charon's version: an absent hash should be an error, not consent.
- **`browse.go` is still unbounded.** The primitive exists now and `browse.go` does not use it. That was
  deliberate: tightening a picker people already use is its own argument with its own answer, and doing it in
  the same change as a new feature would have hidden it. See "Known gaps".
- **Nothing outside a card.** Upload and download are per card and rooted at that card's worktree. There is no
  way to ask atrium for a path that is not below a card, and adding one would build the thing `browse.go`
  accidentally is.

### What Charon does that atrium does not

`github.com/Lomchat/charon`, Apache 2.0, single user and self hosted. Six months of one person's daily use, and
its author says so plainly along with the rough edges. Worth reading rather than dismissing: the overlap is
large and the differences are the interesting part.

**The source has now been read. `docs/charon.md` is the standing reference and it corrects nine claims this
section originally made from the author's public post.** The two that change what to build: Charon is a Python
agent driving the Claude Agent SDK in process rather than a supervisor of an interactive CLI, which is why its
peer messaging can inject text as though it were typed and atrium's cannot. And its editor is CodeMirror 6, not
Monaco.

Ranked by what is worth building here, with the mechanism and the file references in `docs/charon.md`:

1. **A peer bus: sessions that can address each other.** A stable handle, a `list` tool as mandatory discovery,
   a `send` that returns acceptance rather than an answer, and status tools so a model asks instead of guessing.
   Atrium already has the handle (`wire_name`), the transport (the agent listener), and the delivery mechanism
   (the `message` table drained by the permission and Stop hooks). This is the one thing `CLAUDE.md` says atrium
   has no answer for at all.
2. **Approvals reachable from a phone.** Still the gap that matters most: a gate you cannot answer from away is
   a gate you turn off. Charon's version is small because it is one more client of the same call the browser
   makes, and atrium's `/v1/permissions` endpoints already have that shape. Atrium's own notifications need the
   board open, since the board registers a service worker but never subscribes to push.
3. **A detached process holding the pty, so a runner outlives the daemon.** Answers open question 2 in
   `docs/supervision-design.md` for POSIX hosts. It does not close the Windows gap, which stays on resume ids.
4. **A precondition on any write endpoint.** An expected content hash treated as a precondition rather than a
   hint, so a save cannot silently clobber an agent mid-turn. Worth adding to whatever write path appears first,
   with or without an editor.
5. **One upload pipeline for drag, paste and picker,** splicing the resulting path into the prompt. See "Moving
   files, and pasting into a session" above, which this answers.
6. **A dedup key on `POST /v1/tasks/{id}/prompt`.** `permission.dedup_key` exists for the same reason and the
   prompt path does not have one.

Where atrium is ahead, and should stay:

- **It drives an overlay natively.** zrok and OpenZiti are embedded SDKs, and the board answers on the overlay
  listener itself rather than being proxied to. Charon reaches its machines over SSH, which works and needs no
  new ports, but it needs an `sshd` already accepting connections, so it is a tunnel to a box rather than a
  service on a network with a policy in front of it. `docs/overlays.md`.
- **Standing rules and a durable audit log.** Charon has one rule mechanism, a per-session list of bare tool
  names, so one "Always" on a `Bash` card approves every later `Bash` command in that session. Atrium matches by
  prefix, glob or folder, most specific wins, and records what answered each decision. Their own comment says
  they moved that list onto disk for the same reason `perm_rule` exists.
- **A card outlives its process.** Not durability, which Charon has: atrium has a unit above a session and a
  status a human curates.
- **Pending approvals do not expire.** Charon has three timeouts, 10 minutes for a Claude tool permission and
  30 for Claude's `AskUserQuestion` and for Codex, and each expiry returns a deny the model cannot tell from a
  human's. Atrium blocks until answered, which is the correct default for something whose answer is a decision.
- **Auto mode is not a bypass.** Charon's `auto` skips the hook entirely, and the model can put itself there by
  exiting plan mode. Atrium's sits last in the chain and pays for itself with a review. `docs/auto-mode.md`.

Refused outright, with the reasons in `docs/charon.md` section 6: approval timeouts, the hub-dials-out
federation transport, a second durable event store on the agent side, holding provider credentials, and any of
the authentication.

### An editor in the board

Charon (`github.com/Lomchat/charon`) puts an editor next to its agents, and that is a better idea than it first
sounds here. Atrium already streams a terminal for a runner it owns, and the gap between watching an agent edit
a file and reading that file is a context switch to another window.

Not evaluated. This section used to guess Monaco. Charon shipped **CodeMirror 6**, which is lighter, loads its
language modes lazily per file, and vendors as static assets the same way xterm.js already does, so it is the
thing to evaluate first. `docs/charon.md`.

Two of the three open questions here now have answers worth starting from. What writes back: an endpoint with an
expected-content-hash precondition, which is worth having whether or not an editor follows. Whether a save races
the agent editing the same file: yes, and optimistic concurrency answers it without a lock, refusing the write
and handing back the current hash rather than arbitrating who owns the file. Still open: whether a read-only
view answers most of the want.

### Every node needs its own configuration

Found while answering how a tenant id gets set. Once there is more than one atrium, the gear stops being
"settings" and becomes "settings for which node". Hooks, rules, runners and overlays are all per machine, and
a satellite is exactly the machine somebody is least likely to have a terminal open on.

So the forum needs an inventory view, not just a card list: which nodes exist, which are reachable, and the
existing configuration screens reachable per node. That is a bigger claim on the board's shape than the routing
is, and it belongs in the forum work rather than bolted on afterwards.

### The forum: one board, many machines

Designed and planned, not started. `docs/federation-design-v2.md` for why it is shaped the way it is, and
`docs/forum-implementation.md` for the stages.

The shape: leaves dial OUT to a forum, which holds nothing. A leaf's whole board is already one `http.Handler`
(`internal/daemon/daemon.go:478`), so serving it on a connection the leaf dialled is `http.Serve` on that
connection, and the forum forwards. Leaves keep owning every card, so there is no second copy of anything and
no namespace atrium did not issue. Dialling out is what makes a container or a pod work with no port map.

Stage one is a day: a forum that answers `GET /peers`, a daemon that dials it and reconnects, and nothing
forwarded yet. Its acceptance test is killing the forum and watching both leaves carry on.

Three things found while planning it that are worth knowing before anyone starts:

- **A dialled connection reports the forum's address as `RemoteAddr`.** With a forum on the same machine, every
  forwarded request presents as `127.0.0.1` to `isLoopback` in `internal/daemon/shutdown.go`. That is the
  ordinary development setup, which makes the existing `sharing()` gate load-bearing rather than tidy, and it
  needs a second wording because "a share is running" would be false.
- **A browser caps HTTP/1.1 at six connections per origin.** One `EventSource` per leaf means the board stops
  working at six of them, so the nudge stream has to be merged at the forum rather than opened per leaf.
- **Alerting keys are id sets, and ids are minted per leaf.** `knownPerms` and `knownWaiting` in the board would
  silently swallow a second machine's request on a collision, and nothing appearing is exactly what a working
  board looks like. Every key has to become peer plus id.

### The unexplained block was not a bug. Closed.

Kept because the wrong conclusion is instructive.

A tool call was refused with the shelved reason, and the first investigation reported that the card had never
been shelved, that nothing was in the decision log, and that zero blocks existed across 1193 decisions. All
three were wrong, and all three came from the same mistake: querying the events endpoint with a limit smaller
than the card's history and reading an empty result as an absence.

Queried properly, the permission table has three blocks with `decided_by = shelved` and the card has two
`status-changed` events into `shelved`. The gate worked, the block was correct, and it was recorded.

Two things came out of chasing it, and both were real:

- The replay path returned a decision without writing an event. Fixed, and it now records `replay` as what
  answered.
- The dedup key identifies a COMMAND rather than an ATTEMPT, so a decision could replay indefinitely. Bounded
  to a two minute window. See the changelog entry.

The lesson worth keeping: on a board where a card can carry thousands of events, an absence proves nothing
until the query is known to have covered the range.

### Small interface debts

- **A folder rule reads the command text, not what a shell would make of it.** An absolute path that only
  exists after expansion, `$HOME/x` or `$(cat somewhere)`, is not seen, so a command reaching outside that way
  is approved when the session sits inside an allowed folder. A literal `cd /elsewhere && rm x` IS caught,
  because `/elsewhere` is visible in the text. Closing the rest means expanding shell syntax, which is its own
  project and a source of new mistakes. Worth knowing before allowing a folder that matters.
- ~~**Auto mode has no time limit.**~~ Done. Both switches take one, the button asks how long, and the time left
  is on the switch. Nothing enforces it: the deadline is read when the chain runs, because a timer that has to
  fire is a timer that does not fire across a restart. `docs/auto-mode.md`.

### An agent can say it finished now. Mostly.

`atrium finish [recap]` moves the card to `done` and records what the session says it did. `--hand-back` puts it
in `ready` instead, which is a different claim and worth being able to make.

**A command rather than a tool, and that was the decision.** The v2 design named `submit(kind="task-complete")`
for this. A command is better here than anywhere else in atrium, because a command is the one channel every
runner already has: an agent that can run `ls` can run this, with no MCP server, no tool description and no
cooperation from the harness. It works for codex and for a bare shell, not only for the runner with a tool
surface.

What is left, and it is the part that decides whether this gets used:

- **Nothing tells an agent to call it.** It exists and is documented and no session knows. The honest options
  are a line in the operator's `CLAUDE.md`, a skill, or a card action that sends "write yourself up and finish"
  as a prompt. The third is the best of the three because it needs no cooperation from the session at all, and
  it is the strongest argument for building card actions.
- **A recap has nowhere else to live.** The `/recap` skill writes markdown into a history root outside atrium,
  and this writes two or three sentences onto a card. Those are different artifacts with the same name, and
  whether one should feed the other is not decided. See "Everything that has ever run here".

### Hooks for runners that are not Claude Code

The hook wiring is Claude Code's shape: `settings.json`, and the event names in
`internal/claudeconf/hooks.go`. Codex keeps its configuration in TOML somewhere else, and its events are its own.

Nothing about the daemon side is claude-specific. `/activity` and `/session` take an agent name and an event, so
a second harness needs a writer next to `claudeconf` and its own entry in the wanted list, not a new endpoint.

**The Claude Code side is now as wired as it can be without a live probe.** `PostToolUseFailure`, `PreCompact`
and `Notification` are in, the last one filtering itself. `SessionEnd` reads its reason. What is left is one
item and it is a real blocker rather than a gap:

- **Moving the gate from `PreToolUse` to `PermissionRequest`.** `docs/hook-coverage-spike.md` recommends it and
  the argument is strong: it fires only when a decision is actually needed, which retires the hardcoded skip
  list and the 134 imported rules that exist to buy back the silence the harness already provides. It also
  carries `tool_use_id`, and the payload keeps `tool_input`, so the diff survives.

  It is not built because the spike says not to build it from the document. Two readings of the reference gave
  two different output shapes for what the hook prints, flat and nested, and the correct one has to be found by
  emitting a probe hook that dumps its stdin against a live session. That is a thing to do at a keyboard, not
  from a summary. The staging the spike proposes is right: wire it alongside the existing gate and log both for
  a day, confirm the diff still renders for `Edit`, `Write` and `MultiEdit`, then decide edit-then-approve
  explicitly, since `PermissionRequest` has no `updatedInput` and leaving a dead field on the board is the one
  dishonest option.

Also unverified and named for completeness: `Setup`, `UserPromptExpansion`, `PermissionDenied`, `PostToolBatch`
and `StopFailure`. The spike argues against `PermissionDenied` and `MessageDisplay` outright, is undecided on
`PostToolBatch` until somebody finds out whether it replaces or supplements the per-call events, and thinks
`StopFailure` is worth its own look because `error_type` separates "wants you" from "wants time".

### Governed calls from sterling

The strongest of the four integrations examined in `docs/ai-platform-fit.md`. Sterling's signed recipes classify
every tool method `auto`, `prompt` or `deny`, and today `prompt` can only reach a human at a terminal, so
unattended it fails closed and the only way through flips every prompt to auto at once.

Atrium is a human that is not a terminal, and the wire shapes already match. Two conditions the fit depends on
and neither is optional: atrium has to fail CLOSED for that caller, which inverts a documented guarantee and
belongs in the resilience section rather than being smuggled in, and standing rules and auto mode have to be
skipped for a governed call, or an unsigned database answers on behalf of a human that a signed artifact
deliberately deferred to.

### A runner atrium can ask for help

Atrium knows things a model could act on: what a session did, what a rule would cover, why a launch failed.
Right now every one of those ends in the operator reading it.

A configured helper runner, which can be claude, codex, ollama or anything else, gives the board actions like
"summarise what this session changed" and "explain why this failed". Runner agnostic like everything else: the
harness table already describes how to start one, so this is a setting naming which row to use for it.

Not the same as a launched runner. A helper answers one question and exits; it does not get a card.

### Working directories from a repo URL

Launching asks for a directory that has to already exist.

**The inversion is better than building it in.** Atrium does not need to learn git worktree semantics. Whatever
already creates worktrees can make the directory the way it likes and then hand it over, which `atrium launch`
now does:

```powershell
atrium launch --cwd $worktree --title $branch --why "what this is for"
```

It prints the card id, so the caller can hold on to it. A `-WithAtrium` flag on an existing worktree workflow is
the whole integration.

What is left is the other direction: atrium creating a worktree itself, for the case where there is no script to
call it. Lower value now that the hand-off works.

### Pruning on a timer. Done.

Two settings in the gear under `housekeeping`, side by side because the difference between them is the point.
Sweeping archives and pruning deletes, so one is on by default and the other is off until you turn it on, with
what it destroys spelled out and an hour floor under it. `done` and `dead` only: shelved is refused by the store
whatever it is asked, and the inbox is left alone because an offered item nobody started is still work somebody
found.

What is still open, from "Everything that has ever run here": **how far back**. The setting exists now and the
answer to what to put in it is a deliberate judgement about how long the record is worth keeping, which is not
something a default can make.

### Status inference for runners that cannot speak

The last piece of supervision. A cooperative runner reports its own state, so atrium never guesses at its output.
A bare shell or a runner with no hook has only its terminal, and inferring `needs-input` from that is heuristic.
Worth doing last, and worth keeping manual status override as the escape hatch.

### Starting a card from a ticket, an issue or a pull request

Designed in `docs/intake-design.md`. **Layers 0, 1 and 2 are built.** What follows is the original entry, kept
because the layers it describes are still the shape, with the state of each marked.

- **Layer 0, the hand-off. DONE.** `--tags`, `--prompt`, `--source`, `--external` and `--item-url` on
  `atrium launch`. A `prompt_args` column on the harness, next to `resume_args`, because how a runner takes an
  opening instruction is per runner. A `-WithAtrium` switch on the gwt verbs is still not written, and is now
  the whole remaining integration.
- **Layer 1, the inbox. DONE.** `POST /v1/intake`, an `offered` lane, a start button that claims the card rather
  than making a second one. It uses `backlog` rather than a new status, and the reason is worth reading: see the
  correction in `docs/intake-design.md`.
- **Layer 2, sources on a timer. DONE.** A `source` table, a scheduler, bounded output, failure reporting on the
  row, and `run it now`. `scripts/sources/` has working examples.
- **Layer 3, mcp-gateway as a Go dependency. NOT DONE and still refused** on the grounds the document gives. The
  place the gateway belongs is behind a layer 2 source command, which needs nothing from atrium.

What is left, in order of value: the `-WithAtrium` switch on gwt, a CI failure source, and
`gwt sessions audit` pointed at the board, which needs no external system at all.

Two things found while building it, both recorded rather than fixed:

- **`offered` cards are not prunable yet.** The design says an item nobody started for a month is not work, and
  `PrunableStatuses` is still `done` and `dead`. Adding `backlog` to it would make the existing `clear` button
  delete unstarted work, which is not what anybody pressing it means. It belongs with the pruning timer.
- **A source has no way to say an item stopped being work.** An issue that gets closed by somebody else stays in
  the inbox forever. Polling ticket state backwards is refused below, and the honest alternative is that a
  source could report what it can see and atrium could archive offered items that stopped appearing. That is a
  real design question and it is not answered.

Researched and written up in `docs/intake-design.md`.

The ask was source control and ticketing integration, so a card can be started from GitHub, GitLab, Zendesk, Jira
or Discourse. The `gwt` script already does this and has seven verbs for it: `issue`, `pr`, `advisory`, `ghsa`,
`backport`, `discourse` and `zendesk`, each turning an identifier into a worktree, a branch and a seeded claude
session. It has no atrium integration at all, so the ledger it writes and the board are two trackers of the same
sessions that never meet.

The thing worth copying from it is how little it knows about each system. GitHub is read through `gh`, best
effort, and a failed fetch costs a nicer prompt and nothing else. Zendesk is not read at all: the verb builds a
URL, puts it in the prompt, and the agent's own MCP tools do the reading four seconds later.

Four layers, cheapest first:

- **Finish the hand-off.** `--tags`, `--prompt` and `--external` on `atrium launch`, then a `-WithAtrium` switch
  on the gwt verbs. A day, and every layer below needs it anyway. It adds no new entry point, which is the honest
  argument against it.
- **An inbox atrium owns and does not fill.** `POST /v1/intake` taking a normalized item, an `offered` status for
  a card with no runner, and a start button. Atrium never learns what `source` means. Two days.
- **A source is a command on a timer.** Shaped like the `harness` table: argv, an interval, stdout parsed as
  intake items. `gh` keeps the token, atrium keeps the argv. Three to four days.
- **mcp-gateway as the reader.** It has Zendesk and Discourse wired already. Solves fetching, which was never the
  hard part. Better used as a source command than as a Go dependency.

Named and refused, each against something already written down: no provider credential in the database, no
inbound webhook (it needs a public address and the share already broke what loopback means), no OAuth client, no
`ticket` table. Atrium may hold the name of a command that has a credential, and never the credential.

The reverse direction splits three ways. A link on the card is free and worth having. Ticket state on the board is
polling backwards and would sit next to the status column disagreeing with it. Posting back belongs to the agent,
which has the tools and the context, and the message channel already reaches it. The useful half depends on "an
agent cannot say it finished" above.

Also in this category and not named in the ask: CI failures (highest value, and gwt explicitly refuses an Actions
URL today), PR review requests, advisories and backports, a TODO scan, unanswered Discourse topics, alerting
(a trap: a page on an hourly board is a missed page), calendar and email (worst, because neither yields a
worktree), and `gwt sessions audit` pointed at the board, which needs no external system and is an afternoon.

## Parked

Real, understood, and not wanted yet.

### atrium on PATH

The `atrium-join` and `atrium-leave` skills call `atrium` by name and treat "not found" as the answer, so both
are inert. Hardcoding a checkout path into a skill would work on one machine and rot the first time it moves.

Parked because launching from the board covers the same ground: a runner atrium starts is already on the board
and already gated, which is what joining was for.

## Known gaps

- **Stage 5 was skipped.** The TUI still receives a `*Hub` pointer in process rather than going through the HTTP
  API. That was the stage meant to prove the API is complete, so until it is done the API has exactly one client
  and its gaps are invisible.
- **The board is a plain page**, not the React app the decisions table names. It speaks the same JSON and SSE
  contract, so this is a client side swap whenever it is worth doing.
- **The dedup key names a command, not an attempt.** The permission hook hashes the session, the tool and the
  command, which is all it has: Claude Code's `PreToolUse` payload carries no per-call identifier the hook
  reads today. A two minute replay window makes that safe, but the window is a bound on a wrong key rather
  than a right one. If the payload does carry a tool use id, using it would make the key exact and the window
  unnecessary. Unverified, and worth checking next time the hook events are looked at.
- **A session that dies without warning is now handled three ways, and one gap is left.** A known pid gets the
  liveness check. No pid and three hours of silence gets the quiet check. No pid while waiting to be answered
  gets the orphan check, which asks the hub whether anybody is still parked on the request. What is left: a
  card in `needs-input` with no pid and no pending request. Nothing is waiting on an answer there, so there is
  no third fact to consult, and silence cannot be the signal because needing input IS silence. It would take a
  session-level heartbeat, which is a cost on every session to catch a rare case, so it is not obviously worth
  paying.
- **Postgres portability is asserted, not tested.** The schema is written for it. Nothing runs the migrations
  against it. A CI job would settle it.
- **`docs/test-plan.md` predates v2.** It covers the hub and the agent loop, not the daemon, the board, rules,
  launching, supervision or the permission diff.
- **Repo metadata is unset.** `gh repo edit` returns 403 with the current token, so the description and topics on
  the GitHub page are still empty. Needs `gh auth refresh -s repo` or setting them in the web UI.
- **A share widens what loopback means.** A tunneler terminates on this machine, so while a share is up every
  request presents as `127.0.0.1`. The shutdown endpoint now notices and demands its token, but that is one
  endpoint. Anything else that ever decides by source address has the same problem, and there is no general
  answer here, only the rule: do not publish the agent listener on `:7777`.
- **A share also publishes `GET /v1/browse`, which is unbounded.** Found while designing file transfer. The
  handler applies `filepath.Clean` to whatever it is given and has no root, no allow list and no symlink
  resolution, so every directory the daemon's user can read is listable. On loopback that is a directory picker
  and it is fine. Over a share it is an unauthenticated recursive directory listing of the machine. Nothing
  reads file contents through it, so this enumerates rather than discloses, which is why it is written down
  rather than treated as an incident. `docs/file-transfer-design.md` builds the containment primitive that
  would let this be narrowed, and deliberately does not narrow it in the same change: tightening a picker
  people already use is its own argument.
- **`wire_name` collisions are solved but not yet enforced.** `atrium name` prefixes every session an atrium
  registers, so two machines cannot claim each other's cards. Nothing makes a satellite set one, though, and an
  unnamed atrium still registers bare names. The forum handshake is the place to require it, since that is the
  first moment a second atrium exists. See `docs/forum-implementation.md`.
- **Supervised runners die with the daemon.** The daemon owns each pseudo terminal, and on Windows closing one
  takes the attached process with it. There is no reattach. So stopping the daemon ends every runner it started,
  and a runner cannot outlive a restart. Resume ids are the answer rather than orphan survival, which ConPTY does
  not offer.
- **The daemon can silently open an empty database.** `WORKTREE_ROOT` unset means it falls back to `~/.atrium`,
  which looks exactly like every rule and card having vanished. It should say loudly when it creates a database
  rather than opening one.

## Waiting on something external

- **Codex and ollama ship disabled.** Both have now been launched on this machine, so the invocations are known,
  but the seeded rows stay off: a fresh database should not offer a runner that is not installed. Discovery
  reports what is actually on PATH at startup, which is the signal to turn one on.
- **Rule import for anything but Claude Code.** Claude Code's `settings.json` is understood. No other harness has
  a permission config whose location and shape are known, so the generic path is atrium's own JSON export.
- **Notification buttons cap at two.** Chrome on Windows renders at most two actions, so it is approve and block.
  There is no inline text reply on desktop, which is a browser limitation rather than something to work around.

## Out of scope

- Authentication. Single machine, loopback. Reaching it from elsewhere is a job for an overlay such as OpenZiti.
- Multi tenancy, accounts, a deployment story. This is one person's tool.
- Adopting sessions from a worktree ledger. Tried and removed. It filled the board with sessions atrium could
  watch but never talk to. See the abandoned section in `docs/architecture-v2.md`.
- Replacing the runner. claude-code owns the tool loop, context and credentials. Atrium supervises, it does not
  become an agent harness.
- Attaching to a session atrium did not start. A console cannot be handed to another process after the fact, so a
  joined session gets gating, a card and history, and never a terminal. Only a runner launched under a pty is
  attachable.

## Done

Kept short, because the point of the list is what is left. Recorded so the same ground is not re-covered.

- Supervision: ConPTY validated, pty spawn, output capture, browser attach over a WebSocket, terminate, and a
  shutdown that asks a runner to wind up before closing its terminal.
- Terminals as a view with a session switcher, copy and paste that does not fight the runner, a pending request
  shown inline while attached, and a notification that attaches rather than landing on the board.
- Join and leave, so a running session can put itself on the board without restarting.
- Shelving answers what a card was holding, and a shelved card is a standing no.
- Shelving stops the runner and unshelving starts the same conversation again, off the stored resume id. The card
  is the launch spec: `runner` is the harness, `worktree` is the directory. When it cannot resume it says which
  piece is missing rather than doing nothing.
- Live activity per card, held in memory and never written down.
- Auto mode, and the review that pays for it. Turning it on drains the queue it was turned on because of, since
  the chain only runs when a request arrives and anything already waiting had asked before the switch existed.
- Saying something to a session from the card, reporting which of the two routes it took and listing anything
  queued that has not arrived. The two are different promises and the box has to say which one it made.
- The Stop hook, as `atrium turn --event end`. The only way to reach a session sitting idle, since an idle
  session makes no tool calls for a message to ride. Offered by name and never installed by "install all",
  because it is the one hook whose answer changes what a session does.
- Folder rules, covering work inside a directory rather than a command shape.
- Stopping the daemon without killing it, and a launch that proves the runner started.
- A narrow layout, and the board served with no-store so a rebuild is always what is on screen.
- Every browser alert, confirm and prompt replaced with the app's own dialog, and no dialog that follows another
  dialog. The repeatable ones carry "do not ask me again", and the gear lists what that turned off and turns it
  back on. The ones that throw something away have no such tick.
- Grouping cards by project, on rules the operator writes: a function that names a card's group and one that
  orders the groups, both plain JavaScript in the browser, both defaulted so it works with nothing configured.
  A colour per group, hashed from its name, overridable. On the board and on the stack, off one setting, with an
  on/off control on both screens rather than only in the gear.
- The stack as the first tab: every card as one ordered list, sorted by activity, waiting, status, name, project
  or runner, with one axis per pill.
- Per-runner exit keys, so asking a runner to quit sends what that runner actually quits on.
- A prepare command per harness, so a shell function that puts a toolchain on PATH can reach a launched agent.
- Runner discovery against the daemon's own PATH at startup, reported in the log.
- Reaching the board from elsewhere, by driving zrok or OpenZiti rather than becoming either. `docs/overlays.md`.
- Global auto mode, board wide, recorded under its own name and kept across a restart.
- The daemon recording where it is listening, so the CLI finds a non-default port with no flag.
- The audit log as a table, with the command and what answered it on one line.
- The activity hooks as `atrium hook --event <name>`, and a board that reports which are registered and writes
  the missing ones into `settings.json`. What used to be a documentation page is a button.

## Review

- Mercurius round two ran against the revised design. Two findings were stale documentation, since fixed. The
  third, a card leaving a waiting state without answering its pending requests, was a real bug and is fixed.
- Mercurius round three ran against the code. Two folder-rule findings were real and security relevant: a command
  naming a path inside an allowed folder AND one outside it was approved, and a quoted Windows path with spaces
  was split into tokens that looked relative. Both fixed with regression tests. A third, `/activity` returning
  400 on a malformed body against its own fail-open contract, was also real and fixed.
- Nothing has been run against `docs/supervision-design.md` as built.
