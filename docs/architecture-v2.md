# Architecture v2: the daemon, the API, and the clients

Status: design, not yet built. Supersedes the Mode A / Mode B framing in `CLAUDE.md` once implementation starts.
Read `docs/state-of-the-art.md` first for where v1 actually sits.

## Why v2 exists

v1 answers "let me talk to many claude sessions from one terminal." That works. It does not answer the question
that actually costs time day to day, which is "what do I have running, which one needs me most, and what was I
even doing in that one."

v2 reframes atrium from a message broker into a **task tracker with live agents attached**. The broker becomes an
implementation detail of one adapter.

## Goals

1. Organize agents. One view of everything running, across repos and across runners.
2. Notify on completion, and distinguish "done" from "waiting on you." Show the one that has waited longest.
3. Remember intent and history. Why did I start this, what has happened since, what failed. Survives restarts and
   survives task switching.
4. Kanban as the primary human surface. Cards move between columns. Age is visible on the card.
5. Runner agnostic. claude-code, codex, ollama, aider, or a bare shell loop. No runner is privileged.
6. Launch and kill agents from atrium itself. The goal is to never open a runner's terminal by hand.
7. Reachable from anywhere over OpenZiti, with a mobile friendly web UI and room for a native app later.

## Non goals

- Not a product. Single user, single operator, built for one workflow.
- No multi tenancy, no accounts, no billing, no public deployment story.
- Not dependent on any vendor's remote control, session format, or cloud. Atrium owns its own state.
- Not a workflow engine. Atrium tracks and supervises work. It does not decide what the work is.

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Storage | SQLite, written to stay Postgres portable | One file, no server. Portability keeps the door open. |
| SQLite driver | `modernc.org/sqlite` | Pure Go. No cgo, so cross compiling from Windows stays trivial. |
| Web UI | React SPA, built with Vite, embedded via `go:embed` | Same JSON plus SSE contract a native app will use later. |
| API shape | JSON plus an SSE event stream, and a WebSocket for terminal attach only | No endpoint returns markup. The SPA and a future app are peers. |
| Permissions surface | A kanban column, plus a derived oldest first queue view | See how the column feels before building a bespoke surface. |
| Agent supervision | Daemon spawns runners under a PTY and owns their stdio | Lets any runner be launched, killed, and attached to from the browser. |
| Output handling | Always captured for attach, parsed for status only when a runner cannot report its own | One fact, one source. Removes any need to arbitrate between a guess and a declaration. |

## The three layers

```
  ingestion adapters              core: atriumd                     clients
  ┌──────────────────────┐        ┌───────────────────────┐        ┌───────────────────┐
  │ PTY supervisor       │───────▶│  HTTP JSON + SSE      │◀───────│ React SPA (web)   │
  │  (universal)         │        │  task lifecycle       │        │ atrium tui        │
  │ atrium-agent MCP     │───────▶│  event log            │◀───────│ atrium CLI        │
  │  (fidelity upgrade)  │        │  process supervisor   │        │ mobile / ziti     │
  │ permission hook      │───────▶│  SQLite               │◀───────│ future native app │
  │ external reporters   │───────▶│                       │        │                   │
  └──────────────────────┘        └───────────────────────┘        └───────────────────┘
```

The rule that keeps this honest: **no client gets a privileged path into the core.** In v1 the TUI receives an
in process `*Hub` pointer (`internal/cli/cli.go` hands the same struct to `h.Serve` and `tui.New`) and reads
state through direct method calls that never cross HTTP. That is why there is no human facing API today. In v2
the TUI talks to the daemon over loopback HTTP like everything else. If the TUI can only see what the API
exposes, the web UI can never be second class.

`atrium hub` survives as a convenience: it starts the daemon in process and attaches the TUI over loopback. One
command, one binary, but no shared pointer. The API can be the terminal. It just cannot skip the API.

## Domain model

The missing noun in v1 is the task. v1's noun is "an agent that POSTed," which is why it cannot tell you how long
something has been sitting, or what you were trying to do.

### Status lifecycle

```
  backlog ──▶ running ──┬──▶ needs-input ───────┐
                        │                       │
                        ├──▶ needs-permission ──┤
                        │                       │
                        ├──▶ done               │
                        │                       │
                        └──◀────────────────────┘

  any state ──▶ shelved   (put it down, keep the history)
  any state ──▶ dead      (killed or crashed)
```

`needs-input` and `done` are the distinction v1 cannot make. Today `submit(kind="response")` covers both. v2 adds
`kind="task-complete"` so an agent can say "finished" rather than "your turn."

