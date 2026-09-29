# 31 design: hold STUCK while background work runs

Scope: only this bug. Not a v2 rewrite. A worker ends its turn with background shells or headless runs still going
and the board marks it STUCK ("stopped without reporting") three minutes later. It was waiting.

Constraints that are settled: the permission chain is untouched, hooks never fail a session, `/activity` stays fire
and forget, nothing about what a runner is doing now is stored, and no migration.

## Signal

The Stop hook payload's `background_tasks` (entries with `type` and `status`). `atrium turn` already parses it and
counts only `type == subagent`. Chosen because it is the runner's own account, free, and identical on Windows and
Linux. The process tree is rejected: descendants of the runner cannot tell a Bash-tool shell from the MCP servers
every claude owns, so it is a guess where the payload is an answer. MCP children never appear in the payload, so
no separation is needed. If a Claude Code version omits the field the behaviour is today's.

## Count

`background_running` on `/stop`: entries with `status == running` and a type that is not `subagent`. Separate from
`subagents_running`, because subagents keep the card in running and shells must not (a stopped session with a dev
server up still waits on the operator, so the card still goes to needs-input).

## Hold

`stoppedSilently` reports not-silent while the card's last Stop named running background work. It feeds both the
board escalation and the launcher notice, so both hold.

## Clock

Claude Code wakes the session when a background task finishes and that ends in another Stop, which names nothing
running and records a later turn end. The clock is that later turn end. If the work ends with no wake, nothing tells
atrium, so the hold is capped at `BackgroundHoldMax` (2h, env `ATRIUM_A2A_BACKGROUND_HOLD`), after which the
original turn end is the clock, evaluated against the usual threshold, so the card may escalate immediately when
the cap expires. The cap also bounds a dev server left up on purpose.

## Storage and board

The count lives in the in-memory activity tracker, replaced by every Stop and dropped when the card is forgotten.
The board is unchanged: needs-input as before, no STUCK. A "waiting on background work" badge would need the count
on the card and is left out.
