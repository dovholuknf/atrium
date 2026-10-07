# bb: what it is, and what atrium should do about it

Status: research, 2026-09-29. A source read of bb, with what atrium could borrow from it ranked in section 5.

Standing reference. Written from the source code, not from the README. The survey of the wider field is
`docs/rnd/competitors.md`, which is where the extensibility question lives. This file is the depth on the one tool that
turned out to be large enough to need its own.

Source read: `github.com/get-bb/bb`, MIT, cloned shallow to `D:/tmp/bb` on 2026-09-29 and not committed here. Every
bb path below is relative to that clone. Every atrium path is relative to this repo. Nothing was executed. Every claim
is a static read, and where a claim rests on a README, an overview file or a design note rather than on code that was
traced, it says so.

Adoption evidence, from the GitHub API on the same day: about 3,990 stars, 565 forks, 513 open issues, created
2026-02-24, last push 2026-09-29, latest desktop release `desktop-v0.44.0` on 2026-09-25. Seven months old and moving
daily. The 513 open issues are a count, not something read.

## 1. What bb is

"An agentic IDE that builds itself" (`README.md:14`). A TypeScript monorepo (pnpm and turbo) with about forty packages
(`packages/`), nine apps (`apps/`) and thirty-nine bundled plugins (`plugins/`). The runtime has four pieces
(`docs/system-overview.md:5-10`): a **server** holding all state in SQLite and speaking HTTP plus WebSocket, a **host
daemon** on each machine that provisions workspaces and runs provider processes, a **web app** (also wrapped as an
Electron desktop app and an Expo phone client), and a **`bb` CLI** that is meant to be as capable as the UI.

The unit of work is a **thread**. A thread has a provider, a model, an environment (a workspace bound to a host),
an append-only event stream, and optionally a parent. Managers were removed: `apps/cli/src/commands/manager.ts:4-16`
now only prints "Manager threads were replaced by parent threads". A thread can be spawned from a running thread
with `bb thread spawn --parent-self` (`apps/cli/src/commands/thread/spawn.ts:101-120`), and there is an immutable
`lifecycleOwnerThreadId` so archiving or deleting an owner cascades to its dependents
(`docs/system-overview.md:28-67`).

The most important fact, and the one that decides almost every comparison with atrium: **bb is the client, not a
supervisor of a terminal.** The Claude Code provider is a plugin whose bridge process calls `query()` from
`@anthropic-ai/claude-agent-sdk` (`plugins/provider-claude-code/src/bridge/sdk-session.ts:293`, dependency at
`package.json:42`) and passes `canUseTool`, `hooks`, `allowedTools` and `permissionMode` straight into it
(`sdk-session.ts:258-268`). The plugin overview says it "drives the Claude Code CLI", which is true in the sense that
the SDK spawns it, but there is no interactive TUI that a human types into and no pseudo terminal in this path. This
is the same architecture as Charon (`docs/rnd/charon.md` section 1) and the opposite of atrium's.

Codex, Pi and any ACP agent (Cursor, OpenCode, Grok, Hermes, or one the user names in a setting) are the other
providers, each a plugin with a bridge speaking a line-JSON protocol
(`plugins/provider-claude-code/src/bridge/bridge.ts:1984-2081` handles `thread/start`, `thread/resume`, `thread/fork`,
`turn/start`, `turn/steer`, `thread/stop`).

## 2. Strengths

### Almost everything is a plugin, including the providers

`docs/provider-plugin-api.md:16-25` states the principle: zero first-party privilege. First-party providers use only
the public API, and "every special case is a public primitive or is deleted". The test that backs it is real: a check
called `check:plugin-forks` copies each listed built-in plugin out of the repository and confirms it installs,
typechecks, tests and builds against only the published SDK (`docs/forkable-plugins.md:25-33`). Thirty-nine bundled
plugins, from the task tracker and the workflow engine down to keep-awake and custom instructions, sit on that
surface. This is the strongest evidence in the tree that the extension surface is a real one and not a diagram.

