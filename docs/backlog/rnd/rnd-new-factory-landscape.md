# rnd-new-factory-landscape: other software factories, and what atrium takes from them (SPIKE)

Status: SPIKE WRITTEN, docs/rnd/factory-landscape.md. Owned by @rnd. Filed by the orchestrator 2026-10-01, from clint: atrium is "moving past
being an agent harness aggregator/orchestrator" toward a software factory, and he asked what else is out there,
OPEN SOURCE first.

## Start from

- Open source, self-hosted: rusnino/ai-software-factory (governance controller, human approval gates, state machine,
  audit log, Plane CE as the UI), mitkox/esf (agents in microVMs, verified patches, evidence kept), loki-mode (issue
  in, pull request with a signed receipt out), finn-loop (spec, build, review skills for Claude Code), OpenHands Agent
  Canvas, agent-orchestrator, Aperant, Automaker, Open-Agents, Vibe Kanban. The GitHub topic `software-factory` and
  andyrewlee/awesome-agent-orchestrators.
- Closed or SaaS, for contrast: Factory (Droids, Missions), Devin, Copilot coding agent, Jules, Augment Cosmos,
  Overcut, Conductor, Nimbalyst.
- Framing: Igor Ostrovsky, "Software Factories in September 2026" (igoro.com/archive/software-factories).

## Answer

- For each: licence, self-hosted or not, which agents it drives, how work enters, how review and landing work, how
  it holds credentials, multi-machine or not, and whether a human gate exists.
- What atrium does that none of them do, and the reverse.
- What to borrow, as candidate backlog items, ranked by cost and value.
- Whether any of them is something atrium should run alongside or plug into instead of rebuilding (a backlog
  backend, see `rnd-new-backlog-in-atrium`).

Output: a landscape doc and a ranked list, reviewed by @review. Nothing built.
