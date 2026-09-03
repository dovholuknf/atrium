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

### The overlay lifecycle stops short of setting anything up

Atrium can drive a zrok share or a ziti tunneler you already configured. It cannot get you to that point, and
that is the half a human actually needs. See `docs/overlays.md` for what exists.

Missing for zrok, roughly in the order somebody hits them:

- No environment check. `zrok status` says whether this machine is enabled, and atrium never asks. Starting a
  share on an unenabled environment fails with zrok's own message rather than "you are not enabled yet".
- No path to getting enabled. The answer is `zrok invite`, then an emailed token, then `zrok enable <token>`.
  Atrium should recognise the state and walk it, rather than leaving somebody to find the docs.
- No `zrok reserve`. A reserved token is how an address survives a restart, and today it has to be created at a
  terminal and pasted in.

Missing for OpenZiti:

- No enrolment. An identity comes from a JWT, and `ziti edge enroll` turns one into the identity file the
  tunneler wants. Atrium takes the file and has nothing to say about where it comes from.
- No view of what an identity can bind. The tunneler hosts whatever the network's policies allow, and atrium
  shows none of it, so "is this going to work" is only answerable by starting it.
- No service, config or policy creation. This one is probably correct to leave out: a network somebody
  administers is not one a board should be editing.

The shape that fits the rest of atrium: report the state honestly, offer the next command, never invent one. The
runner discovery already does this and is the model.

### An editor in the board

Charon (`github.com/Lomchat/charon`) puts a VS Code editor next to its agents, and that is a better idea than
it first sounds here. Atrium already streams a terminal for a runner it owns, and the gap between watching an
agent edit a file and reading that file is a context switch to another window.

Not evaluated. The obvious candidate is the Monaco editor, which is what VS Code is built on and which vendors
as static assets, so it would fit the "vendored rather than from a CDN, because the board has to work offline"
rule the terminal already follows. Open questions before it is worth planning: what writes back, whether a save
races the agent editing the same file, and whether a read-only view answers most of the want.

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

### An unexplained block, with no audit trail

A tool call was refused with the shelved reason on a card that was never shelved, and nothing was written to the
decision log. Verified after the fact: no `status-changed` to `shelved` in that card's whole history, no card
shelved at the time, and zero blocks recorded across 1193 decisions on it.

The one path that returns a block without writing an event is a decision replayed onto an already-decided
request, since `DecidePermissionBy` returns the existing answer and appends nothing. That fits, and it is not
proof. It matters because it means an agent can be refused and the audit log will not say it happened, which is
the one thing that log exists to prevent.

Worth doing regardless of the cause: make the replay path record that it replayed.

### Small interface debts

- **A folder rule reads the command text, not what a shell would make of it.** An absolute path that only
  exists after expansion, `$HOME/x` or `$(cat somewhere)`, is not seen, so a command reaching outside that way
  is approved when the session sits inside an allowed folder. A literal `cd /elsewhere && rm x` IS caught,
  because `/elsewhere` is visible in the text. Closing the rest means expanding shell syntax, which is its own
  project and a source of new mistakes. Worth knowing before allowing a folder that matters.
- **Auto mode has no time limit.** "For the next hour" is the right shape for something meant to be temporary.
  Today it stays on until switched off, with only the card's `auto` badge as a reminder.

### An agent cannot say it finished

Everything an agent reports lands in `needs-input`, so the board cannot tell "finished, go look at the result"
from "stuck, answer me". Only a human moving a card by hand produces `done`.

The v2 design named `submit(kind="task-complete")` for this and it was never built. Whatever carries it, the
board needs the two states to arrive from the agent rather than being sorted out afterwards, because sorting them
out afterwards means reading each one.

This is the largest remaining hole in the thing atrium exists to do.

### No way to send a message from the board

The back channel works end to end: a message reaches a busy session through its next tool call, an idle one
through the Stop hook, and a supervised one by being typed straight into its terminal. `POST
/v1/tasks/{id}/message` is the only way to send one, so in practice it means curl.

A box on the card is the whole job.

### Hooks for runners that are not Claude Code

The hook wiring is Claude Code's shape: `settings.json`, and the event names in
`internal/claudeconf/hooks.go`. Codex keeps its configuration in TOML somewhere else, and its events are its own.

Nothing about the daemon side is claude-specific. `/activity` and `/session` take an agent name and an event, so
a second harness needs a writer next to `claudeconf` and its own entry in the wanted list, not a new endpoint.

Also unverified and worth checking before building against it: Claude Code's hook list has reportedly grown to
include `Setup`, `UserPromptExpansion`, `PermissionDenied` and `PostToolBatch`. None of those are wired, and none
have been confirmed from the docs rather than from a summary of them.

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

### Pruning on a timer

Sweeping finished columns is a button. Cards still accumulate on their own between presses. An age based sweep on
a schedule, with the age configurable, would keep the board from needing the button at all. Done and dead only:
`Prune` refuses shelved no matter what it is asked, and that has to stay true.

### Status inference for runners that cannot speak

The last piece of supervision. A cooperative runner reports its own state, so atrium never guesses at its output.
A bare shell or a runner with no hook has only its terminal, and inferring `needs-input` from that is heuristic.
Worth doing last, and worth keeping manual status override as the escape hatch.

### The Stop hook is written but not wired

`atrium-stop-hook.ps1` exists and the `/stop` endpoint it talks to is tested. It is not registered in
`settings.json`, so a message queued for an idle session sits in the queue.

Deliberately the one hook the board does not offer to install. A Stop hook that blocks makes a session keep
working, so getting it wrong means sessions that will not stop. Turning that into a button would be handing out
a way to hang every session on the machine.

Making it a subcommand the way the activity hooks now are is the smaller half of the job, and worth doing
whenever the behaviour is wanted: the script holds a machine-specific path, which is exactly what the
subcommand removes.

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
- **Permission requests carry no dedup key.** The store supports it, the replay path is tested and the key is now
  scoped per task, but the hook does not send one, so a crash between a decision and its write could ask twice.
- **A session that dies without warning can hang.** `SessionEnd` covers a clean exit and the reaper covers a
  known pid. A session that is killed outright, with no pid recorded, sits in `running` forever.
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
- **`wire_name` is unique per database and derived from a directory name.** Fine on one machine. Two containers
  off the same image in the same working directory would collide, and a collision does not error, it matches an
  existing card. See `docs/federation-design.md`.
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
- Auto mode, and the review that pays for it.
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
