# Backlog

What is outstanding, why it matters, and what is out of scope. Ordered roughly by value.

Kept here rather than in a conversation, because a list that lives in a chat dies with it.

## Where things stand

The daemon is what gets used. It has durable state, a permission gate with standing rules, a web board, pseudo
terminals it owns and can attach to from the browser, live activity per card, auto mode with a review, launching
with a directory picker, a message channel into a running session, and a wind-down that is not a kill.

`atrium hub` and `atrium serve` are untouched and still work.

The gaps below are ordered by what would change day to day. The two that block existing features from working at
all are first: neither is code, both are wiring.

## Next

### Wire the activity hooks into settings.json

`atrium-activity-hook.ps1` exists and `/activity` is tested end to end, but nothing calls it yet, so every
`running` card looks the same whether its session is working or sitting at a prompt.

Four entries. See `docs/hooks.md` for what goes where and why.

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

### Group cards by project, however the operator wants

A card shows its leaf directory, so `dotfiles` and `targetted-releases` sit next to each other with nothing
saying which repo either belongs to. With several worktrees per repo, the column is a list of names that only
mean something if you already know them.

Grouping by project is the fix, and the grouping rule should not be atrium's to decide: the operator already has
a worktree layout with its own conventions. Two hooks, both plain JavaScript held in the browser:

- **group a card**, `(task) => string`
- **order the groups**, `(a, b) => number`

With defaults that derive a project from the worktree path, so it works before anyone writes anything. A colour
per group from a hash of its name, so it is stable without configuration.

Running operator-supplied JavaScript in the operator's own browser on their own machine is not a security
question, but a broken function must never take the board down: wrap it, fall back to the default, and show the
error.

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

Left off on purpose until the behaviour is understood well enough to want it. A Stop hook that blocks makes a
session keep working, so getting it wrong means sessions that will not stop.

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
- **Supervised runners die with the daemon.** The daemon owns each pseudo terminal, and on Windows closing one
  takes the attached process with it. There is no reattach. So stopping the daemon ends every runner it started,
  and a runner cannot outlive a restart. Resume ids are the answer rather than orphan survival, which ConPTY does
  not offer.
- **The daemon can silently open an empty database.** `WORKTREE_ROOT` unset means it falls back to `~/.atrium`,
  which looks exactly like every rule and card having vanished. It should say loudly when it creates a database
  rather than opening one.

## Waiting on something external

- **Codex and ollama are configured but disabled.** Their command lines were never confirmed on this machine, and
  guessing at an invocation would produce a runner that fails on first use.
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
  dialog.

## Review

- Mercurius round two ran against the revised design. Two findings were stale documentation, since fixed. The
  third, a card leaving a waiting state without answering its pending requests, was a real bug and is fixed.
- Mercurius round three ran against the code. Two folder-rule findings were real and security relevant: a command
  naming a path inside an allowed folder AND one outside it was approved, and a quoted Windows path with spaces
  was split into tokens that looked relative. Both fixed with regression tests. A third, `/activity` returning
  400 on a malformed body against its own fail-open contract, was also real and fixed.
- Nothing has been run against `docs/supervision-design.md` as built.
