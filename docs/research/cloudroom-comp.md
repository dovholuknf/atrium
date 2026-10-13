# Cloudroom compared with atrium

Researched 2026-10-12 against `cloudroom-core` 0.1.53 (HEAD of the default branch) and cloudroom.dev. Both were read
as untrusted data. Nothing was built, installed or run. Tags: **[code]** seen in the repo, **[doc]** stated in the
repo's docs and not checked against code, **[web]** marketing or site copy only.

## What cloudroom is

Cloudroom is a young open source project (first commit 2026-09-17, 38 commits, 0.1.53 on 2026-10-11, Apache 2.0, about
23.5k lines of Rust) that runs coding agents on a Linux VM and keeps their chat history in PostgreSQL. A desktop app
([cloudroom-gui](https://github.com/davidondrej/cloudroom-gui), a separate repo we did
not read) drives it, running agents on your Mac or on that VM. The core is one Rust service with an HTTP API. A paid hosted version gives each cloud agent its
own sandbox VM. Its pitch, from [cloudroom.dev](https://www.cloudroom.dev/): "Cloud Agents should be open source."
and "Start a task and close the lid. The agent keeps working in the cloud, and the result waits for you." The repo's
[README](https://github.com/davidondrej/cloudroom-core) says "Your agents get their own room in the cloud."

The short version: cloudroom is a cloud execution and history layer for agents with a desktop client. Atrium is a
human-attention board and permission gate over agents that run on machines you already own. They overlap less than
the names suggest.

## Feature table

| Capability | cloudroom | atrium |
| --- | --- | --- |
| Where agents run | Your Mac (via the app) or a Linux VM running the core. Hosted gives one sandbox VM per cloud thread. [web] `README.md`, `docs/setup.md`, security page | Your own machines, one room per machine, each dialling one hub. No cloud service. `docs/fabric/hub-and-rooms.md` |
| Agent runtimes | Codex, Claude Code, Pi, Cursor, plus opencode and Hermes adapters. [code] `src/runtime/{codex,claude,pi,cursor,opencode,hermes}.rs`. Claude is driven over stream JSON, Pi over `--mode rpc`. `docs/harnesses.md`. README says four, the site says three | Runner table: claude, codex, gemini, opencode, ollama, plain shell, added by configuration. `docs/how-atrium-works.md` |
| Multi-machine | One core per VM, the app pairs with each by URL and token. No cross-core coordination seen. [doc] `docs/setup.md` | Hub plus rooms over mutual TLS, merged board, `name@room` addressing. `docs/fabric/hub-and-rooms.md` |
| UI | Native desktop app, Apple Silicon Mac and Linux alpha. Separate repo, not read. Core exposes a PTY terminal websocket `GET /v1/terminals/{id}` and a session API. [doc] `docs/api.md`, README | Browser board with a card per session, status columns, xterm.js terminals, pop-out windows, push subscriptions. `docs/how-atrium-works.md` |
| Phone | No phone client found. Reach the core from other devices with Tailscale. [doc] `docs/setup.md` | Board is a web page, reachable through zrok or OpenZiti. Push subscriptions exist on the hub. We did not check the phone layout for this doc |
| Permissions and approval | None by design: "Agents run without approval prompts". Mitigations are a separate unprivileged Linux user, cgroups, and Command Guard that blocks "a few disastrous commands". [code] `src/runtime/command_guard.rs`, `command-guard.mjs`, README, security page | Every tool call passes a gate: replayed decision, queued message, shelve, deploy hold, standing rule, auto mode, then ask the human. Audited. `docs/how-atrium-works.md` |
| Durable state | PostgreSQL, you apply the SQL, core never migrates. Fsynced receipts, request-id idempotency, queued prompts survive restart, interrupted work is marked `unknown` and never resent. [doc] `docs/storage.md`, `docs/session-lifecycle.md` | SQLite per room plus a small hub store. Cards, events, rules, messages, audit. Storage failure halts the room |
| Agent-to-agent messaging | Parent spawns child threads in the same sandbox through an in-process MCP transport, child results flow back. Pi and Claude only. [code] `src/session/children.rs`. Site: "Each subagent appears as a thread under the lead agent." No peer-to-peer between unrelated sessions seen | Durable per-card queue, `atrium_peers` and `atrium_say`, cross-room, delivered by typing or by hook. `docs/runtime/agent-messaging.md` |
| Git and PR flow | A workspace read endpoint returns path, branch and head. Site claims each parallel agent gets "its own Git branch". [code] `docs/session-lifecycle.md`. No PR creation or review flow found | Worktree per card, hub git mirror that rooms push to, change requests, pull request claims, backlog. `docs/how-atrium-works.md` |
| Overlays and remote access | Core binds localhost, expects HTTPS or Tailscale. Cloud dev servers reach your laptop through `cloudroom preview PORT` over SSH or a websocket tunnel. [doc] `docs/previews.md`, `docs/setup.md` | Board loopback only, remote access through zrok or OpenZiti. `docs/fabric/overlays.md` |
| Auth | One bearer token per core, 256 bit. Hosted uses an account and a per-sandbox token. Agents' model logins are done by native device or OAuth flows on the VM, with Mac login sync. [doc] `docs/harnesses.md`, security page | Board has no login, loopback only. Rooms join with a pinned CA string then mutual TLS. Atrium holds credential names, never credentials |
| Pricing | Free tier (coming soon), Pro $50/month (coming soon), Ultra $500/month (open now), Enterprise custom. Bring your own model subscriptions. [web] cloudroom.dev/pricing | Free, no account |
| License | Apache 2.0, core only. App is a separate repo. [code] `LICENSE` | Apache 2.0 `LICENSE` |
| Open vs hosted | Core open, hosted "invite-only for now" and the sandbox fleet is the paid part. [web] README | Open, self-run, no hosted service |

## What cloudroom does that atrium does not

| Thing | Should atrium want it? |
| --- | --- |
| Managed cloud sandboxes, one VM per agent [web] | No. It is a hosting business and atrium's pitch is no cloud and no account. |
| Sleep idle sessions after 30 min and resume the same native conversation on the next message [doc] | Maybe. Cheap memory win on busy rooms if the runners can resume by id, which `atrium` already records. |
| Idempotent request ids with receipts, and edit, cancel and reorder of queued prompts [doc] | Maybe. Atrium has a message queue, so reorder and edit are small UX wins, request ids matter more for retrying over flaky links. |
| Disk guard: warn at 5 GB, freeze agents below 2 GB, kill the biggest writer after 30 s [doc] | Yes. A full disk kills agents silently and atrium's storage halt only covers its own database. |
| Rewind, compact and fork ("side chat") a session through the API [doc] | Maybe. Fork is attractive for review and experiments, rewind depends on each runner. |
| Preview tunnel: agent runs `cloudroom preview 3000` and the dev server opens on your laptop [doc] | Maybe. Useful for web work, overlaps with zrok shares we already have. |
| Move ("teleport") a local conversation and its files to a cloud machine [doc] `src/session/teleport.rs` | Maybe. Maps to moving a card between rooms, which we do not do. |
| Agents schedule their own future work (cron or one-shot) [code] `src/session/automation.rs` | Maybe. Needs a room that is always up, which ours is. |
| Secret request flow: agent asks, human fills a form, value never enters chat or history [doc] `POST /v1/sessions/{id}/secrets/{request}` | Yes. Atrium has no equivalent, so keys get pasted into terminals and land in scrollback. |
| Usage-limit detection and continue after switching account [doc] `docs/harnesses.md` | Maybe. Pain is real with subscription runners, scope is runner specific. |
| Per-agent Linux user, cgroup containment, no sudo [doc] `README.md`, `src/runtime/linux.rs` | Maybe. Worth it for untrusted work, atrium's gate is the primary control and does not isolate. |
| Computer use and Mac remote control from cloud agents [code] `src/computer_use`, `src/mac` | No. Large attack surface, the security page admits it. |
| Native desktop app | No. The browser board plus pop-out covers it. |

## What atrium does that cloudroom does not

- Gates every tool call with human approval, standing rules and an audit log. Cloudroom has no approval step, by design.
- A board that ranks sessions by how much they need you. Cloudroom's client is a thread list.
- Agent-to-agent messaging between unrelated sessions and across machines, not only parent to child.
- Joined sessions: an agent started in any ordinary terminal can get a card through `atrium join`.
- A multi-machine fabric with one board over many rooms, mutual TLS and overlay transports.
- Shared backlog, reports, change requests and PR claims, and a git mirror for finished work.
- No account, no database to provision, one binary. Cloudroom needs PostgreSQL and two Linux users.
- Runs without Ubuntu, systemd or cgroup v2. Cloudroom's core installer targets Ubuntu 24.04 (`docs/setup.md`).

## Ideas worth taking, ranked

1. Secret request flow, so agents ask for a key through a form and it never enters a terminal or the event log. Cost: a
   new card question type, a store path that skips the event log, and a hook to hand the value to the runner.
2. Disk guard in the room that warns, then parks agents, when free space is low. Cost: a poll loop, a park and resume
   path we mostly have for deploy holds, and a policy for what to kill.
3. Request ids with receipts on message and launch calls. Cost: a column and a unique index on the message queue, plus
   client retry discipline.
4. Idle sleep with resume by id. Cost: a per-runner resume check and a wake path on next message, which the restart wake
   work in `docs/runtime/restart-wake.md` may already cover.
5. Edit, cancel and reorder of queued messages on a card. Cost: three small endpoints and board controls.
6. Usage-limit detection that moves the card to `needs-input` with a clear reason. Cost: per-runner output matching.
7. Session fork from the board. Cost: depends on each runner's fork flag, Claude and Codex only at first.

## What I could not verify

- The desktop app (`cloudroom-gui`) was not read, so every UI claim is from the site and the core's API docs.
- No phone client was found. The site does not mention one. Absence here is not proof.
- Hosted behaviour (sandbox VMs, laptop-off reliability, "dozens of agents") is marketing copy. The site itself says
  laptop-off reliability is "still being tested in the team preview".
- "Each on its own Git branch" is site copy. The repo only has the workspace read endpoint, and I did not trace code for
  branch creation.
- Pricing pages were read once and plans marked "coming soon" may change. Ultra at $500 per month is the only open plan.
- Code was skimmed by file layout, headers and docs, not audited. Doc claims about recovery, sleep, disk guard and
  previews were not confirmed in the Rust.
- Whether the Cursor, opencode and Hermes adapters are production ready: the README names four agents and the site
  names three, while the API lists six harnesses.
- Atrium's own phone layout was not checked for this doc.
- Star count (305) and commit counts were true when read and will drift.

## Sources

- https://github.com/davidondrej/cloudroom-core at 0.1.53, files named in the table
- https://www.cloudroom.dev/, /pricing, /security
