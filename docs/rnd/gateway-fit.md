# mcp-gateway, llm-gateway and sterling: what atrium should take, lean on, or leave

**Status: done, 2026-09-30.** Written by worker rd-001 and finished by @rnd from its handoff. Nothing was run from
the projects read, and nothing in atrium was changed.

Read `docs/rnd/ai-platform-fit.md` and `docs/rnd/spike-mcp-gateway.md` first. This document does not redo them.

---

## Recommendation

**Keep atrium's control MCP, keep leaning on clint's mcp-gateway for everything third party, and do not build an
aggregator.** Three points carry it.

1. **The premise needs correcting: atrium is not an MCP gateway.** The hub's control MCP serves eleven tools of its
   own and aggregates nothing (`internal/link/control_mcp.go:124-303`). Nothing in atrium proxies a third party MCP
   server. The gateway role is already split correctly, atrium serves atrium's tools and mcp-gateway serves the rest.
   The only thing that blurs it is `leanDefaultServers` (`internal/daemon/lean.go:56`), whose `mercurius` entry is
   the whole gateway, not one backend. See finding 5.
2. **Keep the control MCP because it does what a gateway cannot.** It reads the caller from a per-request header,
   `X-Atrium-Agent` (`control_mcp.go:33-39`, `:374-379`), and it is stateless (`:119`). mcp-gateway has no caller
   identity (`gateway/session.go:22-27`) and opens a full set of backend connections, and a stdio subprocess per
   stdio backend, for every client session (`gateway/session.go:97-105`). Routing atrium's tools through it would
   give up identity and bring back the process per session cost that `CHANGELOG.md:1971-1983` records atrium
   removing.
