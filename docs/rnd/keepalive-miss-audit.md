# Keep-alive true miss audit

Item `f-keepalive-miss-audit`. Measured on 2026-10-26 against room `sgg`, which runs fb8bb86. The keep-alive policy
code there is the same as claude/main at ca6a5c8: the only difference in `internal/daemon/keepalive.go` is a view
refactor. Every query ran on a copy of `~/.atrium/atrium.db` and its WAL, never on the live file.

## The question

`docs/runtime/claude-cost-model.md` section 3.2 found 8 true misses in the `claude-sg4` ledger between 2026-09-28 and
2026-09-30. They cost $9.25, 68 percent of the $13.53 ledger. Four of them read exactly 12,147 tokens. The database
ended on 2026-09-30, so nobody knew whether misses still happen.

## What the sgg ledger says

| What | Value |
| --- | --- |
| Cards with a keep-alive switch | 246, every one `on` |
| `keepalive_refresh` rows | 0 |
| Room suspended | no |
| Cards in the store | 247, created 2026-10-04 to 2026-10-26 |

No refresh has ever run on this room, so there is no miss to count. That is not evidence that misses were fixed. It is
evidence that the keep-alive never found a card to refresh.

## Why nothing was refreshed

The room's task list gives every card's last skip reason. Of 148 listed cards, 141 are `done` and 3 `running`, all
skipped as "not idle", and 3 have no session id yet. `decide()` refreshes only a card in `needs-input`, and a worker
here ends in `done`.

The `status-changed` events give every `needs-input` stretch since 2026-10-04.

| Stretch length | Count |
| --- | --- |
| under 5 minutes | 339 |
| 5 to 55 minutes | 9 |
| 1 to 4 hours | 2 |
| over 4 hours | 1 |

Only three stretches lasted long enough for a refresh to come due. None qualified.

| Card | Idle | Why it was skipped |
| --- | --- | --- |
| `u-new-new-card-late-in-terminals` | 62 min | no session id |
| `bug-terminals-row-under-toolbar` | 81 min | Sonnet 5.5, 37k context |
| `leak-setup-worktree-for-the-runner-tree` | 510 min | Sonnet 5.5, 34k context |

Sonnet keys its cache on effort, so it is never refreshed, and both cards sat under the 50k floor as well.

The usage rows agree. Of 555 sessions on this room, only 2 started more than an hour after the card's previous one: a
restart wake at 152k and a `say` at 37k. Everything else came back inside the hour or was a first turn. On sgg's
workload, short headless workers that end `done`, the keep-alive has close to nothing to save, and it spent nothing.

## Can a true miss still happen on claude/main

Yes. Nothing in the receipt path changed after the ledger's window. What did change bears on the likely cause.

- The 12,147-token reads match a fork that shares the system prompt with the card and then diverges, which is what a
  different tool list or MCP set does. On 2026-09-28 the fork began carrying the card's launch env and effort
  (879b3e7f0). On 2026-09-29 it began carrying the card's prompt, tool and MCP launch args so lean cards warm
  (fd761df33). Both landed inside the ledger's window, so some of the eight misses likely predate them. Which ones
  cannot be told from here, because the `claude-sg4` database is not reachable from this room.
- A miss still costs a whole 1h write, about $1.16 at the ledger's average context.

## What bounds a miss today

- A miss sets the card to `stopped:miss` (`apply()` in `internal/daemon/keepalive.go`).
- `clearOnRealTurn()` puts `stopped:miss` back to `on` after the card's next real turn. A card whose cache the fork
  cannot reach therefore misses once per idle stretch, every stretch, for as long as it lives.
- The room suspends only when two misses in a row land on two different cards. `lastMissCard` is in memory, so any
  warmed refresh on another card in between, or a room restart, resets it. One card that always misses never trips it.

So a single persistently missing card is not caught by any rule. That is the obvious small fix, filed as
`f-keepalive-sticky-miss` in dept runtime: a card's second true miss, counted from the ledger, sets a state that only
a person clears, as `stopped:acted` does.

## Verdict

On sgg no refresh has run since the room's database began on 2026-10-04, so no miss happened here. The reason is the
workload: cards end `done`, idle `needs-input` stretches past 55 minutes are rare, and the few were Sonnet cards under
50k. Whether misses continue on `claude-sg4`, where the original ledger came from, needs that room's database, read
the same way. The code still lets one card miss on every idle stretch, and that should be closed whatever the answer.