The size of a plugin is also honest. `plugins/custom-instructions/server.ts` is 121 lines: one declared setting, one
`bb.agents.contributeInstructions` call, one `bb.cli.register` with `get`, `set` and `clear`. Its `package.json`
carries the whole manifest in a `bb` key (`name`, `description`, `branding`, `server: "./server.ts"`) and the
`engines.bbPluginSdk` floor. Nothing else is needed to ship.

### The plugin API, as a list of what an extension can do

From `packages/plugin-sdk/src/backend-contract.ts:2056-2139` (`BbPluginApi`), read as contract and not as
implementation:

| Member | What it lets a plugin do |
| --- | --- |
| `settings`, `storage` | Declarative settings rendered by the host, a namespaced KV store and a per-plugin database |
| `http`, `rpc`, `realtime` | Routes and RPC methods under `/api/v1/plugins/<id>/`, and pushes to connected frontends |
| `background` | Long-lived services and cron schedules |
| `cli` | A subcommand of the agent-facing `bb` CLI, with typed options (`defineCli`, `cliCommand`) |
| `agents` | `registerTool` (a native tool the model can call), `configure` (pick tools, skills and instructions per thread), `contributeInstructions` |
| `providers` | Register an agent provider |
| `ui` | `requestInput` (a form the user fills in, which blocks the tool call), and mention providers for `@ # $ ! ~` triggers |
| `events` | Announcements core makes: `thread.created`, `thread.idle`, `thread.failed`, `interaction.pending`, `message.queued`, `message.dispatched`, turn failed, and others (`:267-348`). Return value ignored |
| `experimental_hooks` | Questions core asks and acts on. Today exactly one, `message.dispatch` |
| `experimental_environments`, `experimental_machines` | Places a thread can run, and machine providers that create hosts |
| `experimental_aiServices` | Who writes thread titles, commit messages and voice transcripts |
| `sdk` | The full bb SDK, so a plugin can spawn, fork, send to and archive threads |
| `onInstall`, `onDispose` | Lifecycle |

Beside the backend there is a frontend contract of 3,386 lines (`app-contract.ts`) covering nav panels, thread panels,
composer customization, slots, themes and content scripts.

### `message.dispatch`: a real veto point, and the way it is fenced

This is the single most transferable idea in the tree. `PluginHookSignatures` has one hook,
`message.dispatch`, "THE admission checkpoint, run identically for a thread's first message, a follow-up, a steer, a
retry, and every re-attempt a drain makes" (`backend-contract.ts:699-711`). The context carries the thread, project,
environment, host, the kind of environment about to be provisioned, the input, the requested execution, who initiated
it (`user`, `agent`, `system`), the sending thread, and every queued row being retried (`:612-691`). A handler returns
proceed, wait (with a reason and an optional `sendAt`) or reject.

The fencing is the part worth reading (`:721-785`):

- It is a question, kept in a different namespace from events, and the comment names the git split: pre-commit versus
  post-commit.
- Handlers run as a deterministic chain in plugin install order. A `reject` short-circuits. `wait` decisions are
  collected across the pass, the first plugin owns the row's `waitingOn`, and the rest have their reasons appended, so
  one decision produces one card rather than one per plugin.
- **Fail closed, time boxed**: a handler that throws or exceeds 10 seconds fails the attempt with the plugin named.
  The doc tells authors to decide in milliseconds and to return `wait` with a `sendAt` if the answer needs work.
- The pass runs under one server-wide lock so a counting handler cannot race another attempt.
- A user's explicit "Send now" bypasses it "by design", because a policy that could veto its own override would not be
  one.
- `recheck(hook)` asks core to re-run every plugin-held row, coalesced, fire and forget.

The bundled `concurrency-limit` plugin (`plugins/concurrency-limit/host.ts`, `limits.ts`) is the worked example: a
global and per-host cap on live threads, implemented entirely as a `message.dispatch` handler. It was not read past its
overview and file list, so how it counts is unverified.

### Per-thread agent configuration from context

