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
   The notification reaches you where you are, not only where atrium is: a desktop notification carrying the
   decision itself, an in page toast when the tab is already in front, and a different sound per kind.
3. Remember intent and history. Why did I start this, what has happened since, what failed. Kept across restarts
   and across task switching. The record is an append only event log, so the history can be read back rather than
   inferred from whatever the current state happens to be.
4. Kanban as the primary human surface. Cards move between columns. Age is visible on the card, and a column that
   grows too tall can be folded out of the way.
5. Runner agnostic. claude-code, codex, ollama, aider, or a bare shell loop. No runner is privileged, and adding
   one is configuration rather than a code change.
6. Launch and kill agents from atrium itself. The goal is to never open a runner's terminal by hand.
7. Reachable from anywhere over OpenZiti, with a mobile friendly web UI and room for a native app later.
8. Answer every question that does not need a model. Whether a process is still alive, whether a request matches
   a rule already agreed to, what a pending edit would actually change: all of these are free, and spending a
   turn on any of them is a bug rather than a design choice.

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

The rule that keeps this true: **no client gets a privileged path into the core.** In v1 the TUI receives an
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

### A waiting card cannot be put down without answering

An agent asking for permission is frozen. It stays frozen until something answers, and nothing else will: there is
no timeout on the agent side, by design, because a timeout would either wake the model or guess at an answer.

That makes it possible to strand an agent by tidying the board. Shelve a card that is holding a request and the
card moves while the question does not: the request stays in the queue, the agent stays frozen, and the request
can be approved an hour later against a situation that has moved on. The interface would be offering a decision
the operator has already mentally discarded.

Three rules close that off. They are lifecycle rules, not interface polish, so nothing that changes a status can
opt out of them.

1. **Moving a card out of a waiting state answers what it was holding.** Every pending request on that task is
   decided as `block`, with a reason naming what the operator did. A block includes guidance, so the agent is
   released and told why rather than left waiting.
2. **A shelved card is a standing no.** While a task is shelved, a new request from it is blocked immediately
   with the same explanation, and never reaches the queue. Otherwise an agent asks, gets nothing, and freezes
   behind a card that has been nobody is looking at any more. Unshelving restores normal behavior, or
   shelving would be a trap.
3. **A request nobody answers keeps asking.** One alert on arrival is not enough, because the alert is easy to
   miss and the cost of missing it is an agent idle for an hour. After a minute the board nags: every minute for
   the first ten, then every five, by sound, desktop notification and toast. Widening rather than constant, so it
   stays a reminder instead of an assault.

The operator is told before rule one applies, not after. Moving a card that is holding requests asks first and
says exactly what will happen to them.

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
  reason       TEXT NOT NULL DEFAULT '',
  -- What the tool would actually do: the diff for an edit, the content for a
  -- write. A path names the target without saying what happens to it.
  details      TEXT NOT NULL DEFAULT '',
  -- "you" for a decision made by hand, or the pattern of the rule that
  -- answered it. Without this the log cannot distinguish the two.
  decided_by   TEXT NOT NULL DEFAULT '',
  -- The pattern a rule this decision established, set when the answer was
  -- "always" or "never". Distinguishes those from a one-off approval.
  rule_created TEXT NOT NULL DEFAULT '',
  -- Idempotency for the replay path. NULL when the caller sends no key,
  -- because UNIQUE permits many NULLs but only one empty string.
  --
  -- Scoped to the task, not global. The key means "this agent's same
  -- request", so two agents deriving keys the same way, from a hash of the
  -- command for instance, must not collide and hand one the other's answer.
  dedup_key    TEXT,
  UNIQUE (task_id, dedup_key)
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
two neighbors, take the midpoint of their two ranks, which is why the column is a float rather than an integer:
inserting never renumbers the cards around it. A column can be renormalized on demand if a long run of midpoint
inserts ever exhausts precision.

