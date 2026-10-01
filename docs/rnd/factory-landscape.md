# Software factories: the landscape, and what atrium takes from it (SPIKE)

Status: spike by @rnd, 2026-10-01. Nothing built. Backlog: `docs/backlog/rnd/rnd-new-factory-landscape.md`. Clint:
atrium is "moving past being an agent harness aggregator/orchestrator" toward a software factory. Open source first.

Method: researched 2026-10-01 from READMEs, project docs and the GitHub API (stars, licence, last release). Nothing
was cloned or run. Prices marked (3p) come from third-party sites. Anything a source did not say is marked
**unverified**. This does not redo `docs/rnd/competitors.md` (2026-09-29, eight tools at source depth: Orca, herdr,
ruflo, oh-my-claudecode, bb, Charon and others) or `docs/rnd/competitor-features.md` (2026-09-30, ranked features).
Those are harness aggregators. This file is about factories: systems that take work in and land reviewed code out.

## 0. The answer

- **Nobody else combines atrium's four traits**: live terminals a human can take over, for several harnesses, on
  several of your own machines, with a rule that the tool holds no credential. Each trait exists somewhere. The
  combination does not. The closest are OpenHands Agent Canvas (multi-backend, but its servers store secrets),
  Agent Orchestrator (no credentials, many agents, but one host) and Nimbalyst (local-first, multi-harness, but one
  desktop).
- **What the factories have that atrium lacks** is the factory half: work that enters from a tracker, a plan gate
  before code, status derived from facts instead of typed, a fixed outcome and evidence per run, and landing rules
  written as policy. Atrium has the floor (rooms, cards, gates, review). It lacks the line.
- **Borrow, ranked (section 4):** status derived from facts, landing rules as policy, an outcome and evidence on
  every report, a plan gate, hidden-character stripping on intake, then sealed acceptance tests. Most are small,
  because atrium already has the pieces.
- **Plug in rather than rebuild, in one place: the backlog.** Plane CE, Linear and GitHub issues are what the
  factories use as their tracker, and the pluggable backlog spike (`rnd-new-backlog-in-atrium`) should treat them as
  backends. Do not plug into another orchestrator. Agent Orchestrator, Vibe Kanban and the rest sit on atrium's own
  layer. Two smaller spikes are worth naming: ACP as a harness protocol, and a sandboxed room type.

Framing. Igor Ostrovsky's "Software Factories in September 2026" (igoro.com/archive/software-factories) defines a
factory as a system that "starts work from events and coordinates agents through structured engineering workflows",
where agents read and write specs, issues and PRs, and "every change still needs human approval". His ladder is: a
coding agent (a model plus tools), a cloud agent (on remote compute), a factory (coordinated agents in a structured
process). The author is Augment's CTO and cites Augment Cosmos, so it is not neutral. By that definition atrium is a
factory in its review and PR flows and not yet in its intake.

## 1. Open source and self-hosted