`bb.agents.configure` runs at `thread.start` and `turn.submit` with a frozen context (`PluginAgentConfigurationContext`,
`backend-contract.ts:1137-1192`: the thread's plugin metadata, project, environment, host, provider capabilities and
origin) and returns `{ tools, skills, instructions }`. Tool schemas can be narrowed per resolution. Failure is
contained: a throw, an unknown id or more than 256 ids "fails closed for this plugin only" (`:1614-1620`). Thread
metadata written under a plugin's namespace is documented as untrusted input, because "any API client, another plugin,
or the thread's own agent can write it" (`:1141-1144`). That is a sentence worth copying into any atrium extension
doc.

### Workflows: orchestration as a script

`plugins/workflows` (off by default) runs a JavaScript file that fans work out to agent threads. The overview says
the script runs in a sandbox with no file, shell, network or clock access, that calls to `agent(...)` start ordinary
worker threads with their usual tools and permissions, and that "successful calls are cached, so a resumed or
restarted run replays them and continues live from the first change"
(`plugins/workflows/PLUGIN_OVERVIEW.md:13`). The sandbox is QuickJS compiled to WebAssembly
(`plugins/workflows/src/runtime.ts:1-8`, `quickjs-emscripten-core`). That the runtime is QuickJS is traced. The replay
claim rests on the overview and on `src/cache.ts` existing, and the replay logic itself was not read.

The authoring reference is where a "director of review" already exists as a pattern
(`plugins/workflows/skills/workflows/references/orchestration.md`): `pipeline()` by default, a barrier only when a
stage needs every prior result, adversarial verify with N skeptics who each try to refute a finding, perspective-diverse
verification, a judge panel that scores N independent attempts and grafts from the runners-up, loop-until-dry
discovery, and a completeness critic. It also says "no silent caps: `log()` what was dropped". Agents start runs with a
`bb_workflow_run` tool, a run has a status card and a "Workflow run" panel, and the completion message is delivered to
the thread that started it. Worker threads are hidden from the sidebar and archived after a retention period.

### A packaged, distributable, reviewed extension story

`docs/plugin-marketplace-plan.md` (status: "draft for review", so a plan and not shipped behaviour, and marked
"AGENT GENERATED" at the foot) describes a strict-schema marketplace manifest that anyone can host, with entries
sourced from npm or git (including a subdirectory of a multi-plugin repository). The parts that show real thought:

- Git sources can track a semver range over tags, record the exact tag and commit, and **refuse loudly if the tag
  later points at a different commit** (`:158-169`), which the plan calls the `go.sum` lesson.
- A refresh "never installs, updates, or runs code". Applying an update is manual, staged and rollback protected.
- A listing declares no compatibility. The plugin's own `package.json` carries `engines.bb` and `engines.bbPluginSdk`
  and the install is refused there, because "a listing's copy of a range is a second source of truth" (`:90-95`).
- A repository can hold a `.bb/plugins.json` collection manifest for nested plugins.
- Submission is a pull request against a data-only registry repository, opened by an agent skill using the author's
  own `gh` credentials, so the listing owner is the account that opened the PR.
- Install counts are a sidecar file and never a manifest field, because the manifest is strict.

The CLI side (`apps/cli/src/commands/marketplace.ts`, `plugin.ts`) and `apps/server/src/services/plugins/`
(`install-sources.ts`, `update-resolver.ts`, `plugin-updates.ts`, `plugin-artifact-gc.ts`) exist. Whether the shipped
behaviour matches the plan was not checked, and `docs/configuration.md` already documents `bb marketplace add` and a
reserved `bb-community` marketplace, so at least part of it is live.

### A test kit shipped to plugin authors

`@get-bb/plugin-sdk/testing` and `/testing/app` (`packages/plugin-sdk/README.md:104-190`) give an external author a
fake host: `createFakePluginHost`, deterministic fixtures (`makeMessageDispatchHookContext`, `makeThreadResponse`,
`makeQueueEntry`, `makeTurnFailedEvent`), atomic reload that preserves settings and storage, and a jsdom `renderSlot`
for frontend slots. The README states its own fidelity boundaries honestly: HTTP does not enforce bb's authentication,
schedules run only when driven, and "use a live BB test for those boundaries".

### Machine permission ceiling