New cards get a rank that places them at the top of their column. Dropping a card into a different column keeps
the position it was dropped at. Changing status through the API without specifying a position puts the card at the
bottom.

Board order and stack order differ. The Board sorts by `rank`, because that is a judgement
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
2. **Self started.** The runner connects knowing only what its own process can see. Its first `/submit` sends the
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

Two listeners, not one. The **agent facing** listener serves runners. The **human facing** listener serves the
TUI, the SPA, and the CLI. They are separate so the agent side can be shut down independently while the human side
stays up to explain why. See "Failure posture" below.

Agent facing, carried over from v1 with a task id added:

```
POST /v1/submit        { task?, observed?, kind, content }
                                                 -> { task, prompt }        (long poll)
POST /v1/permission    { task, tool, command, dedup_key }
                                                 -> { decision, reason }    (blocks)
POST /v1/describe      { task, title?, why? }    -> { ok }
POST /session          { agent, event, runner?, cwd?, pid?, task_id?, resume?, source? }
                                                 -> { ok }
GET  /gate?agent=X                               -> { gate: bool }
POST /stop             { agent, stop_hook_active? }
                                                 -> { continue } | { decision, reason }
POST /activity         { agent, task_id?, event, tool? }
                                                 -> { ok }                  (fire and forget)
```

`/gate` is what makes `join` and `leave` take effect on the very next tool call. The hook used to decide from its
own environment, which fixed the answer for the life of a session.

`/stop` rides the harness's Stop hook, which fires as a turn ends. A Stop hook that blocks tells the model to keep
going with the reason it was given, which is the only way to reach a session that is sitting idle and therefore
making no tool calls at all. Nothing queued means `{"continue":true}` and the turn ends normally.

`/activity` is what a card's live badge is made of, and it is the one endpoint whose failure posture lives on the
**caller's** side. It rides `PreToolUse`, so a slow or stalled daemon would add latency to every tool call in
every session. The hook gives it one second, ignores every failure, and nothing downstream reads its result. A
session that never lands a single activity post behaves exactly as it did before the endpoint existed. See
`docs/activity-design.md`.

`/session` is posted by the harness's own session hooks rather than by the model, because a session sitting at its
prompt has made no tool call and would otherwise be invisible, and asking the model to announce itself would spend
a turn on something the harness already knows. The rules it follows:

- **Identity.** `task_id` binds a runner atrium launched to the card that launched it. Otherwise the task is
  matched or created by `agent`, exactly as `/submit` does, and `pid` and `cwd` refresh the observed bucket.
- **`event: "start"`** records a `launched` event. It moves a card to `running` only from `dead` or `backlog`,
  which is what a resume looks like from here. A card already running or waiting is left where it is.
- **`event: "end"`** records an `exited` event and moves the card to `dead`. Not to `done`: ending a session says
  the runner is gone, not that the work succeeded, and only a human or a `task-complete` submit can claim that.
  A card the operator shelved or marked done stays where they put it.
- **Failure is silent.** A session must never fail to start because atrium was not listening.

`task` is omitted only on a self started runner's first call, which instead sends the `observed` bucket described
under "Identity and registration." The response always returns the resolved `task` id, and the runner uses it from
then on. `dedup_key` is what keeps a crash between decision and write from asking the operator the same question
twice. `describe` is the small cooperative tool that lets a model set or revise intent.

Human facing:

