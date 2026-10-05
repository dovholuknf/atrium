# r-new-mercurius-one-shot. A Mercurius one-shot entry point that survives a restart

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G6 of docs-deps. Owned by @runtime.

## What is missing

A single call that starts a Mercurius review, waits for it, and returns the result, run by atrium and not by an agent
session. On 378 the second opinion was lost when the session that started it went away.

## Why it is needed

Pulls P4 and `review/30` both assume a one-shot call that does not exist.

## Depends on it

- pulls P3 step 6 (second opinion)
- P4 Mercurius as a recipe step
- review/30 peer review inside an atrium session

## Done looks like

- One entry (command and API) that takes a prompt and a target and returns when the round is collected.
- Atrium owns the run, so a room restart or a closed session does not lose it. It resumes or reports the loss.
- The result is stored where the recipe step reads it.
- A test kills the starting session mid-round and still collects the result.
