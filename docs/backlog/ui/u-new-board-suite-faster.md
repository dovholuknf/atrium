# u-new-board-suite-faster. The whole board suite in minutes, sharded, sg3 included

Status: part 1 DONE, `e90e1a8c` (sharded runner, flaky list, per-unit timing: 3m40s on sg4, serial 22m on sg3).
Part 2 (item 2 below, the sg4 and sg3 split) DROPPED 2026-10-01 by clint: under 5 minutes on one machine is enough.
No worker. Owned by @ui, with @fabric for the sg3 half. Filed by the orchestrator 2026-10-01, from clint.

## Why

2026-10-01 a two-item landing waited about 25 minutes on one whole run of the headless board suite (123 sections,
run serially on sg4 by @ui after @review asked for one whole run). clint: "let's also make that faster, put sg3 on
that if we can."

## Wanted

1. Shard on one machine first: N headless browsers in parallel, each taking a slice of the 123 sections, one merged
   report with the failures named. Sections that share global state stay in one shard, say which. Target: the
   whole run under 5 minutes on sg4 alone. (Memory keep-cpu-busy: the goal is 80-90% CPU, not serial.)
2. Then sg3: the same shards split across sg4 and sg3. sg3 needs the branch under test, so this rides on whatever
   git sync exists to sg3 (f-019 was the blocker, check its state first) or on copying the built tree over. @fabric
   owns that half.
3. Each section's timing goes in the report, so a slow section is visible. The current timing log
   (`TIMING <section> <ms>`) already has them.
4. A known-flaky list kept in the repo, so a flaky FAIL is called flaky in the report rather than re-judged by hand
   each time. Today's run failed `atriumDown` (reload got ERR_EMPTY_RESPONSE) and `shiftMenu` (no new-context entry
   in the row menu): classify both.
