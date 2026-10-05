# f-029 report

## Done
- Prune body: read `1<<16 + 1`, 413 over the limit, nothing sent to rooms (`internal/link/prune.go`). Test added.
- Input gate: the answer is yes, a card can stay at needs-input with no `turn_ended_at` (idle-prompt Notification with
  no Stop hook). The proof is in `docs/backlog/fabric/f-029.md`. The gate now stays quiet only for `started` or a card
  under five minutes old at its wait (`internal/link/notify.go`). Tests for old, new and started cards.
- Item status updated, changelog `changelog/fabric/2026-10-05-f-029.md`.

## Left
- Nothing. The five-minute freshness is a judgment call and lives in `freshCard`.

## Verify
- `go test ./internal/link -count=1` with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared: no failures at all on this
  run, so nothing new against the clean-run list.
