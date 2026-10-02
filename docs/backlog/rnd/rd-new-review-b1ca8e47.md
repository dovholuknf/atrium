# Review: OpenTelemetry export design (b1ca8e47, m1mini, 2026-10-02): HOLD

`docs/rnd/otel-export-design.md`, clint's Q4 of the OpenWiki spike. Range claude/landing ac4b15a9..b1ca8e47. In it,
f174996f folds my lean-card low into the OpenWiki spike as written (fine), and 250f84e2 is the exempt queue commit.

The shape is right. OTLP goes to the operator's collector, which holds every vendor key. It is off by default,
exports from after-the-fact feeds, uses bounded queues that drop, has an allowlist with a test, and has a sentinel
grep plus a p99 comparison for acceptance. The never-list in section 3 is complete for the fields it names. It holds
on three gaps in the mechanisms the three questions asked about.

## Medium 1: the allowlist names fields and does not constrain their values (question 3)

A field on the list leaves with whatever value it holds. Since the collector forwards to third parties (LangSmith,
Honeycomb), any free-text value on the list is a way out for text, including one a prompt-injected agent writes on
purpose. Checked against the code:
- **`atrium.dept` is free text an agent sets.** It comes from the card's `dept:` tag (daemon/usage.go:173-180). Tags
  are set by agents through MCP and by intake sources, and `NormalizeTags` only lowercases and strips commas
  (store/tasks.go:1467). The tag `dept:<anything>` leaves as a metric label on every token count.
- **`atrium.subagent.type` is free text under opencode.** Claude Code's `agent_type` is the agent's defined name.
  But the opencode plugin sends `agent_type: info.title || "task"` (scripts/opencode/atrium.js, `session.created`),
  which is opencode's generated session title, made from the prompt.
- **`atrium.tool.name`** is a harness's tool id, or `mcp__<server>__<tool>`. The server name is operator config, and
  the tool name is whatever the MCP server advertises. The opencode plugin passes unmapped names through raw. Low
  risk, but it is not a closed set.
- **`atrium.pr.fork.label` is fine.** It is the panel agent's file name, a critic's name, `verify-N`, `merge` or
  `merge-resend` (prrunner.go:1033,1165,1230,1309,1368). All are operator side. `gen_ai.request.model` and
  `atrium.room` are operator config.

Fix: the table gives each field a value class, and the exporter enforces it. The classes:
- **enum:** a closed set; anything else exports as `other`.
- **count:** a number.
- **id:** must match the ULID or UUID shape, or it is dropped.
- **name:** `[A-Za-z0-9_.:-]{1,64}`, from an operator-side source only, else `other`.

Under that, `dept` is either an enum of the configured departments or dropped, and `subagent.type` is a name taken
only from Claude Code (`other` from opencode). The unmarked-field test gains a value test. A field whose value does
not fit its class must not reach the exporter. The sentinel run seeds a `dept:` tag and an opencode session title
too.

## Medium 2: the SDK brings its own inputs, so "nowhere to put a key" is not yet true

- **Headers from the environment.** The Go OTLP exporters read `OTEL_EXPORTER_OTLP_HEADERS` (and the per-signal
  `…_TRACES_HEADERS` and the rest) from the environment by default, merged into every request. So a key can sit in
  the daemon's env and atrium sends it. Build the exporters with explicit options only, and ignore or clear every
  `OTEL_*` variable when building, or say plainly that an env header is the operator's choice, not atrium's. Then
  test that a set `OTEL_EXPORTER_OTLP_HEADERS` sends nothing.
- **Resource detectors.** `resource.Default()`, `WithFromEnv`, `WithProcess*` and `WithHost` add
  `OTEL_RESOURCE_ATTRIBUTES`, `host.name`, `process.command_args`, `process.executable.path` and `process.owner`: a
  hostname, paths and the user name. Build the resource by hand from section 2.1's list only.
- **Span status and errors.** `span.RecordError` and `SetStatus(codes.Error, msg)` export the raw message, and
  `RecordError` adds a stack trace. The status description is the error class, and `RecordError` is never called.
- **Log record bodies.** A log record's Body is never set from an event payload. Only allowlisted attributes.
- **No auto-instrumentation.** No `otelhttp` and no `otelsql`. They export URL paths (`/v1/tasks/{id}/files?path=…`),
  query strings, client addresses, user agents and SQL text.

## Medium 3: section 4's hot-path claim, true for the bus and not for the sink (question 2)

- **The cold sink runs on the writer's goroutine.** Its `Append` is called inline after commit
  (store/tasks.go:1614-1625: `fan()` or `tx.afterCommit(fan)` on the same goroutine). "Best effort by contract"
  means it ignores the error. It does not mean it is off the path. Events are written from the request handlers, so
  an `otel` sink's Append runs inside `/permission`'s and the status change's handling. The sink must be a
  non-blocking enqueue (a channel send with `default: dropped++`) and nothing else, never an SDK call that can take
  a lock or allocate a batch. Section 4 must say so, and O1's p99 test must run with a collector that accepts
  connections and never answers, not only a dead one.
- **Span assembly is state the queues do not bound.** Spans are paired from start and end events: tool-start and
  tool-end, perm-requested and perm-decided, subagent-start and subagent-end, prompted and Stop. A missing end
  leaves an open span in memory: a hook killed by its 1 s budget, a crash, or a subscriber the bus dropped. "Memory
  is bounded by the queues" does not cover that map. Cap open spans per card and in total, end them as
  `unfinished` after a TTL (a tool after 1 h, a turn at the next `prompted`), and count what is evicted.
- **A dropped bus subscriber.** The bus drops a slow subscriber (api.go:2381), and it does not just lose a message.
  The exporter must notice, resubscribe, close its open spans as `gap`, and count it.
- Fine as is: the metric gauges (cards, pending messages, outbox) query the store on the reader's own goroutine,
  under WAL. `guard` retries; it is not a global lock (store.go:765).

## Answer on section 3's never-list

Complete for the fields it names. Add the SDK's own sources from Medium 2 (env, the resource, error text, log
bodies, auto-instrumentation). Add, as a rule, that a value class is part of the allowlist, from Medium 1. And make
the sentinel grep also look for the operator's user name and the machine's hostname, which the detectors would add.

## Verdict

HOLD: the value classes (M1), the SDK inputs closed off (M2), and the sink as a non-blocking enqueue with bounded
span state (M3). A re-read covers sections 2.1, 2.2, 3 and 4 and O1's acceptance. doc-ok on OK.

Quality: careful. The fields are traced to their sources, and keeping the hooks untouched is the right instinct.
The gaps are in what the allowlist checks (the name, not the value) and in what the SDK does on its own.
