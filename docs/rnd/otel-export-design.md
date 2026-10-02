# OpenTelemetry export: atrium sends traces, metrics and logs to a collector the operator runs

Status: design by @rnd, 2026-10-02, revised for @review's HOLD (0d5d2db5, `docs/backlog/rnd/rd-new-review-b1ca8e47.md`).
Nothing built. Asked by clint, from question 4 of
`docs/rnd/langchain-openwiki-spike.md` ("rnd task").

## 0. The answer

- **Each atrium process exports what it already knows**, by OTLP over HTTP, to one endpoint: a collector the
  operator runs. The collector holds any vendor key (LangSmith, Grafana Tempo, Honeycomb, Jaeger) and forwards to
  them. Atrium holds no key. There is no headers setting, and the SDK's own `OTEL_*` env inputs are
  cleared and unused (section 3), so there is nowhere to put one.
- **Off by default, one setting.** `otel_endpoint`, empty by default. Empty means no exporter, no goroutine and no
  connection.
- **Never on the hot path.** Hooks do not change: they still post to the room daemon with a 1 s budget, and the
  daemon still answers before it decodes. The exporter is a subscriber to things the daemon already publishes after
  the fact: the in-memory event bus, the event sink after commit, and the usage rows. The event sink runs on the
  writer's goroutine, so it only does a non-blocking enqueue. Every queue and every map of open spans is bounded, and
  a full one drops and counts. A dead collector, or one that never answers, costs a counter, never a wait.
- **An allowlist decides what leaves, by field and by value.** Every attribute is named in one table in code and
  marked exported, under the per-field rule of `docs/runtime/scm-design.md`, with a value class (enum, count, id or
  operator-side name) enforced at the exporter. Anything not in the table is dropped, and a value outside its class
  becomes `other`. Prompt text, reply text, tool
  input, permission commands and details, paths, titles, recaps, and anything a source or card holds never leave the
  machine.
- **What is emitted:**
  - **Traces**: one per turn, with a span for each tool call, permission decision and subagent. One per PR run, with
    a span for each step and fork.
  - **Metrics**: tokens, gate wait, decisions, turn and tool duration, cards by status, queue depth, relay backlog,
    and room resources.
  - **Logs**: the card events.

## 1. What is there today

- **No telemetry export of any kind.** There is no Prometheus, `/metrics`, expvar, statsd or OTel code. The
  `go.opentelemetry.io/otel` modules in `go.mod` (v1.38.0) are indirect only, through
  `github.com/go-openapi/runtime/client`. There is no OTLP exporter dependency.
- **The hook path** (`internal/cli/hook.go:28`, `hookTimeout = time.Second`): `atrium hook` posts `/activity` with
  the fields agent, event, tool, agent_id, agent_type and notification, and ignores the answer. `handleActivity`
  (`internal/daemon/activity.go:765`) writes `{"ok":true}` before it decodes, then runs `go d.onActivity(in)`. That
  updates an in-memory tracker that is never stored, and broadcasts `activity` on the bus. `/session` (3 s),
  `/stop` (2 s) and `/permission` (no timeout, it waits for a decision) are the other hook routes.
- **Permissions**: the `permission` table holds requested_at, decided_at, decision, decided_by and tool. The
  decision chain is `onPermRequest` (`daemon.go:788-940`): replay, queued message, shelved, deploy hold, rule, auto,
  human. **Gate wait is `decided_at - requested_at`.** No field holds it.
- **Usage**: `session_usage`, one row per turn, read from the transcript 1.5 s after Stop
  (`internal/daemon/usage.go`). It holds model, input, output, both cache writes, cache read, context and cause, and
  is broadcast as `usage` with no message text. **Money is not worked out any more** (`usage.go:565`, item 37b). The
  PR runner records `cost_usd` per fork and step from the fork receipts (`prrunner.go:849`).
- **Events**: the `event` table (kinds created, submitted, prompted, perm-requested, perm-decided,
  status-changed, output, notified, launched, exited, compacted), with **pluggable cold sinks fed after commit,
  best-effort** (`internal/store/eventsink.go:15`, setting `event_sink`). The in-memory SSE bus
  (`internal/api/api.go:2381`) fans out without blocking, buffers 32, and drops slow subscribers.
- **Queues**: `message` (pending while `delivered_at` is null), `say` (by state), `relay_outbox`.
- **Room stats**: `internal/roomstats` samples token rates, process and disk every 10 s.
- **Hub**: one upstream SSE per room, git-sync results (`internal/gitsync/hub.go`), and board-wide auto-approve
  (`internal/link/autoapprove.go`). It computes no metrics.
- **Rules on what leaves**:
  - `docs/runtime/scm-design.md:197-221` makes every field exported, local or flagged, and says "No absolute path
    leaves this machine".
  - `docs/rnd/telegram-notify-design.md:20-33` names the card name as "the one real leak". Its sink never gets a
    command, a question, a recap, code, a diff or a path.

