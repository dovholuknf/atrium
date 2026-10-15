## Test plan

Needs a hub restart (the migration, the pull, the routes) and a room restart (the delivery export and the 5m write price).
Test on a throwaway hub and room on other ports with a temp data dir, or read the live hub's `GET /_hub/work/<id>/cost`.
Never restart the live ones to try this.

## @LETTER@. What an item cost

### @LETTER@1. The steps plus coordination equal the rows

1. Attach a card to an item (`atrium launch --item <id>`), let it take a few turns, and wait a minute for the pull.
2. `GET /_hub/work/<id>/cost`, then `GET /_hub/work/<id>` (its `cost` is the same object).

**Expected:** `steps` (each `name`, `display`, tokens) holds all eight, `headline` is the steps alone with `headline_suffix` "+ coordination", `coordination` is its own line and
`total` is the sum. `units` of every step plus `coordination.units` equal `total.units` exactly, and `total.tokens` are
the room's rows from `GET /v1/usage/export` for that card. `estimated` is true and `prices` names the table.

### @LETTER@2. A model with no price shows a question mark

1. Use a card on a model the table does not hold.

**Expected:** that step and the totals show `?` with `unpriced_models` and `unpriced_tokens`, and the tokens are still
there. A step nobody spent anything on shows `-`, never `$0.00`.

### @LETTER@3. A replayed pull adds nothing

1. Pull twice (wait two ticks) with no new turns.

**Expected:** the totals do not move. `GET /_hub/prices` lists the table, with Opus and Fable rows.

### @LETTER@4. A row crossing a step boundary is divided

1. Take a builder whose turn spans the end of `building` and the start of `tested`.

**Expected:** the turn's tokens appear in both steps in the ratio of the time in each, and the two add up to the turn.

Known limits: the division is by time, so the error is at most one turn per boundary. A relay is coordination only when
its card carries the role `relay`, which a launch does not set yet. The orchestrator's spend counts when a delivery it
received names an item's card as the sender.
