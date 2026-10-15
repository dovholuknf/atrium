# Item lifecycle, stage L1

Backlog item f-item-lifecycle-l1. Stage L1 of `docs/rnd/item-lifecycle-design.md`: item identity, `--item` required on
every launch, and the `--isolated` split.

## What changed

- **Work.** The hub has `work` (one row per backlog id) and `work_card` (every card attached). Migrations `0020_work`,
  `0021_item_work_id` and `0022_work_backfill` on the hub, and `0103_item_work_id` on the room, which needs a room
  restart. Existing items become `adhoc-<item_id>` work with their one card attached. `GET /_hub/work` and
  `GET /_hub/work/<id>` read it.
- **Attachment is the tag `atrium:item:<backlog id>`.** It is not `atrium:item`, which stays the isolated marker. The
  room's item push carries `work_id` through the outbox, so a card launched while the hub was down attaches on
  reconnect. A replay changes nothing. A push with no id is attached to `adhoc-<item_id>`.
- **`--item` is required.** `atrium launch --item <id>`, the `item` argument of `atrium_launch` and
  `atrium_launch_worker`. `--item new:"a title"` files the backlog item first, under the launcher's `dept:` tag, and
  attaches to it. A refusal names `--item` and comes before a slot, a briefing or a post. A hub that cannot be reached
  does not refuse a launch with an id, and `new:` needs the hub.
- **Exempt.** An interview launch (`kind: interview`) names no item, because an interviewer does the human's talking
  and is not work. `atrium launch --resume` and `--onto` name none either, since the card already exists.
- **`--isolated`** is what `--item` used to mean: the isolated toolset and endings. `--item-owner` goes with it. A card
  attached but not isolated keeps its report to its launcher, and its assignment moves to finished or blocked.
- **MCP schema.** `item` is a string and `isolated` a bool. `item: true` over MCP now fails schema validation. A room's
  `/v1/launch` body still takes `item: true`, as an alias of `isolated`, for one release, and takes `work_item`.

## Test plan

## @LETTER@. Every launch names an item

### @LETTER@1. Refusal

1. Run `atrium launch --cwd <dir>` with no `--item`.

**Expected:** it refuses, naming `--item`, and no card appears.

### @LETTER@2. Attach to an existing item

1. File a backlog item. Run `atrium launch --item <id> --prompt hi`.
2. Open `/_hub/work/<id>` on the hub.

**Expected:** one work row and one attached card, and the card has the tag `atrium:item:<id>`.

### @LETTER@3. Two cards, one item

1. Launch a second card on the same id.

**Expected:** still one work row, now two attached cards.

### @LETTER@4. New

1. Run `atrium launch --item new:"a title"` from a card tagged `dept:fabric`.

**Expected:** a backlog item is filed under fabric and the card is attached to it.

### @LETTER@5. Isolated

1. Launch with `--item <id> --isolated`.

**Expected:** the card has no `atrium_say` and ends without naming a launcher, as an item did before.

### @LETTER@6. Backfill

1. Start a hub on a copy of a store that has items.

**Expected:** each has an `adhoc-<item_id>` work row with its card attached, and a second start changes nothing.