Each machine has `maxPermissionMode` (default `full`) and "the server resolves every thread on that machine down to
the ceiling", so a sandbox host can stay at Full Access while a personal one stays at Accept Edits
(`docs/configuration.md:751-764`). Deliberately absent from the SDK and CLI, settable only by an owner session on the
machine page. It is a clamp on the host and not a rule about the agent, which is the right layer for it.

### Smaller things that are right

- **Archive with an undo grace.** Archiving owes a thread `ARCHIVE_UNDO_GRACE_MS` before teardown, so Undo costs
  nothing, and a mid-turn thread keeps running inside the window (`docs/system-overview.md:47-56`).
- **Child outcomes are batched into the parent.** `child-thread-notifications.ts` batches child turn results over a
  2 second window, truncates each excerpt to 4,000 characters with a visible marker, and adds guidance when a workflow
  the child started is still running so that its output "is not its final result" (`:78-91`).
- **Precedence is written down.** `docs/configuration.md:63-71`: flags, then persisted config, then environment, then
  defaults. Startup-only keys are enumerated (`:94-108`).
- **Optimistic concurrency on UI preferences.** Writes name the revision they expect and get `409` on conflict
  (`docs/configuration.md:786-792`).
- **A code review guide with simplicity red flags** (`docs/CODE_REVIEW.md:34-45`): new registries, coordinators,
  managers and abstractions with one real caller are findings. Worth reading against the SDK's own size.

## 3. Trade-offs

### The API is unauthenticated, and documented as such

The README says of the remote modes: "The server API is unauthenticated and permits command execution and file reads,
so use this only behind a trusted network boundary" (`README.md:153-155`, repeated at `:165-168`). Direct mode on the
phone is "unauthenticated, the same trust model as the browser PWA on a LAN" (`docs/platform-support.md:45-54`). The
posture matches atrium's loopback rule, but bb ships `--server-bind-host 0.0.0.0` as a supported flag and a "bb
connect" cloud pairing service beside it, so the line atrium holds (an overlay does the reaching, atrium never
listens beyond loopback) is not the line bb holds.

### Plugins are trusted code in the server process

`apps/server/src/services/plugins/plugin-runtime.ts:20` imports `createJiti`: a plugin's `server.ts` is loaded
in-process by the server. The manifest has no permission or capability declaration (a grep for
`capabilit|permissions|trusted|sandbox` in `manifest.ts` returned nothing), and the SDK README says of content scripts
that they "are trusted same-origin page code, not a sandbox" (`packages/plugin-sdk/README.md:98-99`). A plugin
therefore has the server's own authority, including the `sdk` handle that can spawn and message any thread. The
mitigations that exist are review at listing time, a safe mode (`getPluginSafeMode` is imported at
`plugin-runtime.ts:43`, its behaviour not read), and fail-closed containment of individual registrations. That is the npm trust model and the
plan says so (`plugin-marketplace-plan.md:387-390`). It is a reasonable choice for an IDE. A tool that holds a
permission gate needs plugins with less authority than the gate.

### Permission control is by mode and, as read, per session

Three modes, `accept-edits`, `auto`, `full` (`PluginProviderPermissionMode`, `backend-contract.ts:1230`). The Claude
bridge's `canUseTool` (`bridge.ts:1870-1981`) checks an in-memory `sessionPermissionGrants` array on the thread
attachment (`:252`, `:930`, pushed at `:2502`), then either allows, auto-denies by policy, or forwards the request to
the human as a pending interaction. `bypassPermissions` allows everything (`:1947-1953`). Whether those grants survive
a restart was not traced. What was not found anywhere in the Claude plugin is a durable, matchable rule set of the
`perm_rule` kind: prefix, glob or folder, most specific wins, exportable. If one exists it is not in the provider
bridge. Compare `docs/rnd/charon.md` section 3, which found the same shape one layer up.

`approvalEnforcedBy: "provider"` is declared at initialize (`bridge.ts:1995`), which states plainly that the
enforcement is the SDK's and bb presents it.

### Windows runs through WSL2