```
GET    /v1/health                   ok, and the halt cause if there is one
GET    /v1/tasks                    list, filterable by status
GET    /v1/tasks/{id}               detail, with display title, ages, supervised, activity
PATCH  /v1/tasks/{id}               overrides, status, why, rank, auto_approve
DELETE /v1/tasks/{id}               forget
POST   /v1/tasks/prune              { statuses?, older_than_hours? } -> { removed }
GET    /v1/tasks/{id}/events        history, paged
GET    /v1/tasks/{id}/review        what this session was allowed to do, folded and grouped
POST   /v1/tasks/{id}/prompt        send text to the agent (the human's turn)
POST   /v1/tasks/{id}/message       queue something to say to a running session
GET    /v1/tasks/{id}/attach        WebSocket: live terminal, both directions
POST   /v1/tasks/{id}/kill          terminate the runner
POST   /v1/launch                   spawn a runner from a harness row
GET    /v1/permissions              pending queue, oldest first, each naming its agent
POST   /v1/permissions/{id}/decide  { decision, reason, forever?, prefix?, kind?, command? }
GET    /v1/permissions/history      decided requests, newest first, each naming its agent
GET    /v1/rules                    standing answers, most used first
POST   /v1/rules                    write one by hand { tool, prefix, kind, decision, reason, scope? }
DELETE /v1/rules/{id}               forget one
GET    /v1/rules/export             every rule as json
POST   /v1/rules/import             load rules from json
GET    /v1/rules/preview-claude     what importing from Claude Code would add
GET    /v1/harnesses                runner configuration
PUT    /v1/harnesses/{id}           add or update one
GET    /v1/browse?path=             list directories on the DAEMON's filesystem
POST   /v1/shutdown                 wind down. loopback only unless a token is configured
GET    /v1/events                   SSE: every state change, for live clients
```

`/v1/browse` exists because the browser's own directory picker reads the filesystem of whatever machine the
browser is on. That is the right answer only when it happens to be the same machine as the daemon, and the whole
point of the remote-access goal is that it will not be. The daemon is what has to open the directory, so the
daemon is what lists them.

`kind` on a decide, and on a rule, is `command` or `path`. A `path` rule covers work inside a directory, which
was previously expressible only as a command glob that had to account for the quoting around the path itself.

`scope` on a rule narrows it to one worktree. Omitted means every session, which is the right default for a rule
written by hand, and is stated because a wrong scope produces a rule that never fires.

## Failure posture: the halt

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
3. **Any other write failure** (disk full, I/O error, corruption detected in flight). **Halt the daemon.**

Wedging means:

- Close the agent facing listener and drop every in flight agent connection. Do not reopen it.
- Do not exit the process. The human facing listener stays up and reports `halted` with the cause, so the TUI and
  the SPA can show a red banner that says what broke instead of going dark.
- Stop all supervised runners (stage 7 onward). If the system cannot record what a runner does, it should not be
  spending tokens doing it.
- Refuse to restart out of it. Recovery is an operator fixing the underlying cause and restarting the daemon.
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

Halt state lives in memory and on stderr, not in the database. By definition the database is what failed.

## Process supervision

The daemon spawns each runner under a pseudo terminal and owns both directions of its stdio. ConPTY on Windows,
through `github.com/aymanbagabas/go-pty`.

### ConPTY was validated, and it has one trap

A spike on Windows 11 with go-pty v0.2.3 confirmed all five things supervision depends on: spawning a process
under a pty, reading its output including the ANSI escapes a browser terminal needs, writing keystrokes back,
resizing so the child notices, and the child exiting rather than leaking when the pty closes.

One surprise, recorded here because it will otherwise cost an afternoon:

> **After tearing down a pty, returning normally from `main` leaves the process with exit status 127.**
> An explicit `os.Exit(0)` gives 0. The parent shell is unaffected either way.

The symptom is a daemon that logs a clean shutdown and then reports failure to whatever started it: a service
manager restarting it in a loop, a CI step going red, a wrapper script taking the error branch. Nothing in the
logs will suggest a cause, because the shutdown was clean. Any atrium build that owns a pty has to exit
explicitly rather than falling off the end of `main`.

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
watch, or type into, the live terminal is a explicit goal, not a debugging afterthought.

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

- **"The hub is intentionally amnesiac. Restart equals reset."** Reversed. Goals 2 and 3 are
  unanswerable without durable state. Record the reversal in `CHANGELOG.md` rather than dropping it silently.