`needs-permission` returns to `running` on resolution. Transitions are recorded as events, so the board can show
churn without the task table carrying history.

### Schema

Portable across SQLite and Postgres. Text ULID keys avoid the `AUTOINCREMENT` versus `SERIAL` split. Timestamps
are RFC3339 UTC text, which sorts lexicographically in both engines and stays readable. `CHECK` constraints stand
in for enums. Placeholders are written `?` and rebound to `$N` for Postgres.

```sql
CREATE TABLE task (
  id               TEXT PRIMARY KEY,
  title            TEXT NOT NULL,
  why              TEXT NOT NULL DEFAULT '',
  repo             TEXT NOT NULL DEFAULT '',
  worktree         TEXT NOT NULL DEFAULT '',
  runner           TEXT NOT NULL,
  hostname         TEXT NOT NULL DEFAULT '',
  pid              INTEGER,
  status           TEXT NOT NULL
                     CHECK (status IN ('backlog','running','needs-input',
                                       'needs-permission','done','shelved','dead')),
  created_at       TEXT NOT NULL,
  last_activity_at TEXT NOT NULL,
  waiting_since    TEXT,
  wire_name        TEXT,
  overrides        TEXT NOT NULL DEFAULT '{}',
  rank             REAL NOT NULL,
  UNIQUE (wire_name)
);

CREATE TABLE event (
  id      TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
  at      TEXT NOT NULL,
  kind    TEXT NOT NULL
            CHECK (kind IN ('created','submitted','prompted','perm-requested','perm-decided',
                            'status-changed','output','notified','launched','exited')),
  payload TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX event_task_at ON event (task_id, at);

CREATE TABLE permission (
  id           TEXT PRIMARY KEY,
  task_id      TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
  tool         TEXT NOT NULL,
  command      TEXT NOT NULL,
  requested_at TEXT NOT NULL,
  decided_at   TEXT,
  decision     TEXT CHECK (decision IS NULL OR decision IN ('approve','block')),
  reason       TEXT NOT NULL DEFAULT ''
);

CREATE INDEX permission_pending ON permission (decided_at, requested_at);

CREATE TABLE launch_spec (
  task_id    TEXT PRIMARY KEY REFERENCES task(id) ON DELETE CASCADE,
  cmd        TEXT NOT NULL,
  args       TEXT NOT NULL DEFAULT '[]',
  cwd        TEXT NOT NULL,
  env        TEXT NOT NULL DEFAULT '{}',
  pid        INTEGER,
  started_at TEXT,
  exited_at  TEXT,
  exit_code  INTEGER
);
```

`payload` is TEXT rather than JSONB so the DDL runs unchanged on both engines. If this ever lands on Postgres for
real, that column is the one worth migrating.

### Ordering

`rank` is the operator's manual order within a board column. Dragging a card writes it. To place a card between
two neighbours, take the midpoint of their two ranks, which is why the column is a float rather than an integer:
inserting never renumbers the cards around it. A column can be renormalized on demand if a long run of midpoint
inserts ever exhausts precision.

New cards get a rank that places them at the top of their column. Dropping a card into a different column keeps
the position it was dropped at. Changing status through the API without specifying a position puts the card at the
bottom.

Board order and stack order are deliberately different. The Board sorts by `rank`, because that is a judgement
only the operator can make. The Stack sorts by `waiting_since`, because "who has waited longest" is a fact. Neither
view overrides the other.

### Observed data versus overrides

Every column above falls into one of two buckets.

- **Observed.** What the runner or the supervisor reports: `worktree`, `repo`, `runner`, `hostname`, `pid`,
  `wire_name`, and a default `title`. Refreshed on every reconnect.
- **Overrides.** What the operator sets by hand, stored as a JSON object in `overrides`.

Resolution is one rule: **show the override when one exists, otherwise show the observed value.** Observed data
never overwrites an override.

That rule is the whole point. Name a card "fix the auth bug," let the runner die, and when it reconnects and
reports its folder name again, the name you chose survives. v1 has a single-field version of this already, where
`/rename` sets a display name while the wire name keeps routing. v2 generalizes it to every field, and one JSON
column is cheaper than a second copy of each.

### Identity and registration

A task id must outlive the process, which rules out identifying a task by pid. Restart a runner and the pid
changes, so pid-as-identity splits one piece of work across two cards and loses the history that justified
building v2. Pids are also recycled by the operating system, so a fresh runner can inherit a dead one's number.
v1 already embeds the pid in the agent name (`atrium-38214`), which is exactly why v1 names are not stable and why
`wire_name` is demoted to an attribute here.

