# Lean sessions keep the time line and the Explore subagent

Lean drops the operator's UserPromptSubmit hooks, and with them the `now:` line a director needs. Atrium now adds its
own hook, `atrium hook --event now`, to a lean session's `--settings`. It prints `now: <yyyy-mm-dd ddd HH:MM zone>`
and nothing else. `atrium launch` also gains `--lean-agents`, and the launch tool docs say that Explore and Plan are
built-ins that need no file. See `docs/runtime/lean-workers-design.md`.

## Test plan

1. Launch a lean claude session. Send a prompt. The context holds one line, `now: 2026-10-22 Thu 08:38 EDT`, and no
   style reminder.
2. Run `atrium hook --event now`. It prints that one line and exits 0.
3. `atrium launch --lean-agents Explore ...`. The card carries `atrium:lean` and `atrium:agent:Explore`, the session
   can start an Explore subagent, and any other subagent type is refused.
4. Resume that card. It still has Explore.