- **"The hub keeps state in memory."** Replaced by SQLite.
- **Agent identity as the routing key.** `wire_name` becomes an attribute of a task rather than the primary
  identity. Task ids are stable across runner restarts, which wire names never were. Pid drops to a matching hint.
- **`/rename` as a special case.** Generalized into the observed-versus-overrides rule, which applies to every
  field rather than to the display name alone.
- **The TUI as an in process consumer of `*Hub`.** Becomes an HTTP client.

## Staged migration

Each stage leaves `atrium hub` working.

1. **Checkpoint.** Commit the existing working tree. The refactor needs a clean base. **Done.**
2. **Storage.** Add `internal/store` with the schema, migrations, and the rebind helper. Includes the three tier
   failure classification from "Failure posture," so the halt exists before anything can trigger it. **Done.**
3. **Task model behind the existing hub.** Every `/submit` and `/permission` creates or updates a task and appends
   events. Behavior does not change on the happy path. State becomes durable, and the halt becomes reachable, so
   the listener split and the hook's 5xx handling land here too. Registration goes in here as well: existing
   agents send no task id, so the self started path and the observed-versus-overrides rule have to work from the
   first commit that touches storage. **Done.**
4. **Human facing API.** Add the `/v1` endpoints and the SSE stream on top of the task model. **Done.**
5. **Cut the pointer.** Rewrite the TUI against the HTTP API. Delete the privileged path. This is the stage that
   proves the API is complete. **Not done.** The TUI still receives a `*Hub` in process. Everything since stage 4
   has gone into the board instead, which means the API is exercised by only one client and its gaps are
   invisible.
6. **Board.** Kanban, stack, permissions and runners against the same API. **Done**, as a plain page rather than
   the React SPA the decisions table names. The JSON plus SSE contract is unchanged, so swapping it is a client
   side job.
7. **Supervision.** PTY spawn, kill, and output capture, then browser attach over a WebSocket. Validate ConPTY
   first. Status inference for non-cooperative runners comes last, after attach proves the capture works.
   **Done, except status inference.** Window mode launches a runner in a real terminal. PTY mode is now the
   default for a launched runner: the daemon owns the pseudo terminal, holds 256KB of scrollback, fans output out
   to every attached browser, and takes keystrokes, resizes and signals back over a WebSocket. Stopping walks
   ctrl-c, then `exit`, then closing the terminal, then a kill, narrating each step. Status inference for a
   runner with no hooks is still the missing piece.
8. **Ziti.** Bind the daemon to a service and confirm the board works unchanged over the overlay. **Not started.**

Stages 1 through 5 are the critical refactor. 6 through 8 are additive.

## Built since this document was written

These were not in the original plan and are worth recording, because two of them replaced ideas it proposed.

- **Standing rules.** A permission request that matches a stored pattern is answered without ever being shown.
  Patterns are a prefix by default and a glob when they contain `*` or `?`, and the most specific match wins.
  Importing from Claude Code's own allow and deny lists brought 134 rules across on the first run, which is the
  difference between the board being usable and being a click farm.
- **Runners are rows.** A harness is a command, arguments, a working directory, an environment, a launch mode and
  a way to resume. Adding claude, codex, ollama or anything else is configuration. Nothing about any particular
  runner is special cased in code.
- **Sessions register through hooks, not through the model.** `SessionStart` and `SessionEnd` post to `/session`.
  A session sitting at its prompt has made no tool call, so waiting for the permission hook would leave it
  invisible, and asking the model to announce itself would spend a turn on something the harness already knows.
  `SessionEnd` is the more valuable half: it is the only reliable signal that a session is over.
- **Liveness is a syscall.** A card carries the runner's pid, and whether that process still exists is a question
  the operating system answers for free. A reaper sweeps every twenty seconds. A card with no known pid is left
  alone, because not knowing is not the same as being dead.
