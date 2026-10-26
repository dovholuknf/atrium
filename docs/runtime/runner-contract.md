# Runner contract v1

What a runner's hooks or plugin say to a room, and what a room says back. Atrium owns this contract and a kit that
tests against it. It does not own runners. The design is `docs/rnd/runner-adapters-design.md`, and `hooks.md` is the
rules every hook follows.

An adapter is whatever makes these calls for a runner: atrium's own hooks for claude and codex, or a plugin that
someone else writes for opencode, pi or hermes.

## Where the room is

The agent listener's address, first match wins:

1. The file `ATRIUM_LOCATION` names. Its `agent` field is the address.
2. `ATRIUM_HUB_URL`.
3. `http://127.0.0.1:7777`.

The card comes from `ATRIUM_TASK_ID`, which atrium sets on every runner it launches. `agent` is `ATRIUM_AGENT_NAME`
for a session atrium did not launch, else the base name of the working directory. Send both when you have both.

## Versioning

- Every body may carry `contract`, the integer version the sender speaks. Absent means 1.
- `GET /contract` answers `{"versions": [1]}`. A room answers every version it lists.
- v1 only ever gains optional fields. An absent field has a default, and a field a room does not know is ignored.
  A change to what an answer means is v2.
- `POST /session` with `event: start` may carry `features`, the calls the adapter makes: `session`, `activity`,
  `permission`, `stop`, `finish`. It is a claim. The room checks it against the calls that arrive.

## The calls

Every body and answer has a JSON Schema in `docs/runtime/contract/`, named `<call>.schema.json` and
`<call>-answer.schema.json`. Times are the longest an adapter may wait before it carries on as if the room were not
there.

| Call | When | Answer | Wait | Room not there |
| --- | --- | --- | --- | --- |
| `POST /session` | session start, end, compact | `{ok, context?}` | 3 s | go on. Give the model `context` if there is one |
| `POST /activity` | every tool start and end, prompt, subagent, waiting | ignored | 1 s | ignore every failure, never retry |
| `GET /gate?agent=` | before a permission call | `{gate: bool}` | 2 s | treat as not gated |
| `POST /permission` | before a tool runs | `{decision: approve or block, reason, command}` | none, a long poll | say nothing, so the runner's own approval runs |
| `POST /stop` | end of a turn | `{}` or `{decision: block, reason}` | 2 s | let the turn end |
| `POST /finish` | the agent says it is done | `{recorded, task_id, status}` | none | the one error that is shown |

Two answers carry queued messages for the model. A `block` from `/permission` is a message and not a refusal, so word
it to the model as someone talking to it. A `block` from `/stop` has a `reason` that the runner must give the model as a
new turn. Never block a second time on a turn a `/stop` block already continued (`stop_hook_active`).

## The rule that matters most: fail open

A hook never fails a session. Whatever the room does, the adapter:

- exits 0 and keeps the runner going,
- makes one attempt per call inside the wait above, except that `/permission` may repeat the same `dedup_key` while a
  room restarts,
- prints nothing for "no answer", so the runner's own flow runs,
- takes a refused connection, a timeout, a non-2xx status and a body that is not the answer's shape all as "the room is
  not there".

The room's side of the same rule is in `internal/daemon/AGENTS.md`, resilience 2 and 3.

## The conformance kit

```
atrium adapter test --cmd "<command that drives one scripted session>"
atrium adapter test --runner claude
```

The kit starts a fake room on a loopback port, points `ATRIUM_LOCATION` at it and runs the adapter four times:

| Scenario | The fake room |
| --- | --- |
| `ok` | answers every call as above, blocking the permission call and the turn end |
| `refuse` | drops every connection |
| `hang` | never answers, except `/permission`, which is a long poll by design |
| `garbage` | answers 200 with text that is not JSON |

The adapter passes when every run exits 0 inside `--timeout` (default 30 s), every body it sent matches its schema, and
every feature it claimed on session start was called. It prints the features it was seen to use.

`--cmd` is a shell command that plays one session through the runner: a session start, a tool call that asks
permission, a turn end and a session end. It gets `ATRIUM_ADAPTER_SCENARIO`, `ATRIUM_TASK_ID=kit-card` and
`ATRIUM_AGENT_NAME=kit-agent`, and `ATRIUM_PERM_GATE=on`. `--runner claude` drives atrium's own claude hooks and is run
in CI as the reference.