Pid stays useful as a *matching hint*: same pid plus same worktree plus still alive means the same process, so
reattach it to its existing card. A different pid means something restarted.

Two registration paths:

1. **Launched by atrium.** The daemon creates the card before the process exists, so it passes the task id into
   the child's environment. No handshake, no ambiguity.
2. **Self started.** The runner connects knowing only what its own process can see. Its first `/submit` carries the
   observed bucket as a registration payload. The daemon matches an existing card by `wire_name`, or by the pid
   hint, or creates one, and returns the stable task id for every later call.

The split matters because the observed bucket is entirely knowable by the agent's own Go code. It is never worth a
model turn to ask an LLM for its own pid or working directory, and the model can get them wrong. The one thing the
code cannot know is intent, so when a freshly created card has no `why`, the daemon's first returned prompt asks
for it. That costs one turn, once, and buys the "what was I even doing" answer that v1 has no place to put. A
small cooperative tool lets the model revise title and why later, as the work drifts from the original ask.

PTY-supervised runners that speak no atrium protocol cannot register at all. For those the daemon fills the
observed bucket from its own launch spec, and intent comes from the operator at card creation.

### How the schema answers each pain point

- **"How long has this been sitting, should I shelve it?"** The board renders `now - last_activity_at` on every
  card. `shelved` is a real state, so putting something down does not mean losing it.
- **"Who notified me, and who has waited longest?"** Pending work is `task WHERE status IN ('needs-input',
  'needs-permission') ORDER BY waiting_since ASC`. The stack is a query, not a data structure. The sound fires on
  the status transition.
- **"What did we do and why?"** `why` is captured at creation. `event` is the append only trail. Task switch, come
  back in a week, read the story.

## HTTP surface

Two listeners, not one. The **agent facing** listener carries runners. The **human facing** listener carries the
TUI, the SPA, and the CLI. They are separate so the agent side can be shut down independently while the human side
stays up to explain why. See "Failure posture" below.

Agent facing, carried over from v1 with a task id added:

```
POST /v1/submit        { task?, observed?, kind, content }
                                                 -> { task, prompt }        (long poll)
POST /v1/permission    { task, tool, command, dedup_key }
                                                 -> { decision, reason }    (blocks)
POST /v1/describe      { task, title?, why? }    -> { ok }
```

`task` is omitted only on a self started runner's first call, which instead sends the `observed` bucket described
under "Identity and registration." The response always carries the resolved `task` id, and the runner uses it from
then on. `dedup_key` is what keeps a crash between decision and write from asking the operator the same question
twice. `describe` is the small cooperative tool that lets a model set or revise intent.

Human facing, none of which exists today:

```
GET    /v1/tasks                    list, filterable by status
POST   /v1/tasks                    create (title, why, repo, runner, launch spec)
GET    /v1/tasks/{id}               detail
PATCH  /v1/tasks/{id}               set overrides, change status, shelve, set rank
DELETE /v1/tasks/{id}               forget
GET    /v1/tasks/{id}/events        history, paged
POST   /v1/tasks/{id}/prompt        send text to the agent (the human's turn)
GET    /v1/tasks/{id}/attach        WebSocket: live terminal, both directions
POST   /v1/tasks/{id}/launch        spawn the runner
POST   /v1/tasks/{id}/kill          terminate the runner
GET    /v1/permissions              pending queue, oldest first
POST   /v1/permissions/{id}/decide  { decision, reason }
GET    /v1/events                   SSE: every state change, for live clients
```

## Failure posture: the wedge

Idle token burn is the one failure this project exists to prevent. v1 prevents it by making the agent client
absorb every transport failure silently. v2 adds durable storage to the agent facing path, which creates a failure
class v1 never had: the daemon is reachable but cannot persist. A reachable daemon that returns a 500, or an empty
prompt, is not a disconnect. The agent client would not recognize it, the LLM would wake up, and the burn returns
in exactly the path this design claims to preserve.

The rule: **storage failure is never a degraded mode.** There is no partial service. Failures sort into three
tiers.

1. **Open or migration failure** (database missing, unmigrated, unreadable, corrupt at startup). Refuse to start.
   Loud on stderr, non-zero exit, before any runner can connect.
2. **Contention** (`SQLITE_BUSY`, locked). Not a failure. Retried internally with backoff and never surfaced to any
   caller. Single writer contention clears in milliseconds and panicking on it would be self inflicted.
3. **Any other write failure** (disk full, I/O error, corruption detected in flight). **Wedge the daemon.**

