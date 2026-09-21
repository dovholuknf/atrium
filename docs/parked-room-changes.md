# Parked room-side changes

Room-side commits that are built and integrated onto `claude/orchestrator` but NOT yet live, because landing them
needs a room restart (hub-only deploys never touch the room). The orchestrator holds these here so the list survives
a context reset. Clear a row when a room restart lands it.

Hub-only changes never appear here: they ship the moment they are deployed and are gone from this ledger by design.

## Awaiting the next room restart

| SHA | What it does | From doer |
| --- | --- | --- |
| `dc5d24b` | held-message `!` chip clears on hook delivery (`pendingInjector.deliveredElsewhere`, called from `takeMessages`) | held-chip-22200 |
| `302ee47` | launched sessions get a title-derived unique wire name instead of the cwd basename | unique-names-34900 |

## How to land them

Run the maintenance window (hub + room restart) from a DETACHED process. It kills the orchestrator's own supervised
terminal, so it cannot run inline. On restart the room's `reopenSaved` resumes every card.

```
pwsh -File C:\Users\claude\.atrium2\scripts\maintenance-window.ps1
```

After it reports `rooms >= 1` on the new build, empty the table above.