"Native Windows PowerShell and CMD are not supported" (`README.md:40-42`), and native Windows checkouts are "outside
the support contract" (`docs/platform-support.md:184-187`). Setup hooks are POSIX shell only. Atrium's first platform
is Windows. This is not a defect in bb, it is a reason the two do not compete for the same person today.

### Footprint

About forty packages, `apps/server/src/services/threads/` alone holds more than seventy files, and the system
overview cites migration `0121`. The native add-ons (`better-sqlite3`, `node-pty`, `@parcel/watcher`) break on npm 12
by default and get a dedicated troubleshooting section (`README.md:243-297`). Telemetry is on by default for
production runs (`README.md:83-93`, opt out with `BB_TELEMETRY=false`), though it is anonymous and documented.
Atrium is one static Go binary and a store. That difference is real for the people atrium is for.

### The API is still settling

`experimental_` prefixes are all over the contract, `docs/api_to_audit.md` exists to track why each one is still
experimental, and the SDK has scheduled removals (`provider-bridge-scheduled-removals.test.ts`) and a deprecated
pre-1.0 composer API that "has been removed" (`packages/plugin-sdk/README.md:80-82`). A plugin written today will
likely need changes as the surface settles.

## 4. Where atrium differs

- **Supervising the real terminal.** A human types into the same `claude` the daemon supervises, and the permission
  chain, activity badge and message queue work around that human rather than instead of them. bb has no equivalent
  because it has no terminal. This is atrium's whole premise (`docs/terminal/supervision-design.md`).
- **Durable standing rules with a decision log.** `perm_rule` matching and the recorded `by` on each decision.
- **Auto mode as a paid-for trade.** Last in the chain, recorded under its own name, with a review, versus bb's
  `bypassPermissions` branch.
- **The halt.** Storage failure closes the agent listener and runners park on connection-refused. bb's server is
  "stateless itself; the DB is the source of truth" and no halt behaviour was found. Unverified either way.
- **Loopback, no login, and an overlay to reach it.** Versus a bind-to-all-interfaces flag and a cloud pairing
  service.
- **Windows native, one binary.**
- **Extensions that cannot become the daemon.** See `docs/rnd/competitors.md`: atrium's extension points run commands with
  bounded output, out of process. Nothing a user writes shares the address space of the permission gate.

## 5. Worth borrowing, ranked

The ranked list across every tool is at the end of `docs/rnd/competitors.md`. The bb entries, with the sketch that fits
atrium's design:

### 1. A dispatch admission hook, as an out-of-process command

**Adapt. Keep the fencing and drop the in-process handler.**

What transfers is the contract in section 2: one checkpoint, one context, a three way answer (proceed, wait with a
reason and an optional time, reject), fail closed on timeout, a chain in install order, and an explicit human bypass.
Atrium already has a checkpoint in the right place. `docs/orchestrator/dispatch-queue.md` at 3be95cab and the message
queue (`internal/store/messages.go`, `internal/daemon/messages.go`) hold a prompt for a card until the runner can take
it.

The sketch: a **gate** row, shaped like a `source` (`internal/store/sources.go`), which is "a command on a timer,
shaped like harness because it is the same idea". A gate is a command plus a bound. Before the daemon queues or
delivers a launch, a prompt or a peer message, it runs the gates that match the card's tags, with the context as JSON
on stdin (card, tags, initiator, sender handle, queued rows, requested runner) and reads `{"decision":"proceed"|
"wait"|"reject","reason":"...","until":"..."}` from stdout. Output is bounded while being read, as sources already
do. A gate that fails, times out or prints nonsense is a **wait with the failure as its reason** and never a
proceed, and never a crash, which is bb's fail-closed rule and `docs/archive/architecture-v2.md`'s no-degraded-mode rule
saying the same thing. Three failures switch the gate off with the reason on its row, exactly as sources do
(`CLAUDE.md`, resilience 6). The human's "send now" bypasses it and says so.

It is not in the permission chain. It sits before it, on admission, so `onPermRequest`'s order is untouched.
Cost: medium. Needs: a table, a runner in `internal/daemon` beside `sources.go`, one settings page.

### 2. Per-context agent configuration