3. **Running the gateway as another OS user is the right design for secret custody, with one hole to close now.**
   Verified: the agent account cannot even read the ACL of `C:\Users\clint`. But the gateway binary the other user
   runs, `D:\git\github\openziti\mcp-gateway\build.claude\mcp-gateway.exe`, grants `Authenticated Users` Modify, so
   an agent can replace what runs as clint at the next gateway restart. The same holds for the other process clint
   runs from `D:\`, `mercurius.exe` (below). Move both binaries somewhere only clint can write. That matters more
   than any idea below, and it went to @orchestrator for clint's morning report the moment it was confirmed.
4. **llm-gateway: lean on nothing and copy nothing.** Its only inbound surface is OpenAI chat completions
   (`gateway/handler.go:21-23`). Claude Code speaks Anthropic's Messages API on a subscription, and codex speaks its
   own, so no runner atrium supervises could route through it. Its custody idea is the same one mcp-gateway already
   gives clint.
5. **sterling: still the one integration worth building, and still not started.** The approval gate is still
   terminal-only and still constructed in one place (`internal/harness/harness.go:303` at `origin/main`'s newest
   line). What changed is that `Allow` now returns a `runrecord.Resolution`, so sterling has its own record of each
   decision, which makes stage 5 of `ai-platform-fit.md` (one record, naming the artifact) easier. "mint" still does
   not exist on this machine.

Ideas worth taking are small and all sit inside existing atrium files. Ranked list follows.

---

## What changed since 2026-09-15

- **Two of the three projects have not changed.** Last commits: mcp-gateway `cc45748` 2026-08-13, llm-gateway
  `5f06b36` 2026-08-12. **sterling has.** Its checkout here sits on the stale branch `learn-updates` (`e113dec`,
  2026-07-30), while `origin/main` is at 2026-09-16 and `origin/digest-keyed-materialization-lifecycle` at `7a15882`,
  2026-09-18, with 247 files changed against the checkout. The approval seam was re-read at `7a15882` (sterling
  section below). Anyone reading sterling on this machine should read `origin/main`, not the working tree.
- **Atrium changed a lot** (roughly 80 commits since 09-15). What matters here: the control MCP moved to the hub
  and is per request and stateless (already true on 09-15, `control_mcp.go:21-45`), lean workers exist
  (`lean.go`), and the Go permission gate `atrium hook --event permission` shipped (`internal/cli/hook_permission.go`).
- **One earlier claim is now too narrow.** `ai-platform-fit.md` section 2 says atrium's gate "only reaches Claude
  Code" and that gating at the gateway is the way to make it harness agnostic. Both are true, but the gate already
  covers every MCP tool a Claude Code session calls through the gateway, and it knows the caller. See finding 1.

---

## Ranked ideas from mcp-gateway

Each says where it would go. Read means I saw it in source. Inferred means I reasoned from it.

### 1. Let standing rules name MCP tools (small, high value)

**Read.** The gate is asked about every tool except a short read-only list (`hook_permission.go:31-36`, skip at
`:72`), so a call to `mcp__mercurius__discourse_discourse_create_post` reaches `onPermRequest` with that tool name
and the agent's identity (`internal/daemon/daemon.go:720`, `:801`). `MatchRule` matches `tool = ?` exactly
(`internal/store/rules.go:333-335`), so a rule for an MCP tool name would work. But the settings importer skips every
`mcp__` entry with the reason "atrium does not gate" it (`internal/claudeconf/claudeconf.go:90-95`, `:104-110`, and
the test `claudeconf_test.go:68-71` asserts it). That list is stale relative to the hook.

**Inferred.** The result today is that every gateway tool call from a gated session is asked of the human, or waved
through by auto mode, and cannot be covered by a standing rule imported from `settings.json`.

**Where.** `internal/claudeconf/claudeconf.go` (`toolsWeGate`, `convert`) plus a test. Also check `permSummary`
(`hook_permission.go:241-269`), which for an MCP call falls through to the compacted JSON, so a rule pattern would
match against JSON text. That is the design question to settle first.

### 2. Audit every mutating control call with its caller (small)

**Read.** mcp-gateway logs every call with complete arguments and a session id (`gateway/session.go:372-379`,
CHANGELOG `Unreleased`/v0.1.6). Atrium's control MCP records one audit kind from a tool call, `launch-refused`
(`control_mcp.go:1200`). Cull, exit, restart and say leave no hub audit entry naming the caller. Only lifecycle
kinds `session-start`, `session-finish` and `session-exit` and permission lines are recorded
(`internal/link/audit.go:148-155`).

**Where.** `control_mcp.go` handlers (`launchHandler` `:1161`, `exitHandler` `:1326`, `cullHandler` `:1385`,
`restartHandler` `:1468`) calling the existing `c.audit`. The `X-Atrium-Agent` value is unauthenticated, so the entry
records a claim, not a proof. Say so on the row.

### 3. A tool set per caller class in the control MCP (medium)

**Read.** mcp-gateway filters tools per backend with allow and deny globs (README "Tool Filtering",
`aggregator/namespace.go`). Atrium's handler is built with a function that receives the request
(`control_mcp.go:113`, `func(*http.Request) *mcp.Server`) yet returns one shared server. So a per-request server, and
so a smaller tool list for a lean worker, is possible without a new mechanism.

**Inferred.** Today a worker sees `atrium_cull`, `atrium_launch` and `restart_atrium` and is kept from misuse only by
handler checks and a prompt line (`lean.go:82-89`). Whether hiding them is worth a server per caller class is a
judgment call, and the identity header is spoofable, so this is tidiness not a boundary.

### 4. A closed environment for launched runners (medium, needs a decision)

**Read.** mcp-gateway's `env_policy: closed` starts a backend with exactly the configured entries
(`aggregator/environment.go:9-40`). Atrium starts a runner from the daemon's whole environment minus a deny list of
`CLAUDE_CODE_*`, `CLAUDECODE*`, four `ATRIUM_*` names and `ATRIUM_DEBUG_*` (`internal/daemon/launch.go:1462-1485`,
`:1583-1598`).

**Where.** `childEnvFrom` in `launch.go` and a harness column.

**Read, names only, from a supervised session on sg4.** Every runner inherits `CLAUDE_GH_TOKEN` and `BB_TOKEN` from
the environment the daemon was started in. So a lean worker writing a docs change holds a GitHub token and a
Bitbucket token it never needs. This makes the idea a real control rather than a nicety: a per-harness allowlist
(the `env_policy: closed` shape), or at least a per-harness deny list the operator writes, so a worker row can drop
the tokens while @review's row keeps `CLAUDE_GH_TOKEN` for `gh`. **Inferred:** which of these are deliberate is
clint's to say. Nothing here says either token has been misused.

### 5. Do not give every lean worker the whole gateway (finding, not an idea taken)

**Read.** `~/.atrium/mcp.json` gives a lean worker `mercurius` at `http://127.0.0.1:8088/mcp`
(`C:/Users/claude/.atrium/mcp.json:4-7`), and that URL is the gateway itself: `C:/Users/claude/.claude.json:2392-2395`
registers `mcp-gateway` at 8088, and the running process is
`mcp-gateway.exe run C:\Users\clint\.mcp-gateway\config.active.yml --listen 127.0.0.1:8088`.

**Read, from this session's own tool list.** A lean worker sees `discourse_*` tools including
`discourse_create_user`, `discourse_update_user` and `discourse_delete_query`, not only the review tools.