Wedging means:

- Close the agent facing listener and drop every in flight agent connection. Do not reopen it.
- Do not exit the process. The human facing listener stays up and reports `wedged` with the cause, so the TUI and
  the SPA can show a red banner that says what broke instead of going dark.
- Stop all supervised runners (stage 7 onward). If the system cannot record what a runner does, it should not be
  spending tokens doing it.
- Refuse to unwedge. Recovery is an operator fixing the underlying cause and restarting the daemon deliberately.
  No automatic retry, no half-open state.

The reason this is correct rather than merely dramatic: a closed listener produces connection refused, and
connection refused is the one failure the agent client already handles perfectly. Every cooperative agent parks
inside its current `submit` call, retries on the existing backoff schedule, and never wakes the model. Agents
already mid-turn finish that turn and then park at their next submit. The system quiesces on its own. Returning a
500 instead would route through a path nothing has ever tested.

Two consequences that do not follow from wedging alone and must be built anyway:

- **The permission hook treats 5xx exactly like a transport error.** Both mean unreachable, both fail open. A
  partial failure can return 500 with the process still alive, and a hook that only fails open on a refused
  connection would block the runner there.
- **Permission requests carry a dedup key.** If a decision is made, the write fails, and the agent reconnects to a
  restarted daemon, the same request must not appear a second time for a human to answer again.

Wedge state lives in memory and on stderr, not in the database. By definition the database is what failed.

## Process supervision

The daemon spawns each runner under a pseudo terminal and owns both directions of its stdio. ConPTY on Windows.
`github.com/aymanbagabas/go-pty` is the candidate cross platform wrapper and **must be validated on Windows 11
before we depend on it**.

Atrium does not replace the runner. claude-code is the agent: it owns the tool loop, context management,
permission enforcement, and the MCP client. It also holds the subscription credentials, so bypassing it in favor
of direct API calls would mean paying per token as well as rebuilding everything it does. Atrium supervises
agents. It does not become one. The same reasoning applies to codex and to any other runner.

Launching from atrium is therefore nothing more than atrium typing `claude` on your behalf. The relationship
afterward is identical to the operator starting the runner by hand and activating the loop, which keeps the whole
v1 mechanism valid under supervision.

### Capture, do not interpret

Owning the pty produces a stream of output. There are two entirely different things one can do with that stream,
and conflating them is a mistake.

- **Capture.** Keep the output so a human can attach and read it. Always on.
- **Interpret.** Parse the output to infer status. Only for runners that cannot tell us anything.

A cooperative runner already reports status exactly through `/submit`. Guessing at its output would add a second,
worse signal for the same fact, and the two can disagree: a line ending in a question mark looks like a prompt,
so a card flips to `needs-input` and fires a notification while the runner is still working. The rule that avoids
this needs no precedence table.

> **Never infer status for a runner that talks.** Interpretation exists only for runners that never do.

Because non-cooperative runners produce no competing signal, there is no arbitration to specify anywhere. The two
ingestion tiers stay clean:

- **Cooperative.** Status is declared. Prompt boundaries are exact. Permissions route through the hook. Output is
  captured for viewing and never parsed. This is v1's mechanism, still the good case.
- **Supervised only.** codex, ollama, aider, or `bash`. No cooperation at all, so process state and output are the
  only signals available, and status is coarse. Manual override in the UI is the escape hatch when a guess is
  wrong.

### Attach

Captured output is worth having on its own, independent of status. Being able to open a card in the browser and
watch, or type into, the live terminal is a first class goal, not a debugging afterthought.

Two consequences:

- Output is ANSI laden, so rendering it faithfully means xterm.js in the SPA rather than a plain text pane.
- Attaching needs bidirectional traffic. SSE only flows down, and one HTTP POST per keystroke is not viable, so
  attach is carried by a WebSocket. This is the single documented exception to the JSON-plus-SSE rule, and it is
  scoped to terminal attach alone. Nothing else gets one.

Retention follows from this rather than needing its own policy. Captured output is a bounded ring buffer per task,
sized so that attaching shows useful recent history, not an archive of everything a runner ever printed. Status
transitions and prompts are already durable in `event`, which is what the timeline reads. The buffer is a
convenience for looking at a live terminal, so it lives in memory and is allowed to be lossy.

## Web UI

React plus Vite, built to static assets, embedded with `go:embed` so the shipped artifact stays a single binary.
Views:

1. **Board.** Kanban by status, ordered within each column by `rank`. Card shows title, runner, repo, age since
   last activity, and a waiting badge. Drag across columns to change status, drag within one to reorder, and the
   order survives a reload because it is stored rather than client side.