**Adapt. Small.** `agents.configure` picks tools, skills and instructions from a frozen context. Atrium's version is
the launch brief: a brief template selected by tag and by runner, so `tags: dept:review` launches with the review
director's brief and skill list and a plain worker does not. The metadata caveat is the part to keep: the card's
attributes can be written by anyone, so the brief must quote them. Cost: small if the brief store exists, and
`docs/runtime/launch-options-design.md` and `docs/runtime/lean-workers-design.md` suggest it is close. Checked against the code on
2026-09-29: a launch carries a `brief` that is written to `BRIEF.md` per launch, and there is no template store keyed
on tags, so this is part of a role (`docs/rnd/competitors.md` section 3.4) and not a small standalone change.

### 3. A pack: a directory that carries an extension

See `docs/rnd/competitors.md` section 3.4 for the recommendation. The bb part is the manifest-in-the-directory shape, the
collection manifest, the semver-over-tags source with a recorded commit, and the moved-tag refusal.

### 4. A test kit

**Adopt the idea, defer the work.** A `fake daemon` that a pack author can run a gate, a source or a brief against
without the real board. Cost: medium, and worth nothing until packs exist.

### 5. Machine ceiling on permissions

**Adapt. Small and cheap.** A per-host or per-room maximum for auto mode and standing-rule application, settable only
from the board. Atrium's auto mode already has a board-wide switch and a per-session switch (`docs/runtime/auto-mode.md`).
A ceiling is a third value that clamps both, and it answers "this box is a shared sandbox, nothing here may
auto-approve". Cost: small.

### 6. Batched child outcomes to the parent

**Adapt. Small.** Atrium has lineage (`docs/rnd/agent-lineage-design.md`) and a peer bus. A director that dispatches four
workers gets four separate queued messages as each finishes. bb's 2 second batch with a truncated excerpt and the
"this is not the final result" guidance (`child-thread-notifications.ts:78-91`) is one message. The truncation marker
and the running-work guidance are the pieces worth copying. Cost: small.

### 7. Workflow scripts

**Do not port the runtime. Take the patterns.** Adversarial verify, a judge panel, loop-until-dry and a completeness
critic are prompts and control flow, not a QuickJS engine. They belong in a director brief and in the review panel's
instructions, and `orchestration.md` is good source material for them. Embedding a JavaScript runtime in a Go daemon
to run them would add a second language to a project that chose one binary on purpose. Cost of the patterns: none,
they are text.

## 6. Not a fit for atrium

- **The in-process plugin runtime.** Third party code sharing an address space with the permission gate is the one
  thing atrium's design rules out. Section 4.
- **A cloud pairing service and telemetry.** Out of scope by `CLAUDE.md` ("Authentication", "Cross-machine
  aggregation").
- **Replacing the terminal with an SDK client.** It is what makes bb able to render every provider the same way and
  it is exactly what atrium is not. `docs/rnd/charon.md` reached the same conclusion.
- **Four frontends over one contract.** Web, desktop, phone and CLI at feature parity is a large standing cost.
  Atrium's board is one file on purpose.

## 7. What remains unverified

- **Nothing was run.** Static read of a shallow clone taken 2026-09-29, no commit pinned.
- **Whether the marketplace plan matches shipped behaviour.** It is a draft, and only `docs/configuration.md` and the
  existence of `marketplace.ts` suggest part of it is live.
- **Workflow replay and the concurrency-limit counting logic.** Read at overview level. The QuickJS runtime itself is
  traced to its import.
- **Whether `sessionPermissionGrants` persist.** Closed 2026-09-29: they do not, as far as the code shows. The array
  is created empty with the thread attachment (`plugins/provider-claude-code/src/bridge/bridge.ts:930`), appended to
  in memory (`:2502`), and the name appears in no other file. Whether the attachment survives a bridge restart was
  not traced.
- **Safe mode** (`getPluginSafeMode`) and what it disables.
- **The Codex, Pi and ACP bridges.** Only the Claude bridge was read.
- **The 513 open issues.** A count, not read. Where bb hurts in practice is unknown.
- **The `tasks`, `docs`, `memory` and `automations` plugins** beyond their overviews.