**Inferred.** "Lean workers keep mercurius" means every worker holds the authority of every gateway backend.
Options, cheapest first: a second gateway listener with a narrower config for workers (one process per listener, the
spike's "the listener is the identity"), or per-backend `tools: allow` lists in that config. Neither is atrium code.

### Ideas considered and left

- **Per-tool path policy** (`aggregator/policy.go:78-115`). Fits a tool with a path argument. Atrium's folder rules
  already exist and `internal/safepath` already answers containment. Nothing to take.
- **Session idle reaping** (CHANGELOG `Unreleased`). Answers a leak atrium's stateless design does not have.
- **Wedge watchdog** (`docs/future/gateway-wedge-resilience.md`). Concerns the zrok listener. Atrium's halt is a
  different and already documented posture.
- **Managed mode** (`CLAUDE.md` in mcp-gateway, an orchestrator creates the share and heartbeats over gRPC). Read:
  `ipc/gatewayGrpc/gateway.proto:5-70` is lifecycle only. The gateway registers with a share token and a pid,
  heartbeats with `active_connections` and `tool_invocations`, reports a state, and answers ping and shutdown.
  Atrium could be that orchestrator, and would gain a health line on the board and a shutdown button over clint's
  process. The button is the wrong way round: it puts a kill switch over the credential holder into a service every
  agent can reach on loopback. A new gRPC surface for a health line is not worth it either. Left, as the spike said.
- **The `/permission` seam at the gateway.** Settled by the two earlier documents and unchanged.

---

## Credentials under another OS user, answered on its own

**Is it simply the best design? For custody of the secret, yes, and it should be leaned on rather than copied.**

What is verified on this machine:

- The gateway runs as a different user from the agents. `icacls C:\Users\clint` returns "Access is denied" for the
  agent account, so the config that holds the tokens is behind an NTFS boundary.
- The gateway listens on `127.0.0.1:8088` only (`cmd/mcp-gateway/run.go:105`, launched with `--listen`).

What that does not do, and the earlier spike already said most of it:

- **It keeps the string, not the authority.** Any agent process can call any gateway tool. Atrium's gate is what
  stands between an agent and that authority, so finding 1 is the practical control.
- **Loopback is not a boundary between local accounts on Windows** (spike, unchanged).
- **New, verified: the binary is writable by the agents.** `icacls` on `build.claude\mcp-gateway.exe` shows
  `NT AUTHORITY\Authenticated Users:(I)(M)`, inherited from the drive. The directory has the same grant. The file is
  locked while the gateway runs, but Windows allows renaming a running executable, which atrium's own binary swap
  relies on (`docs/runtime/reload-design.md`). **Inferred, not tried:** an agent renames the exe aside, drops its own
  at the same path, and the next start of the gateway runs it as clint with clint's config and tokens. The same
  question applies to every command named under `backends:` in the config, which I could not read.
- **Also verified: it is not the only one.** Of the processes running from `D:\`, two run as clint: this gateway and
  `D:\worktrees\github\michaelquigley\mercurius\http-support\build\mercurius.exe` (the reviewer behind every Mercurius
  round). Its exe and folder carry the same inherited `Authenticated Users:(M)`. clint's scheduled tasks all run from
  `C:\Program Files`, which is fine. His Startup folder and `HKCU` Run keys cannot be read from the agent account, so
  anything started that way is unchecked.
- **Fix.** Install the gateway, mercurius and every stdio backend binary somewhere clint alone can write, and run
  from there. `build.claude` and `build` are build output directories and were never meant to be run locations for
  another user. The source trees under `D:\git` are agent-writable too, so a copy should be built from a commit clint
  has looked at.

Should atrium hold any of this? No, and this document adds nothing to the refusal in `spike-mcp-gateway.md`.

---

## llm-gateway

Read: `README.md` lines 1-260, `CHANGELOG.md` whole, `gateway/handler.go` routes, `providers/anthropic.go`, and the
state line of every roadmap item under `docs/future/roadmap/`.

What is known:

- No commits since 2026-08-12, so the 09-15 verdict stands on the source it rested on.
- It is an OpenAI-compatible proxy with virtual keys restricted by model globs, reloadable key sources, and semantic
  routing (`README.md:186-203`, CHANGELOG v0.1.7). It keeps no audit log beyond its logging.
- v0.1.6 added sterling capability coordinates as strict virtual model aliases (`CHANGELOG.md:23`). That is
  llm-gateway serving sterling, not atrium.
- `CHANGELOG.md:31` says a credential-firewall deployment was a design target: the gateway holds provider keys and
  clients hold only a virtual key. That is the same custody idea as the other user's mcp-gateway.

- **Read: the only inbound API is OpenAI's.** The mux serves `GET /v1/models`, `POST /v1/chat/completions` and
  `GET /health` (`gateway/handler.go:21-23`), and everything else is 404 (`:31`). Anthropic is an OUTBOUND provider
  only: `providers/anthropic.go:160` and `:200` POST to `/v1/messages` with an API key (`NewAnthropic(apiKey, ...)`,
  `:128`). So a request through it is billed per token on a key, never on a Claude subscription.
- **Read: the roadmap has not moved.** `per-key-metrics.md` is still `researching`, `gateway-side-spend-limits.md`
  and `per-key-rate-limiting.md` are `horizon`, and `key-storage.md` is `researching`. The spend-limit item says
  it depends on per-key metrics, which still does not exist.

**Answer: no fit, now settled rather than tentative.** Claude Code speaks Anthropic's Messages API on a
subscription, and llm-gateway neither accepts that inbound nor could carry a subscription outbound. codex speaks its
own Responses shape, not chat completions. An ollama row could be pointed at it through an env variable in its
harness row, which works today with no code. A virtual key per card would give per-card spend only once
`per-key-metrics` ships, and atrium already reads spend from the transcript for Claude cards (the usage tab).

**Lean on it or copy it?** Neither. Its custody idea, the gateway holds the provider key and a client holds a
virtual one (`CHANGELOG.md:31`), is sound and is the same idea as clint's mcp-gateway. Atrium has no provider keys to
protect, because its runners sign in themselves. If one day an API-key runner is added (a codex row on an API key, a
local agent on a paid model), llm-gateway running as clint is the right place for that key, and the harness row
carries only the virtual key and the base URL. That is configuration, not atrium code.

---

## sterling

**Read, at `7a15882` (2026-09-18, the newest ref):**

- The approval gate is unchanged in shape. `newApprovalGate(attended, autoApprove, input, output)` is still built in
  one place (`internal/harness/harness.go:303`), still reads `y/N` from a terminal (`approvalGate.go`, `ReadString`),
  and still fails closed when unattended unless `--auto-approve` converts prompts (signed denies stay denied).
- New: `Allow` returns a `runrecord.Resolution` with the error (`approvalGate.go:37`), so each decision lands in
  sterling's own run record. The branch `origin/run-record` (2026-09-02) is where that came from.
- Still no `Approver` interface and no injection seam for the gate, though `harness.Options` has them for the tool
  and model clients. A search of the whole tree for `atrium` or `Approver` finds nothing.
- The newest work (`per-run-runtime-inputs`, `digest-keyed-materialization-lifecycle`, and `internal/recipe/validate.go`
  growing by 428 lines) is about recipes and materialisation, not about approvals.

**Verdict: the 09-15 recommendation stands, and is slightly easier now.** The one integration worth building is
still sterling's `prompt` disposition asking atrium's `/permission`, failing closed when atrium is unreachable, and
with standing rules and auto mode unable to answer a signed prompt (stages 1 to 5 of `ai-platform-fit.md` section 1).
Stage 5 (one record naming the artifact) now has a sterling-side record to reconcile with. Nothing new in sterling
asks for atrium, and nobody has built the seam. It is still a proposal to sterling's maintainer, not a change atrium
can make alone.

**mint:** a search for directories named `mint*` to depth 3 under `D:/git`, `D:/worktrees` and `C:/Users/claude`
finds nothing, and sterling's tree does not use it as a name. Nothing to assess.

---

## What I could not verify

- **The live gateway config.** `C:\Users\clint\.mcp-gateway\config.active.yml` is unreadable to the agent account,
  which is the boundary working. Which backends and credentials it holds is unknown. The backends are inferred from
  tool names only: `discourse_*` and `mercurius_*` are present in this session, and `zendesk_*` was named on 09-15
  but is absent from this session's list.
- **That the binary swap works.** The ACL is read. The rename-aside and replace is inferred and was not tried.
- **What `permSummary` produces for MCP input in practice.** Read from source only.
- **How the settings importer meets a lean worker's filtered settings.** Not traced.
- **clint's Startup folder and HKCU Run keys**, for other things he runs from `D:\`. Unreadable from this account.
- **Whether `CLAUDE_GH_TOKEN` and `BB_TOKEN` reaching every runner is deliberate.** Names only were read, never
  values.
- **sterling's intent.** Whether its maintainer wants an external approver is still unasked.
- **Anything by execution.** No request was sent to 8088, 7777 or 7778.
