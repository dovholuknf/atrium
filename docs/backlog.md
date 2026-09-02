# Backlog

What is outstanding, why it matters, and what is deliberately not being done. Ordered roughly by value.

Kept here rather than in a conversation, because a list that lives in a chat dies with it.

## Next

### Terminal UX, now that attach works

Attaching works, but the surface around it does not match how it is used.

- **It is a modal.** Wrong shape for something you switch between and leave open. Should be a view with a list of
  attachable sessions beside the terminal, so switching is a click rather than close-then-reopen.
- **Copy and paste fight the terminal.** `ctrl-c` is sent as an interrupt even when text is selected, so copying
  interrupts the runner. Needs `ctrl-shift-c` and `ctrl-shift-v`, `ctrl-c` interrupting only when nothing is
  selected, and copy on selection as an option.
- **A permission request is invisible while attached.** The runner blocks, the terminal shows nothing, and the
  request is over in the perms tab. An attached terminal should show the pending request inline with its buttons.
- **A notification should be able to attach.** When a supervised runner needs input, clicking through should land
  in its terminal rather than the board.

### Mobile

The board is unusable below a certain width. Columns, one line rows and the terminal all assume a wide window.
Needs a narrow layout: a single column list rather than a kanban, rows that wrap instead of truncating, and a
decision about what a terminal even means on a phone.

### Board actions without opening a card

Right click, or a `...` menu per card, for the things done most: shelve, done, attach, terminate. Opening the
detail dialog for every small action is too many clicks.

### Launch form needs the enter key

Typing a directory and pressing enter should start the runner rather than doing nothing.

### PTY supervision

The single largest gap. Three things people keep asking for all depend on it, and none of them can work while a
launched runner owns its own terminal:

- Attaching to a running agent from the browser.
- A terminate button that works on a launched runner. Window mode hands the session to the terminal and the
  wrapper exits, so there is no process for atrium to signal.
- Shutdown waiting for its runners, which only means something once atrium owns them.

Order of work:

1. ~~Spike ConPTY under Go on Windows 11.~~ **Done.** go-pty v0.2.3 handles spawn, output with ANSI escapes,
   keystrokes, resize, and clean child exit. One trap found and written up in `docs/architecture-v2.md`: after a
   pty is torn down, returning normally from `main` leaves the process with exit status 127, so any build that
   owns a pty must call `os.Exit(0)` explicitly. A daemon that logs a clean shutdown and then reports failure to
   its service manager is the symptom.
2. Spawn and own the process, with the pid on the card so the existing terminate and liveness paths light up.
3. Capture output into a bounded per task ring buffer. Decide retention then, not before.
4. Browser attach over a WebSocket with xterm.js. This is the one place the client contract widens past JSON and
   SSE, so keep it scoped to attach alone.
5. Status inference for runners that cannot report their own state. Last, because it is heuristic and every
   cooperative runner is exempt by rule.

### Join and leave, from inside a session

A way for a claude session to put itself on the board, or take itself off, without restarting. Today gating is
decided when the session starts, by the hook reading its environment, so a session that was not gated stays
ungated for its whole life.

Shape: an `atrium` CLI subcommand, or a tiny MCP tool, that posts to `/session` with `join` or `leave`. Joining
registers the session and turns gating on for it. Leaving marks the card done and stops gating, so a session can
be handed back to itself.

The permission hook already reads `ATRIUM_PERM_GATE` per call rather than caching it, so the gate can be flipped
at runtime if the state lives somewhere the hook can see: the daemon knowing which sessions have joined is enough,
since the hook already asks the daemon on every call.

### Resume has no session id

The resume button exists and the runner's resume arguments are configured, but nothing populates `resume_id` now
that ledger adoption is gone. The session hook already receives `session_id` in its payload and sends it. Storing
it on the card in `onSession` is a small job that makes an existing button real.

### Verify the always button

The fix for `always` doing nothing was found by reading the code, not by watching it work. Click it once against a
live daemon and confirm a rule appears.

## Known gaps

- **Stage 5 was skipped.** The TUI still receives a `*Hub` pointer in process rather than going through the HTTP
  API. That was the stage meant to prove the API is complete, so until it is done the API has exactly one client
  and its gaps are invisible.
- **The board is a plain page**, not the React app the decisions table names. It speaks the same JSON and SSE
  contract, so this is a client side swap whenever it is worth doing.
- **Drag to reorder is not wired.** The `rank` column, midpoint insertion and the ordering rules all exist. The
  board only uses rank for display.
- **Permission requests carry no dedup key.** The store supports it and the replay path is tested, but the hook
  does not send one, so a crash between a decision and its write could ask twice.
- **A hook connected session with no pid never goes dead.** The reaper only acts on a known pid, and the session
  hook's parent walk is best effort. A card whose runner vanished without a `SessionEnd` sits in running.
- **Postgres portability is asserted, not tested.** The schema is written for it. Nothing runs the migrations
  against it. A CI job would settle it.
- **`docs/test-plan.md` predates v2.** It covers the hub and the agent loop, not the daemon, the board, rules,
  launching or the permission diff.
- **Repo metadata is unset.** `gh repo edit` returns 403 with the current token, so the description and topics on
  the GitHub page are still empty. Needs `gh auth refresh -s repo` or setting them in the web UI.

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

## Review

- Mercurius round two has not been run against the revised `docs/architecture-v2.md`. Round one is recorded in
  `.mercurius/`, which is untracked, along with the decisions taken from it.
