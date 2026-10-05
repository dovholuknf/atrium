# u-new-reply-suggestions-build. Build reply suggestions

Status: HELD (pause). Filed by the orchestrator 2026-10-01, from clint, gap G12 of docs-deps. Owned by @ui for RS1 and
RS4, @runtime for RS2 and RS3.

## What is missing

A build item. `rnd/rd-new-reply-suggestions.md` is designed and splits the work into RS1 to RS4, and it is in no queue.

## Why it is needed

Without a queued item the design sits and the split between @ui and @runtime is never scheduled.

## Depends on it

Nothing else picks it up.

## Done looks like

- RS1 and RS4 built by @ui, RS2 and RS3 built by @runtime, in the order the design gives.
- Each stage lands on its own branch with its own headless section or Go tests.
- The design file's Status line points at the stages.