| Project | Licence | Agents | Work enters | Review and landing | Credentials | Machines | Sandbox | Human gate | Activity |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| [rusnino/ai-software-factory](https://github.com/rusnino/ai-software-factory) | none yet ("TBD") | OpenCode, Claude Code, Codex, Aider | an idea in Plane CE, email, Telegram | CI, a reviewer, then a separate human merge approval | holds Plane and OPA tokens and shared secrets | server, shape **unverified** | "isolated workspaces" | yes: execution and merge, as two approvals | 1 star, phase 1 |
| [mitkox/esf](https://github.com/mitkox/esf) | MIT | named harnesses (opencode) | `factory run --task`, or a manifest | repo-owned deterministic gates, an optional `review --approve`. Outputs a patch and evidence, never ships | "narrowly scoped credentials locally" | Temporal workers | CubeSandbox microVMs with an egress policy | optional | 182 stars, v0.5.0, 2026-09-27 |
| [asklokesh/loki-mode](https://github.com/asklokesh/loki-mode) | BUSL-1.1 (Apache-2.0 in 2030) | Claude Code. Others experimental | a PRD, an issue, a line | sealed tests, verify and fix, a signed receipt. A supervisor opens the PR, draft if unverified | **the worker never holds a GitHub token** | one | worktrees, Docker optional | partial | 1,078 stars, v10.6.6, 2026-10-01 |
| [finna/Finn-loop](https://github.com/finna/Finn-loop) | MIT | Claude Code (3 skills) | `/finn-spec`, then a Linear issue a human labels `agent-ready` | `/finn-review` labels the PR. **Humans merge, agents never do** | none: `gh` and the Linear connector | one | worktrees | yes: a label in, a merge out | 317 stars, 2026-07 |
| [OpenHands](https://github.com/OpenHands/OpenHands) + Agent Canvas | MIT (`enterprise/` licence **unverified**) | OpenHands, Claude Code, Codex, Gemini, via ACP | chat, Slack, GitHub, Linear, cron and events | automated PR review. PR flow **unverified** | **the Agent Server stores LLM profiles and secrets** | **yes: one Canvas, many backends** (local, VM, Docker, K8s, Modal) | worktrees, Docker, K8s | ask always, auto, or an LLM risk rating | 89.7k stars, v1.24.0, 2026-09-25 |
| [Untrivial-ai/agent-orchestrator](https://github.com/Untrivial-ai/agent-orchestrator) | Apache-2.0 | 32 agents | a task, or an orchestrator that plans and delegates | a kanban derived from PR, CI and review facts. Failing CI and review comments go back to the worker. Merge is the user's | **none: `gh` and each agent's own login** | one (daemon on 127.0.0.1) | a worktree and a browser profile per worker | yes, at merge | 12.6k stars, v0.13.2, 2026-09-30 |
| [AndyMik90/Aperant](https://github.com/AndyMik90/Aperant) (was Auto-Claude) | AGPL-3.0 | Claude Code | text, issues, Linear | a QA loop, then an AI conflict-resolving merge | Claude OAuth, `gh` (**unverified**) | one | worktrees, OS sandbox, a command allowlist | **unverified** | 14.6k stars, maintenance. 3.0 in a private repo |
| [AutoMaker-Org/automaker](https://github.com/AutoMaker-Org/automaker) | MIT | Claude SDK, Codex, Copilot, Cursor, Gemini, OpenCode | kanban cards | **plan approval before code**, then a diff gate, then a PR | the Claude CLI login. Docker wants `GH_TOKEN` in `.env` | one | worktrees, Docker | yes, twice | 3.2k stars. **No longer maintained** |
| [vercel-labs/open-agents](https://github.com/vercel-labs/open-agents) | MIT | its own (AI SDK) | web chat | optional commit, push and PR | a GitHub App key, OAuth secrets, Postgres | cloud | Vercel Sandbox VMs. **The agent runs outside the VM** | no | 5.8k stars |
| [BloopAI/vibe-kanban](https://github.com/BloopAI/vibe-kanban) | Apache-2.0 | 10+ CLIs | kanban issues | inline diff comments back to the agent, then a PR | none: `gh` | one | worktrees | yes | 28.2k stars. **Company shut down 2026-04-10**, community maintained |
| [paperclipai/paperclip](https://github.com/paperclipai/paperclip) | MIT | several | heartbeats wake agents to claim tickets | approval gates | an encrypted secrets store | **unverified** | **unverified** | yes, with budgets that hard-stop | 95.8k stars, 2026-10-01 |
| [coleam00/Archon](https://github.com/coleam00/Archon) | MIT | several | a YAML workflow | phases, validation gates, `loop: until: APPROVED`, then a PR | **unverified** | one | a worktree per run | yes | 23.6k stars |
| [kdlbs/kandev](https://github.com/kdlbs/kandev) | AGPL-3.0 | a different agent per step | kanban | gates between steps, one PR per repo | **unverified** | **executors: local, Docker, SSH, sprites.dev** | Docker | yes | 885 stars |
| [23blocks-OS/ai-maestro](https://github.com/23blocks-OS/ai-maestro) | MIT | several | **unverified** | agents clone, never share worktrees, and integrate through PRs | **unverified** | **a peer mesh, no central server** | separate clones | **unverified** | 799 stars |
| [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad), [imbue-ai/sculptor](https://github.com/imbue-ai/sculptor) | AGPL-3.0, MIT | Claude Code and others | a session | review before push | `gh` | one | worktrees | yes | 8.6k, 233 stars |

Also checked: Crystal (stravu) now points at Nimbalyst. uzi (devflowinc) is stale, last pushed 2025-06. Open-Inspect
([ColeMurray/background-agents](https://github.com/ColeMurray/background-agents), MIT, 3.3k stars) mints
short-lived GitHub App tokens on its server and hands them to sandboxes through a git credential helper, a
different trust model from atrium's.

## 2. Closed or hosted, for contrast

| Product | Self-host | Agents | Work enters | Review and landing | Credentials | Compute | Human gate |
| --- | --- | --- | --- | --- | --- | --- | --- |
| [Factory](https://docs.factory.com/) (Droids, Missions) | cloud, hybrid, or air-gapped | several models | CLI, desktop, web, GitHub, Slack, Linear, GitLab. Named stages: triage, code-gen, validate, release, document, monitor | review and QA Droids. You merge | in hybrid, LLM traffic goes through your own gateway. GitHub app auth **unverified** | Droid Computers that live for days. Missions run hours to weeks | plan approval, release gates |
| [Devin](https://docs.devin.ai/) | single-tenant VPC devbox, the brain stays in Cognition's cloud | its own | web, Slack, Linear, Jira, API, `/devin` in a PR comment | opens PRs, answers review comments while alive | **a GitHub App with org-wide rights, and a secrets vault** | cloud devboxes from snapshots | branch protection |
| [GitHub Copilot coding agent](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent) | no | Copilot, plus Claude and Codex through Agent HQ (2026-02) | assign an issue, `@copilot`, chat, Slack, Jira, Linear | pushes only to `copilot/*`, opens a draft PR. **It cannot approve or merge its own PR, and the person who asked cannot approve it either.** Workflow runs need approval. Commits link the session log | a scoped push-only token, one repo per task | an Actions VM with a firewall and a 59-minute cap | built in |
| [Jules](https://jules.google/docs/) | no | Gemini | web, issues, CLI, REST API | a PR for review | GitHub through a Google account | a Cloud VM per task | **plan approval before code, also over the API** |
| [Augment Cosmos](https://www.augmentcode.com/) | SaaS, VPC, on-prem, air-gapped (vendor's claim) | routed across vendors, BYOK | IDE, CLI, web, MCP | validator agents, and an "adviser" that picks the agent and environment | BYOK | laptops, dev VMs, cloud. Shared org memory | humans own priority, spec and risk |
| [Overcut](https://overcut.ai/) | enterprise tier only | Claude, OpenAI, Cursor, Copilot | PR and ticket events, CI, security findings | automated review, CI fixes. Open playbooks | **unverified** | **unverified** | approvals and policies |
| [Conductor](https://www.conductor.build/docs/) | a local Mac app. Pro is cloud | Claude Code, Codex, Cursor, OpenCode | a workspace per task | diff, checks and merge in the app | **reuses your Claude Code or Codex login** | a worktree per workspace, and it says plainly this "is not a security boundary" | you merge |
| [Nimbalyst](https://github.com/Nimbalyst/nimbalyst) | MIT desktop app, hosted console optional | Claude Code, Codex, more over ACP | sessions and a kanban | per-change accept or reject, a PR review mode | your own logins | a worktree per session, phone control | per change |

## 3. Atrium against the field

**What atrium does that none of them do, together:**
- Live terminals for several harnesses, which a human can watch, type into or take over at any moment. The
  factories run headless agents or one vendor's agent.
- Several of your own machines as rooms, joined by a hub over an overlay, with one board and the phone. Only OpenHands
  Canvas (backends), kandev (SSH executors) and ai-maestro (a mesh) are multi-machine at all.
- No credential held anywhere in the tool, across all of those machines. loki-mode, Agent Orchestrator, Vibe Kanban,
  Finn-loop, Conductor and Nimbalyst share the rule on one host. OpenHands, Open Agents, Open-Inspect, Paperclip and
  Devin all store secrets server-side.
- Directors that review one another's work, with verdicts as commit trailers that a deploy reads.
- A PR review run that forks one cached prime per reviewer, under a budget cap (`docs/rnd/pulls-view-design.md`).

**What they do that atrium does not:**
- Work enters from a tracker on an event (an issue label, a ticket status, a CI failure). Atrium's work enters as
  a director reading a markdown queue.
- A plan gate before any code is written (Jules, Automaker, Factory). Atrium gates tool calls, not plans.
- Status derived from facts (Agent Orchestrator's kanban from session, PR, CI and review). Atrium's backlog status
  lines are typed and go stale.
- One fixed outcome and an evidence bundle per run (loki-mode's VERIFIED, BLOCKED, STALLED or BUDGET_STOP with a
  "NOT PROVEN" line, ESF's patch, logs and manifest). Atrium's `atrium_report` is a summary in prose.
- Landing rules as policy (Copilot: the agent cannot approve its own PR, nor can the person who asked). Atrium's are
  habit, which the room-handoff design starts to write down.
- Sandboxes: microVMs (ESF), containers, cloud VMs. Atrium runs on the host with a worktree, which, as Conductor says
  of its own, is not a security boundary.

## 4. What to borrow, ranked by value for cost

| # | Borrow | From | Atrium today | Cost | Value | Lands in |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | **Status derived from facts**: an item's state comes from its card, branch, verdict and landing commit, never typed | Agent Orchestrator, Finn-loop's labels | stale status lines | M | high | the backlog spike (`rnd-new-backlog-in-atrium`), which already asks for it |
| 2 | **Landing rules as policy**: a verdict's author is never the change's author, the requester cannot approve, and a commit carries a link to its session | Copilot coding agent | habit. The handoff design's landing op has four rules | S | high | the M3 landing op of `docs/rnd/room-handoff-design.md`, and @review's rules |
| 3 | **An outcome and evidence on every report**: `atrium_report` gains a fixed outcome (verified, blocked, stalled, budget) and a "not proven" line that is never empty | loki-mode, ESF | prose summaries. `review.json` does it for PR runs | S | high | @runtime, `atrium_report` |
| 4 | **A plan gate**: a card can stop at "plan proposed", and a director or clint approves it before code | Jules, Automaker, Factory | a tool-call gate only | S to M | high | @runtime and @ui, a card state |
| 5 | **Hidden characters stripped** from issue, PR and comment text before an agent reads it | Copilot coding agent | none | S | medium | the PR runner's fetch, and intake |
| 6 | **Sealed acceptance tests**: the director writes the acceptance test before launch, its hash is recorded, and the worker cannot weaken it | loki-mode's "wall" tests | acceptance is prose in a brief | M | medium to high | @review's rules and the launch brief |
| 7 | **CI and review comments routed back to the owning worker** | Agent Orchestrator, Devin | `docs/rnd/pr-ci-state-design.md` covers part | M | medium | that design |
| 8 | **An egress posture check**: a room refuses to start agents with open egress unless the operator acknowledges it | ESF's `factory doctor` | none | M | medium | `docs/rnd/room-to-room-access-spike.md`'s line, and room-check |
| 9 | **Workflows as data**: a YAML graph of steps and gates, like `pr_recipe` but for any work | Archon, Overcut playbooks | `pr_recipe` for PR runs only | L | medium | later, after 1 to 4 |
| 10 | **Risk-rated confirmation**: a model rates each action and only the high-risk ones wait for a human | OpenHands | a gate and auto-approve rules | M | medium, and a security question | needs @review before anything. A model deciding what is risky is itself attackable |

Already designed, not new: a budget hard stop (Paperclip) is `docs/rnd/freeze-budget-design.md`. A durable,
resumable run (Open Agents) is the PR runner's run folder.

## 5. Run alongside, or plug in?

- **The backlog: plug in.** Plane CE (ai-software-factory), Linear (Finn-loop, Devin, Jules) and GitHub issues
  (Copilot, Vibe Kanban) are the trackers factories use. The pluggable backlog spike should treat them as backends,
  through a command that holds the credential (`gh`, a Linear CLI), as `docs/runtime/scm-design.md` does. Atrium
  should not build a tracker UI to compete with them.
- **Another orchestrator: no.** Agent Orchestrator, Vibe Kanban, Automaker and claude-squad sit on atrium's own
  layer. Running one alongside means two boards for one set of agents.
- **ACP, a spike.** The Agent Client Protocol is how OpenHands and Nimbalyst drive agents without one integration
  per agent. A harness row in atrium that speaks ACP could add agents without new code. Worth a short spike.
- **A sandboxed room, later.** ESF's CubeSandbox and Open Agents' "the agent is not the sandbox" point to a room type
  whose cards run inside a microVM or container, reached by atrium from outside. That is the answer to Conductor's
  "not a security boundary", and it is large.
- **Lessons, not products.** Vibe Kanban's company shut down and its cloud sync went with it. Its local parts survive.
  Aperant went quiet in public while 3.0 is built in private. Both argue for atrium's local-first, self-hosted line.

## 6. Questions for clint

1. **Which borrows to file.** Items 1 to 5 of section 4 as backlog items now, to their owners, the rest later?
   **Default: file 1 to 5.**
2. **The plan gate (item 4).** Make it opt-in per card (a launch flag), or the default for every worker a director
   launches? **Default: opt-in per card.**
3. **ACP and the sandboxed room.** File both as spikes now, or wait? **Default: ACP now, sandboxed room later.**
4. **The tracker for clint's own atrium.** If the backlog plugs in a tracker, which one: GitHub issues on
   dovholuknf/atrium, Linear, or Plane CE self-hosted? This is a question for the backlog spike, named here
   because the landscape answers half of it. **Default: GitHub issues,** since `gh` is already the
   command atrium's PR flow relies on. A room that writes issues needs a `gh` login, which m1mini does not have today.
