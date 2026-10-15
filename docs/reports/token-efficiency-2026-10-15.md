# Token efficiency, overnight 2026-10-15 (02:30Z to 10:45Z)

Sources: `atrium_export usage` and `tasks` from all five rooms at 10:45Z, `GET /v1/says` per room through the hub.
Script: `scripts/token-report/`. Files beside this one, in `docs/reports/token-efficiency-2026-10-15/`:
`says-overnight-2026-10-15.csv`, `tokens-overnight-2026-10-15.csv`, `cards-overnight-2026-10-15.csv`.

## Tokens per landed item

| window | all tokens | fresh (in + cache write + out) | items landed | all per item | fresh per item |
| --- | --- | --- | --- | --- | --- |
| 10-12 | 412M | 11.2M | ~23 | ~18M | ~490k |
| 10-13 | 351M | 10.4M | ~40 | ~8.8M | ~260k |
| 10-14 | 453M | 13.2M | ~41 | ~11.0M | ~320k |
| overnight 10-15 | 85.1M | 1.72M | 10 | 8.5M | 172k |

Landed on claude/main: hooks-powershell-shell and process-names (fc95a968), ssh-any-room (8f85e589), items-stage3
(19875918), mcp-relay (e548afa1), lost-stop-hook (db559f2d), worktree-stamps-hub-url (791eddbf), board-gpu half 1
(e6d91ea5), ssh-room-login (726382e0), ci-green (94ff6445). The first two were worked before the window, so the
per-item figure is a little flattering. Excluding them: 8 items, 10.6M all and 215k fresh per item.

Verdict: fresh per item is the best yet (172k to 215k against 260k on 10-13). All tokens per item is level with 10-13.

## Coordination share

Orchestrator 13.7M plus minion 20.4M is 34.1M of 85.1M, 40 percent (10-13 37, 10-14 51). Every merge still makes four
says: worker to minion, minion to me, me to minion, minion's merge report.

## Say-started turns

85 turns in the window, 52 started by a say (61 percent). 62 says were sent (each counted once, on the receiving
room). 49 of them started a receiver turn, and those turns read 29.4M cache, 35 percent of all cache read (10-14: 51
percent). The minion's 28 turns were all say-started, about 720k cache read each. The says API does not return
`kind`, so fyi and needs cannot be split from it.

## Worst 5 cards

| card | room | turns | total | why |
| --- | --- | --- | --- | --- |
| ci-green | sg3 | 4 | 28.9M | one session took on a dozen unrelated CI failures in one context, three rounds of fixes |
| minion | claude-sg4 | 28 | 20.4M | relay: every worker result and every review answer is a turn at 150k+ context |
| orchestrator | sg4-control | 31 | 13.7M | reviews: fetch, diff, decide, say, per merge |
| ssh-room-login | m1mini | 3 | 6.4M | 25 files, +1471, one fix round |
| board-gpu | claude-sg4 | 6 | 5.9M | sent back twice (unmeasured, then split) |

## Ran as atrium items

None. Items stage 3 (pools, LEARNINGS.md) merged at 19875918 but the hub was not deployed overnight, so every item
above ran on the say-relay path. The A/B against items starts after the next deploy.

## What to change, biggest first

1. Split CI work by failure. ci-green alone is a third of the night. One item per failing step, each a fresh context.
2. Deploy items stage 3 and run the next queue as pool items. That removes the minion relay turns (28 turns, 20.4M)
   that a done/blocked item makes unnecessary.
3. Briefs set the bar the review checks: change notes, no BRIEF.md or regenerated PNGs in the branch, measured
   numbers for a performance fix. Three of eight reviews went back for one of these.
4. The hub mirrors claude/main by itself. Minion asked for a push twice. Now in memory.
