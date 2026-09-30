# mcp-gateway, llm-gateway and sterling: what atrium should take, lean on, or leave

**Status: PARTIAL. Written 2026-09-30 by worker rd-001 under a wrap-up order.** Sections marked `NOT FINISHED:` say
what they still need. Nothing was run from the projects read, and nothing in atrium was changed. `HANDOFF.rd-001.md`
in the repo root says what was read and what is left, so a fresh session can finish without re-reading.

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
   an agent can replace what runs as clint at the next gateway restart. Move the binary somewhere only clint can
   write. That matters more than any idea below.

Ideas worth taking are small and all sit inside existing atrium files. Ranked list follows.

---

## What changed since 2026-09-15

- **The three projects have not changed.** Last commits: mcp-gateway `cc45748` 2026-08-13, llm-gateway `5f06b36`
  2026-08-12, sterling `e113dec` 2026-07-30. Every verdict in the two earlier documents that rests on their source
  still holds.
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

**Where.** `childEnvFrom` in `launch.go` and a harness column. **Not finished:** I did not establish what the
daemon's environment holds on this machine, which decides whether this is a fix or a nicety.

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
- **Managed mode** (`CLAUDE.md` in mcp-gateway, an orchestrator creates the share and heartbeats over gRPC).
  NOT FINISHED: I did not read `ipc/gatewayGrpc/gateway.proto` or `gateway/ipc` to see whether atrium could be that
  orchestrator. Per the spike, supervising the gateway outside a room is a bad trade, so expect no.
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
- **Fix.** Install the gateway and every stdio backend binary somewhere clint alone can write, and run from there.
  `build.claude` is a build output directory and was never meant to be a run location for another user.

Should atrium hold any of this? No, and this document adds nothing to the refusal in `spike-mcp-gateway.md`.

---

## llm-gateway

**NOT FINISHED.** Read: `README.md` lines 1-260, `CHANGELOG.md` whole. Not read: `docs/current`, `docs/future/roadmap/*`,
`keys/`, `gateway/handler.go`, the Sterling capability alias code.

What is known:

- No commits since 2026-08-12, so the 09-15 verdict stands on the source it rested on.
- It is an OpenAI-compatible proxy with virtual keys restricted by model globs, reloadable key sources, and semantic
  routing (`README.md:186-203`, CHANGELOG v0.1.7). It keeps no audit log beyond its logging.
- v0.1.6 added sterling capability coordinates as strict virtual model aliases (`CHANGELOG.md:23`). That is
  llm-gateway serving sterling, not atrium.
- `CHANGELOG.md:31` says a credential-firewall deployment was a design target: the gateway holds provider keys and
  clients hold only a virtual key. That is the same custody idea as the other user's mcp-gateway.

Tentative answer, unchanged from 09-15: **no fit.** Claude Code does not speak OpenAI format to a base URL, so
atrium's runners would not route through it, and a harness row can already carry a base URL in its env. The custody
question has the same answer as above and needs no atrium code.

Still needed: read the roadmap items `gateway-side-spend-limits.md` and `per-key-metrics.md` to see whether the
state recorded on 09-15 moved, and check whether a codex or ollama harness row would gain anything from a virtual key
per card.

---

## sterling

**NOT FINISHED.** Nothing in sterling was re-read. The 09-15 finding stands until someone checks it: the only real
fit is sterling's terminal-only approval gate calling atrium's `/permission`
(`docs/rnd/ai-platform-fit.md` section 1), with the fail-closed inversion written down.

Still needed:

1. `git log` in `D:/git/github/netfoundry/sterling` since 2026-07-30 (the log I ran shows the newest commit is
   `e113dec` 2026-07-30, so nothing new, but the working tree and branches were not inspected).
2. Search this machine for a project called mint: a Glob for directories and repos named `mint*` under `D:/git`,
   `D:/worktrees` and the home directories. The 09-15 document found none. I did not repeat the search.
3. Read the roadmap directory, `docs/future/`, for anything about approvals or an external approver.

---

## What I could not verify

- **The live gateway config.** `C:\Users\clint\.mcp-gateway\config.active.yml` is unreadable to the agent account,
  which is the boundary working. Which backends and credentials it holds is unknown. The backends are inferred from
  tool names only: `discourse_*` and `mercurius_*` are present in this session, and `zendesk_*` was named on 09-15
  but is absent from this session's list.
- **That the binary swap works.** The ACL is read. The rename-aside and replace is inferred and was not tried.
- **What `permSummary` produces for MCP input in practice.** Read from source only.
- **How the settings importer meets a lean worker's filtered settings.** Not traced.
- **llm-gateway and sterling beyond what is listed above.**
- **Anything by execution.** No request was sent to 8088, 7777 or 7778.