## 2. What is emitted, from where

### 2.1 Resource, on everything

`service.name` (`atrium-room` or `atrium-hub`), `service.version`, `atrium.room` (the room's name), `os.type` and
`host.arch`. **Not `host.name`**: the room's name already says which machine, and a hostname is a local fact. The
resource is built by hand. No SDK resource detector runs, because the default detectors add `host.name`,
`process.command_args`, the executable's path and the process owner.

### 2.2 Traces, from the room daemon

| Trace or span | Starts and ends at | Attributes |
| --- | --- | --- |
| **turn** (root, one trace per turn) | `prompted` to the Stop hook (`/stop`) | `atrium.card.id`, `atrium.dept` (enum, section 3), `atrium.harness`, `gen_ai.request.model`, `atrium.turn.cause` (operator, say, restart-wake, resume, subagent, keepalive, unknown), `atrium.turn.end` (needs-input, done, ...) |
| tool call (child) | `tool-start` to `tool-end` | `atrium.tool.name` (a closed set of built-in tools, or `mcp:<server>` for a server in the room's own MCP config, else `other`), `atrium.tool.failed` |
| permission (child) | `perm-requested` to `perm-decided` | `atrium.tool.name`, `atrium.perm.decision` (allow, deny), `atrium.perm.by` as a kind only (rule, auto, human, replay, message, shelved, deploy-hold, hub-auto) |
| subagent (child) | `subagent-start` to `subagent-end` | `atrium.subagent.type`: the agent's name only when it is a file in the room's `~/.claude/agents`, else `other`. Under opencode it is the generated session title, made from the prompt, so it is always `other` |
| compaction (event on the turn) | `compacted` | none |
| **PR run** (root, one trace per run) | the run's start to ready or failed | `atrium.pr.id` (the row id, not the repo or number), `atrium.pr.state`, `atrium.pr.findings` (a count) |
| step (child) | each of fetch, prime, panel, verify, critics, merge, write | `atrium.pr.step`, `atrium.cost.usd` (from receipts), `atrium.pr.step.error` as a class only |
| fork (child of a step) | each fork | `atrium.pr.fork.label` (the reviewer agent's name), `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, cache read and write, turns, `atrium.perm.denials` (a count) |

A card lives for hours or days, so a card is not a trace: backends handle long traces badly. A card's turns join up
through `atrium.card.id`. Token counts arrive 1.5 s after Stop (the usage settle), so the turn span is ended at Stop
and the tokens go on the metric, not the span.

### 2.3 Metrics, from the room daemon

| Metric | Kind | Attributes | Source |
| --- | --- | --- | --- |
| `atrium.tokens` | counter | `atrium.token.kind` (input, output, cache_read, cache_write_5m, cache_write_1h), model, harness, dept, cause | `session_usage` rows (keep-alive refreshes among them, cause `keepalive`), PR fork receipts |
| `atrium.cost.usd` | counter | dept, `atrium.cost.source` (pr-run) | **only where atrium already records dollars** (question 3) |
| `atrium.gate.wait` | histogram, seconds | `atrium.perm.by` kind, decision, tool name | `decided_at - requested_at` |
| `atrium.perm.decisions` | counter | `atrium.perm.by` kind, decision | `perm-decided` |
| `atrium.turn.duration` | histogram, seconds | harness, dept, cause | the turn span |
| `atrium.tool.duration` | histogram, seconds | tool name | the tool span |
| `atrium.cards` | gauge | status, harness, dept | the task table, sampled |
| `atrium.context.pct` | gauge | `atrium.card.id` | the statusline `/telemetry` post, at most once in 2 s per session |
| `atrium.messages.pending` | gauge | none | `message` rows with no `delivered_at`: the queue depth |
| `atrium.says` | counter | state (sent, queued, delivered, held, refused, ...) | `say` transitions |
| `atrium.relay.outbox` | gauge | none | `relay_outbox` depth |
| `atrium.room.cpu`, `.memory`, `.disk` | gauge | none | `internal/roomstats`, every 10 s |
| `atrium.otel.exported`, `atrium.otel.dropped` | counter | signal (trace, metric, log) | the exporter itself |

### 2.4 Logs, from the room daemon

The card events as OTel log records, through a new cold sink, `otel`, beside `db` and `file` in `event_sink`:
created, launched, status-changed (`from`, `to`), exited, compacted, notified (`held` true or false). Each record
carries the card id and the kind, and only the payload fields the allowlist marks. `output` and `submitted` are
**never exported**, because they carry text.

### 2.5 From the hub

The hub exports only what it alone knows. Each room exports its own data, so nothing is counted twice.
- `atrium.hub.rooms` (gauge by state).
- `atrium.hub.gitsync` (counter by result: ok, behind, failed, absent).
- Hub auto-approve decisions (counter).
- Room attach and detach (log records).

### 2.6 Not from the hooks

The hook processes export nothing. They are short-lived, they have a 1 s budget, and they would each need the
endpoint. Everything a hook knows reaches the daemon already.

## 3. What never leaves the machine

**The allowlist is the rule, for fields and for values.** One table in code (`internal/otelx/fields.go`) names every
attribute that may be exported, as in `scm-design.md`'s per-field rule. An attribute not in the table is dropped at
the exporter, and a test fails when a new field reaches the exporter unmarked.

**Naming the field is not enough**, because the collector forwards to third parties, and any free-text value is a
way out. So every field in the table also has a value class, enforced at the exporter and tested:

| Class | Rule | Fields |
| --- | --- | --- |
| enum | one of a fixed list in code, else `other` | dept (the departments configured on the room, since a `dept:` tag is free text set by agents and intake, `usage.go:173-180`), cause, status, decision, `perm.by`, token kind, step, state, error class, built-in tool names, harness ids |
| count | a non-negative number | tokens, turns, findings, denials, durations, gauges, cost |
| id | a ULID or UUID shape, else dropped | `atrium.card.id`, `atrium.pr.id` |
| name | `[A-Za-z0-9_.:-]{1,64}`, from operator-side sources only (the room's agent files, MCP config, harness rows, recipe panel), else `other` | subagent type, `mcp:<server>`, fork label, model |

A value that fails its class becomes `other`, or is dropped for an id, and `atrium.otel.redacted` counts it.

**The SDK has inputs of its own, and none of them are used:**
- The OTLP exporters read `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_EXPORTER_OTLP_ENDPOINT` and the rest of `OTEL_*` from
  the environment by default. Atrium builds them with explicit options only, clears every `OTEL_*` variable before
  it builds them, and a test sets `OTEL_EXPORTER_OTLP_HEADERS` and checks that no header is sent.
- No resource detectors (2.1).
- No `RecordError`, which would attach a raw message and a stack. A span's status is set from the error class only.
- No log `Body` taken from an event payload. A log record carries only allowlisted attributes.
- No auto-instrumentation: no `otelhttp`, no `otelsql`, no global propagator. Every attribute is set by the exporter
  from the table.

Never in the table:

| Never exported | Why |
| --- | --- |
| prompt text, reply text, `last_message`, `output` and `submitted` event payloads | agent traffic |
| tool input: a Bash command, a file path, an edit, MCP arguments; permission `command` and `details` | code, paths and secrets pass through them |
| any path: cwd, worktree, transcript, run folder, repo checkout | "No absolute path leaves this machine" (scm-design:221) |
| card title, why, alias, recap, ask, note, brief | text written by people and agents. The telegram design calls the card name "the one real leak" |
| session ids and resume ids | local handles to transcripts |
| source items, intake URLs, PR titles, repo names and PR numbers, findings | what a source or card holds |
| env names and values, launch args | can hold secrets |
| raw error messages | they carry paths and arguments. Exported as a class (`timeout`, `exit`, `budget`) |

**Tested with sentinels.** O1's acceptance seeds a sentinel string into a prompt, a Bash command, a path, a title,
an alias, a `dept:` tag and an opencode session title. It runs a day of normal work against a collector with a file
exporter, and greps the output for each sentinel, for `/Users/` and `C:\`, and for the machine's user name and
hostname. Any hit fails the stage.

## 4. Export, and the hot path

- **Transport**: OTLP over HTTP (protobuf) to `otel_endpoint`, using the official Go SDK (`go.opentelemetry.io/otel`
  `sdk`, `otlptracehttp`, `otlpmetrichttp`, `otlploghttp`). The core modules are already in `go.mod` indirectly.
  The exporters are new, a few MB of binary. Plain http to a collector on localhost or the LAN, or https to the
  collector's own certificate. **No headers setting, no token, no key.** A collector that needs a vendor key holds it
  in its own config or env.
- **Off the hot path, by construction:**
  - Hooks and their routes do not change. Nothing new runs between a hook's POST and the daemon's `{"ok":true}`.
  - The exporter reads from two places that are already after the fact:
    - a subscriber on the in-memory bus, which never blocks a publisher and drops a slow subscriber. When the bus
      drops the exporter, it resubscribes, closes its open spans with status `gap`, and counts the gap in
      `atrium.otel.gaps`;
    - the `otel` cold sink. **This one is not off the path by itself**: a cold sink's `Append` runs inline on the
      writer's goroutine after commit (`internal/store/tasks.go:1614-1625`), inside `/permission` among others. So the
      `otel` sink does one non-blocking enqueue onto a bounded channel and returns. A full channel is a
      `dropped++`, never a wait.
  - Pairing state is bounded. A start waiting for its end (a turn, a tool call, a permission, a subagent) is held in a
    map capped at 4,096 entries. An entry older than its TTL (1 hour for a turn, 10 minutes for a tool call) is ended
    with status `unfinished` and counted. At the cap the oldest is ended the same way.
  - Spans are built from the events' own timestamps, so a late event still gives a right duration.
  - Each signal has one bounded queue (the SDK's batch processor, 2048 items, never blocking when full) and one
    goroutine that batches every 5 s or 512 items. A send has a 5 s timeout and one retry, then the batch is dropped
    and `atrium.otel.dropped` counts it. Memory is bounded by the queues.
- **Acceptance for the hot path** (O1): the `/activity`, `/permission` decide and `/stop` latencies are measured
  over 1,000 posts each, with the exporter off and on, and the p99s agree within noise. Then two failures:
  - **the collector is killed**: posts keep their latency, and `atrium.otel.dropped` rises;
  - **a collector that accepts and never answers**: posts keep their latency, the sink's channel fills and drops,
    and the daemon's goroutine count stays flat.

**The setting.** `otel_endpoint` is a room setting (`setting` table, `store/settings.go`, one field in
`/v1/settings`) and a hub setting (`hub_setting`). It is read per batch, not at open, so a change applies without a
restart. Empty is off. The settings page shows one field on each.

## 5. Sinks, through the collector

The collector is the operator's: an OpenTelemetry Collector (or Grafana Alloy) on the hub machine or each room. A
`docs/otel-collector.md` recipe (O4) gives one config with a receiver for atrium and one exporter block per sink. Each
block takes its key from the collector's env, never atrium's:
- **LangSmith**: OTLP to `https://api.smith.langchain.com/otel`, with the `x-api-key` header set in the collector.
- **Grafana Tempo, Mimir or Loki**: OTLP to the stack's endpoints.
- **Honeycomb**: OTLP with the `x-honeycomb-team` header in the collector.
- **Jaeger**: OTLP straight in, for traces only.

## 6. Stages

### O1. The room exporter and the metrics. @runtime. About 2 days.

- The `otel_endpoint` setting, the bus subscriber, the allowlist table with its value classes, and their tests.
- The SDK built by hand: explicit exporter options, `OTEL_*` cleared, a hand-built resource, no detectors and no
  auto-instrumentation.
- The metrics of 2.3, and the exporter's own counters (exported, dropped, redacted, gaps, unfinished).
- **Acceptance**:
  - Empty means no goroutine and no connection, checked by a test.
  - A local collector with a debug exporter receives every metric.
  - `OTEL_EXPORTER_OTLP_HEADERS` set in the daemon's env sends no header.
  - A `dept:` tag and a subagent name outside their classes are exported as `other`.
  - The hot-path measurements of section 4, both failure cases included.
  - The sentinel grep of section 3 passes.

Useful alone: dashboards for tokens, gate wait and queue depth.

### O2. Traces and logs. @runtime. About 2 days. After O1.

- The turn, tool, permission and subagent spans, the PR run trace, and the `otel` event sink for logs.
- **Acceptance:** one turn with a Bash call and a permission shows in Jaeger as a turn with two children and the
  right gate wait. A 378-sized PR run shows its steps and forks with tokens. The sentinel grep passes again.

### O3. The hub. @fabric. About 1 day. After O1.

- The hub metrics and logs of 2.5.
- **Acceptance:** a room detached and reattached shows in the hub's logs and gauges.

### O4. The collector recipe and the settings field. @rnd for the recipe, @ui for the field. About half a day.

- `docs/otel-collector.md` with the four sinks of section 5.
- One field on the room and hub settings pages.
- **Acceptance:** the recipe brings atrium's traces up in Jaeger and in LangSmith from one config, with the
  LangSmith key only in the collector's env.

## 7. Questions for clint

1. **Card identity.** Export the card id and its dept only, or the alias too? The alias reads well in a dashboard,
   but it is the leak the telegram design names. **Default: id and dept only.**
2. **Room to collector.** Each room sends straight to the collector, so each room must reach it, over the LAN for
   sg3 and m1mini. Or the rooms hand their batches to the hub, which forwards them, so only the hub needs a route.
   **Default: straight to the collector.** Hub forwarding can come later.
3. **Dollars.** Export tokens everywhere, and dollars only where atrium already records them (PR runs, from the fork
   receipts). Or price every turn from the existing table, which item 37 hid on purpose? **Default: tokens, plus the
   dollars already recorded.**
4. **Auth to the collector.** Endpoint only, with no headers setting, so a collector that needs a key holds it and
   atrium never does. Or allow a headers setting? **Default: endpoint only.** A headers setting would be a place for
   a third-party key.
