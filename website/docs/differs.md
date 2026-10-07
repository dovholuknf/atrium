---
title: How atrium differs
description: The other tools in this space, what each does best, and where atrium takes a different path.
---

# How atrium differs

A lot of good people are building tools for running coding agents, and many of them are excellent. This page names
the ones atrium's authors have studied, says what each is best at, and says where atrium takes a different path. If
one of them fits how you work better, use it. The ideas worth borrowing are listed at the end.

## Atrium's path, in one paragraph

Atrium is a self-hosted board where every coding-agent session is a card. It runs the agents in real terminals it
owns, so a person can type into any of them from any browser. It keeps durable state in a SQLite file on each
machine, and one hub shows every machine on one board. Every tool call passes a permission gate with standing rules.
Agents message each other through atrium. And it is agnostic about the agent: claude, codex, gemini, opencode,
ollama or a shell are rows in a table. Most of the differences below come from those choices.

## Session managers, boards and orchestrators

| Tool | What it is best at | Where atrium differs |
| --- | --- | --- |
| [Orca](https://github.com/stablyai/orca) | A polished desktop app for twenty or so agent CLIs side by side, one worktree each, with a phone companion. The most widely adopted tool in the field, shipping at a remarkable pace. Its task DAG with human-only decision gates and its careful mailbox delivery are well worth studying. | Atrium is a daemon and a board rather than a desktop IDE. It decides allow or refuse before a tool runs, from rules that persist. |
| [herdr](https://github.com/herdrdev/herdr) | A single Rust binary that owns your agents' terminals, closest to atrium in mechanism. Agent state is read from screen rules kept as data that users can override, and the server hands over live without killing panes. | Atrium arranges the terminals as cards in attention columns, and answers permission requests rather than reporting them. |
| [vibe-kanban](https://github.com/BloopAI/vibe-kanban) | A kanban board where an issue becomes a workspace and an agent, with a clean issue-to-PR flow and inline diff comments sent back to the agent. | Atrium drives the agent's interactive terminal rather than a headless run, and its approvals and rules survive a restart. |
| [Symphony](https://github.com/openai/symphony) | OpenAI's spec for unsupervised runs: poll a tracker, make a workspace per issue, land the PR. Its repo-held `WORKFLOW.md`, reloaded live and validated before every dispatch, is a fine idea. | Atrium is built for an operator who wants to stay in the loop and type into live sessions. |
| [Gas Town](https://github.com/gastownhall/gastown) | The closest director-and-worker model to atrium's, with a memorable vocabulary, a nudge queue that delivers at turn boundaries, an e-stop, and scale to dozens of agents. | Atrium owns the terminals rather than wrapping tmux, enforces policy in code, and keeps state in one SQLite file per machine. |
| [oh-my-claudecode](https://github.com/Yeachan-Heo/oh-my-claudecode) | Team workflows inside Claude Code as a plugin, with a mailbox, worker caps and a rate-limit HUD, plus tmux workers for other CLIs. Refreshingly candid about what it does and does not enforce. | Atrium supervises sessions from outside the agent and enforces permissions at the tool-call hook. |
| [humanlayer](https://github.com/humanlayer/humanlayer) | An early local daemon for Claude Code sessions with persistent approvals, a clean event stream and file snapshots. Much of the field learned from it. | Atrium gates every tool call through a hook, so no runner has to opt into an approval tool. |
| [claude-squad](https://github.com/smtg-ai/claude-squad) | Lightweight and quick: a terminal UI over tmux and worktrees, a session per worktree, attach and go. | Atrium's auto approval is recorded, reviewable, has a deadline and loses to a standing never rule. |
| [bb](https://github.com/get-bb/bb) | A genuine plugin platform. Even its providers are plugins on a public SDK, and its workflow scripts carry strong orchestration patterns such as adversarial verification and judge panels. | Atrium supervises the runner's own terminal rather than being its SDK client, runs extensions out of process, and runs natively on Windows. |
| [Charon](https://github.com/Lomchat/charon) | A self-hosted board that drives agents on rented servers over SSH, with a very well guarded peer bus, a detached terminal holder, Telegram and Web Push approvals, and orchestrated upgrades. | Atrium's machines run rooms that dial a hub, rather than a hub that dials out over SSH, and its rules match on prefix, glob or folder. |
| [Agent Orchestrator](https://github.com/Untrivial-ai/agent-orchestrator) | A board derived from PR, CI and review facts, with failing CI and review comments routed back to the worker that owns them. | Atrium spans many machines through rooms and gates each tool call in a live terminal. |
| [Superset](https://github.com/superset-sh/superset) | An agentic IDE with a worktree per agent, scheduled automations and an iPhone app. | Atrium is a self-hosted board rather than an IDE. |
| [Conductor](https://www.conductor.build/) | A polished Mac app with a workspace per task, and diff, checks and merge in one place. | Atrium spans several machines and runs on Windows and Linux too. |
| [Nimbalyst](https://github.com/Nimbalyst/nimbalyst) | Local-first and multi-harness over ACP, with per-change accept or reject and a PR review mode. | Atrium shows several machines on one hub and gates every tool call. |
| [OpenHands](https://github.com/OpenHands/OpenHands) | A strong open coding agent, and Agent Canvas to drive many agents across local, Docker, Kubernetes and cloud backends, with many intake channels. | Atrium brings no agent of its own. It supervises the ones you already run, in terminals a person can take over. |
| [Paseo](https://github.com/getpaseo/paseo) and [cmux](https://github.com/manaflow-ai/cmux) | Paseo for its plugins and client SDK, cmux for a Ghostty-based terminal built around agents, with notification rings and a branch and PR sidebar. | Atrium is a cross-platform board with a gate and durable state. |

## Agent frameworks

| Tool | What it is best at | Where atrium differs |
| --- | --- | --- |
| [ruflo](https://github.com/ruvnet/ruflo) (formerly claude-flow) | Very widely adopted swarm coordination inside the model's context, with shared memory, a global budget and a benchmark-driven agent picker. | Atrium makes no agent smarter. It supervises and gates sessions a person can watch. |
| LangChain, LangGraph and deepagents | The most widely used agent and workflow toolkit, and a batteries-included harness on top. | Atrium is not a library. An agent built on them is one more runner row. |

## Gateways

| Tool | What it is best at | Where atrium differs |
| --- | --- | --- |
| [mcp-gateway](https://github.com/openziti/mcp-gateway) | Zero-trust reach to MCP tools over OpenZiti, with credential custody, per-backend tool allow and deny, and a full call audit. | Atrium serves only its own control tools and aggregates nothing. The two fit together. |

## Hosted products

Factory, Devin, the GitHub Copilot coding agent, Jules and others run agents for you in the cloud, with wide intake
from trackers and chat and careful landing rules. Copilot's rule that an agent can never approve its own pull request
is a good one. Atrium takes the other path: it is self-hosted, you keep your own runners and your own keys, and it
runs on your machines.

## Ideas worth borrowing

The research behind this page ranks these highest, with thanks to their authors: Charon's guarded peer bus, Gas
Town's delivery at turn boundaries, herdr's agent-state rules kept as data, Orca's decisions that only a human can
resolve, and Symphony's repo-held workflow file. The full reads are in the repository under `docs/rnd/`.