2. **Stack.** Everything waiting on a human, oldest first by `waiting_since`. The answer to "who has been blocked
   longest." Ignores `rank` on purpose.
3. **Task detail.** The `why`, the event timeline, and a prompt box.
4. **Attach.** The live terminal of a supervised runner, rendered with xterm.js over a WebSocket, readable and
   typeable from the browser. Works the same for claude-code, codex, or a bare shell, because it is just that
   process's terminal.
5. **Perms.** The pending queue with approve and block plus guidance. Provisional, since the board column may
   make it redundant.

Mobile friendly is a layout constraint on these same views, not a separate build. Attach is the one view where
that constraint bites, since a terminal has a fixed column count and a phone does not have many.

## Remote access

The API is plain HTTP, so exposure is configuration rather than code. Bind the daemon to an OpenZiti service
instead of, or alongside, loopback. No vendor remote control, no homegrown transport, no auth invented here. A
future native app is another client of the same JSON plus SSE contract.

## What carries over from v1

These v1 invariants are agent side or hook side and survive the split untouched. Do not regress them.

1. The LLM never sees a daemon disconnect. `internal/agent/agent.go` retries forever with backoff.
2. The LLM never sees an empty prompt. Long poll timeouts are absorbed internally as keepalives.
3. No token burn while idle, which follows from 1 and 2.
4. Activation is opt in, and the activation message's content is ignored.
5. The permission hook fails open when the daemon is unreachable.
6. The hook does not gate `mcp__*` or `ToolSearch`, since gating the agent's own `submit` is circular.

Preserving 1 through 3 across the split is not automatic, because v2 introduces storage on the agent facing path.
"Failure posture" above is what actually holds them up. Read it as part of this list rather than as a separate
concern.

## What v1 rules get retired

- **"The hub is intentionally amnesiac. Restart equals reset."** Deliberately reversed. Goals 2 and 3 are
  unanswerable without durable state. Record the reversal in `CHANGELOG.md` rather than dropping it silently.
- **"The hub keeps state in memory."** Replaced by SQLite.
- **Agent identity as the routing key.** `wire_name` becomes an attribute of a task rather than the primary
  identity. Task ids are stable across runner restarts, which wire names never were. Pid drops to a matching hint.
- **`/rename` as a special case.** Generalized into the observed-versus-overrides rule, which applies to every
  field rather than to the display name alone.
- **The TUI as an in process consumer of `*Hub`.** Becomes an HTTP client.

## Staged migration

Each stage leaves `atrium hub` working.

1. **Checkpoint.** Commit the existing working tree. The refactor needs a clean base.
2. **Storage.** Add `internal/store` with the schema, migrations, and the rebind helper. Nothing consumes it yet.
   Includes the three tier failure classification from "Failure posture," so the wedge exists before anything can
   trigger it.
3. **Task model behind the existing hub.** Every `/submit` and `/permission` creates or updates a task and appends
   events. Behavior does not change on the happy path. State becomes durable, and the wedge becomes reachable, so
   the listener split and the hook's 5xx handling land here too. Registration goes in here as well: existing
   agents send no task id, so the self started path and the observed-versus-overrides rule have to work from the
   first commit that touches storage.
4. **Human facing API.** Add the `/v1` endpoints and the SSE stream on top of the task model.
5. **Cut the pointer.** Rewrite the TUI against the HTTP API. Delete the privileged path. This is the stage that
   proves the API is complete.
6. **SPA.** Board, stack, and task detail against the same API.
7. **Supervision.** PTY spawn, kill, and output capture, then browser attach over a WebSocket. Validate ConPTY
   first. Status inference for non-cooperative runners comes last, after attach proves the capture works.
8. **Ziti.** Bind the daemon to a service and confirm the SPA works unchanged over the overlay.

Stages 1 through 5 are the load bearing refactor. 6 through 8 are additive.

## Open risks

- ConPTY behavior under Go on Windows 11 is unvalidated. If `go-pty` disappoints, stage 7 needs a different
  library or a Windows specific path.
- Inferring status from output for non-cooperative runners is heuristic. Expect it to be wrong sometimes, and keep
  manual status override in the UI as the escape hatch. Cooperative runners are exempt by rule, so this risk never
  touches claude-code.
- Terminal attach in a browser is the one place the client contract widens to a WebSocket. Watch for that
  exception spreading.
- Postgres portability is asserted, not tested. If it matters, add a CI job that runs the migrations against both.
