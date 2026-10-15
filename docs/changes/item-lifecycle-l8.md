## Test plan

Test on a throwaway hub and room on other ports with a temp data dir, or read the live hub's `GET /_hub/work/<id>`. Never
restart the live ones to try this.

## @LETTER@. The brief and the retirement

### @LETTER@1. A brief opens with where the item stands

1. Launch a card with a brief on an item that has a tested and reviewed branch (`atrium launch --item <id>`, or
   `atrium_launch` with `item`).
2. Read the `BRIEF.md` in its directory.

**Expected:** it opens with "Where this item stands": the item and its state, the branch, eight step lines
(`tested: done at 1a2b3c4. test recorded (seen) at 1a2b3c4`), and the folder `~/.atrium/items/<id>/`. A new card answers
"where are we" from that alone. A hub that is down gives a brief with no header, and the launch still goes.

### @LETTER@2. `atrium_item show`

1. `atrium_item show <backlog id>`.

**Expected:** `lines` holds the same eight lines, `folder` the path, and it is current, not the brief's copy.

Wired today: the lines come from the stored `work_step` rows, and the tested and reviewed text from any `test` or `review`
facts the hub holds (`stepFactLine` in `internal/link/item_brief.go`). Waits on L4: `atrium_record` and the review trailers
that write those facts, and with them "N commits ago" and findings counts. L4 replaces that one function.

### @LETTER@3. The change record points at the design

**Expected:** `docs/rnd/change-record-design.md` says "replaced by item-lifecycle-design". `atrium_change` was never a
tool, and nothing lists it.
