# r-new-process-registry-build. Build the process registry

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G12 of docs-deps. Owned by @runtime.

## What is missing

A build item. The design is accepted (`docs/process-registry-design.md`, revised in `runtime/2.md`, Mercurius
ready_to_build 2026-09-24) and no queue holds the build. `runtime/2.md` says "build after One atrium".

## Why it is needed

Long-running services started by agents have no owner, no stop rule and no record. The design also covers the Windows
firewall prompts from new exe paths that listen on all interfaces.

## Depends on it

Nothing else picks it up.

## Done looks like

- The registry per the revised design: long-running services only, a process never outlives its runner, no restart
  ever, only the owner stops it and others ask the owner.
- The gated `proc-exec` launcher that joins a room-owned job object before it spawns.
- The two open points settled with clint first: processes on window-mode and joined cards, and whether the gate is
  Bash or a tool of its own.
- Stages and tests set when the item is picked up, after One atrium.
