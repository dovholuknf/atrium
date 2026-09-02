# Backlog

What is outstanding, why it matters, and what is deliberately not being done. Ordered roughly by value.

Kept here rather than in a conversation, because a list that lives in a chat dies with it.

## Next

### Put atrium on PATH

The `atrium-join` and `atrium-leave` skills call `atrium` plainly and treat "not found" as the answer. Until the
binary is on PATH they cannot work, and hardcoding a checkout path into a skill would work on one machine and rot
the first time it moves.

### Small interface debts

- **The launch form ignores the enter key.** Typing a directory and pressing enter should start the runner.
- **Board actions need a right click or a `...` menu.** Shelve, done, attach and terminate are the things done
  most, and each currently costs opening the detail dialog.
- **Drag to reorder is not wired.** The `rank` column, midpoint insertion and the ordering rules all exist. The
  board only reads rank, never writes it.

### Shelving should stop the runner, not just move the card

Shelved is not another word for finished. Done and dead are both over: done because the work is over, dead because
the process is. Shelved says the opposite, that this is coming back, and today it only moves a card while the
runner keeps sitting there holding a conversation open.

What it should do instead: record the resume id, stop the runner, and turn the card's action into resume. That
makes shelving cheap enough to use on anything not being worked on right now, which is the point of it.

`resume_id` is captured now, so the missing half is the stop-and-restart path.

### Working directories from a repo URL

Launching asks for a directory that has to already exist. It should take a repo URL instead, and prepare the
working directory the way the shell workflow already does: clone if needed, create a worktree for the branch, then
start the runner there.

This is what makes launching from the board sufficient on its own. Until then, every new piece of work starts in a
terminal to make somewhere for it to run.

### Live sub-state on a card

The columns answer "what needs me". A card should also say what its runner is doing right now: thinking, running
a named tool, or waiting on N subagents. That is a badge on the card, not a column, because a column is a bucket
for human attention and "thinking" never needs any.

The hooks to feed it exist: `PreToolUse` and `PostToolUse` carry the tool name, `SubagentStop` counts subagents.
Nothing sends them yet.

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

## Deliberately not doing

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
- A narrow layout, and the board served with no-store so a rebuild is always what is on screen.
- Every browser alert, confirm and prompt replaced with the app's own dialog.

## Review

- Mercurius round two ran against the revised design. Two findings were stale documentation, since fixed. The
  third, a card leaving a waiting state without answering its pending requests, was a real bug and is fixed.
- Round three has not been run against `docs/supervision-design.md` as built.
