# Review: f-pulls-hub 0d81572b (@fabric)

Range a3fc844f..0d81572b, four commits on claude/f-pulls-hub. The hub carries a room's `/v1/prs` and the `pr` event:
internal/link/pulls.go (the ALL view's merged list, per-row routing by tag or a bounded probe, a raw-JSON retag),
`tagEvent` for `pr`, and `startsNothing` refusing `POST /v1/prs` on a marked room. Read against
docs/rnd/pulls-view-design.md section 9 and docs/rnd/pulls-api.md.

Tests, in a detached worktree at 0d81572b with ATRIUM_LOCATION and ATRIUM_DEBUG_INPUTLAG cleared: `go vet
./internal/link/...` is clean, and `go test ./internal/link/...` passes (157s).

## What holds

- With one room addressed nothing new runs. The request passes as bytes, with `If-None-Match`, `ETag` and `304`.
- The retag leaves findings bytes alone. Only `pr.id`, `pr.walker_task`, `pr.room` and a walker answer's `task` are
  replaced, through `json.RawMessage`, with HTML escaping off. So the `hash` a PUT quotes back is the room's hash.
  `Accept-Encoding` is dropped only on a tagged pulls request, which is the only kind rewritten. Past 16 MiB the answer
  goes through untagged, rather than cut.
- `untag` takes `room~pr_...` back to the bare id, and `roomFor` routes on it. The probe is bounded at 10s per room,
  runs in parallel and skips a quiet room. It shares the card cache, and a `pr_` id cannot collide with a card id.
- The merged list tags `id` and `walker_task`, sums `counts` key by key and `nav_count`, sorts newest first, and says
  which rooms were quiet and which have no pulls. `POST /v1/prs` in the ALL view falls to `needsARoom`.
- The `pr` event is tagged inside its `pr` object, which is the shape the store's broadcast sends.

## Findings

### 1. LOW: the hub answers 200 when no room has pulls, which defeats the board's tab gate

The board (bfa9d0b3, landed b6edcfd6) shows the `pulls` tab only when `GET /v1/prs` answers 200 with a `prs` array.
In the ALL view, `pullsList` answers 200 with `prs: []` even when every room is in `rooms_without` (a 404) or
`rooms_quiet`. So until r-pr-store is deployed on the rooms, a hub board over two rooms shows an empty tab whose paste
box cannot start anything. Fix: when no room answered 200, answer 404 with the contract's error shape. Add a test with
two rooms that both 404.

### 2. LOW: a walker launch on a marked room is not refused

The worker left this as an open decision. My answer: refuse it. `POST /v1/prs/{id}/walker` with `launch` makes a new
card and a runner, which is what "a room on its way out starts nothing new" exists to refuse, the same as
`/v1/launch`. `set` and `clear` only write the row, and stay allowed.

### 3. On the other two open decisions

- `rooms_without` as a key: fine. The contract may grow a field.
- A plain-id GET answered by the probe stays untagged: fine. Every row in the ALL view is tagged, so only a caller
  that already held a bare id gets one, and that caller wants the room's own ids.

## Verdict

OK hub and room, a3fc844f..0d81572b, 2 lows.

Quality: after the Sonnet switch, no drop seen. The raw-JSON retag is the careful answer to a hash precondition, and
each route shape has its test.