- **The change, not just the target.** A permission request includes the actual diff, and the board renders it with
  unchanged context dimmed and the changed words picked out. "Approve this edit" is not answerable from a file
  path.
- **Launching scrubs the environment.** The daemon is usually started from inside a claude session, so its
  environment holds that session's markers. Passing them on made a launched runner believe it was a child
  session, which silently disabled transcript saving.
- **Status is a column, activity is a badge.** The columns are buckets of human attention, so a card is in one
  because you must act or because you decided something. What a runner is doing right now, thinking or running a
  named tool or waiting on subagents, is a fact about the runner and never needs a human, so it goes on the card.
  It is held in memory and never written down, because a stored activity is a lie the moment the daemon restarts.
  `docs/activity-design.md`.
- **Auto mode, and the review that pays for it.** A session can be told to approve without asking while still
  recording everything, and the record is then readable: grouped by tool, identical calls folded, the ones nobody
  saw first. Auto mode sits last in the permission chain, so a `never` rule, a shelved card and a queued message
  all still win. `docs/auto-mode.md`.
- **Rules can cover a folder.** Expressing "let it work in here" as a command glob means writing a pattern that
  also accounts for the quoting around the path, and `rm -f "C:/x/*"` fails against `rm -f "C:/x/y.db"` over the
  closing quote alone, silently. A folder rule says what was meant.
- **A message back channel.** There is no way to type at a claude session from outside, so a queued message waits
  until the session can hear one: typed straight into its terminal when atrium owns it, carried back on the next
  tool call through the permission hook when it does not, or delivered as the turn ends through the Stop hook when
  the session is idle. Framed so the model reads it as the operator talking rather than as a policy refusal.
- **Stopping is not killing.** The daemon owns each pseudo terminal and closing one takes the attached process
  with it, so killing the daemon ends every runner it started at once. `atrium stop` and `POST /v1/shutdown` reach
  the narrated wind-down instead.

## Abandoned

- **Adopting sessions from the gwt ledger.** Implemented, then removed. It turned every session the ledger had
  ever seen into a card, and the ones it could see were ones it could not talk to, so the board filled with
  hundreds of entries labeled as waiting on a human who had no way to answer them. Hooks bring a session in the
  moment it does anything, which is both accurate and free. The `external_id` and `resume_id` columns survive for
  whatever reports a session id next. `resume_id` is populated now, by the session hooks.

## Open risks

- **A supervised runner cannot outlive the daemon.** The daemon owns each pseudo terminal, and on Windows closing
  one terminates the attached process. ConPTY offers no reattach, so there is no orphan-survival path to build.
  The answer is resume ids, which means a daemon restart costs a session its terminal but not its conversation.
  Anything that makes the daemon restart often makes this worse.
- **The subagent count is inferred.** There is no hook for a subagent starting, so a `Task` tool call counts up
  and `SubagentStop` counts down. Inference drifts. The floor is enforced at zero so it can never render as
  nonsense, and the whole activity record expires after fifteen minutes of silence, but the count can be wrong.
- **Auto mode has no time limit.** It stays on until switched off. The card shows an `auto` badge so it is
  never invisible, but "auto mode for the next hour" is the shape this should have.
- ConPTY behavior under Go on Windows 11 is validated, with one trap: returning from `main` after tearing a
  pseudo terminal down yields exit 127. See "ConPTY was validated, and it has one trap" above.
- Inferring status from output for non-cooperative runners is heuristic. Expect it to be wrong sometimes, and keep
  manual status override in the UI as the escape hatch. Cooperative runners are exempt by rule, so this risk never
  touches claude-code.
- Terminal attach in a browser is the one place the client contract widens to a WebSocket. Watch for that
  exception spreading.
- Postgres portability is asserted, not tested. If it matters, add a CI job that runs the migrations against both.
