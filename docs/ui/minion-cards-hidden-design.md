# Item minion cards stay off the Terminals tab (design)

Item `u-minion-cards-hidden`, 2026-10-16. DESIGN ONLY, no code is written here. Every item gets a throwaway minion
session that takes it from launch to landed and exits (`docs/runtime/item-minion.md`). Those cards fill the Terminals
tab and the board with rows nobody steers. They should stay out of the way, be one click from their item, and still
reach the human the moment one of them needs an answer.

## The rule in one line

A card tagged `atrium:minion` is HIDDEN from every list of cards unless it needs the human, is the attached one, or the
debug toggle is on. Hiding is a view filter only: the card list, the waiting list, the permission queue and the
notices still hold every minion, so nothing that counts or rings is ever taken away.

## What exists today

- `atrium:item:<id>` is stamped by the launcher on an item worker (`WorkTagPrefix`, `internal/link/work_launch.go`).
  `ItemTag` (`atrium:item`), `ItemOwnerTag` and `DirectorTag` (`atrium:director`) are in `internal/link/control_mcp.go`.
  Nothing writes `atrium:minion` yet. It is new.
- The board holds every card in `cardRows`, read through `cardList()` and `boardCards()` (`js/cards.js`). `renderBoard`
  (`js/board.js`), `renderStack` (`js/stack.js`) and `renderTermList` (`js/terminal-list.js`) all start from
  `boardCards()`. The phone's home (`m/js/home.js`, `render()` and its `visible` test beside `opts.hideSubs`) starts
  from `store.cards()`.
- The Terminals tab lists a card when `termListed(t, show)` says so, from `termRowKind`. Its chips (`paused`, `dead`,
  `subagents`) are stored per device by `toggleTermShow` under `TERM_SHOW_KEY`.
- Tag predicates live together in `js/cardrules.js`: `isDoer`, `isSubagent`, `isInterview`. Each matches a trimmed,
  lower-cased tag exactly.
- An item's page draws its cards as chips (`itemCards`, `itemChip` in `js/items.js`). A click runs `itemsOpenCard`,
  which does `switchView("terms")` then `attachTask(id)`.
- What needs the human is already computed: `cardWaiting`, `cardHasOpenQuestion`, `waitingRow` and `cardsWaiting()`
  (`js/cards.js`), the permission queue (`loadPerms`, `permsLocal`), growlers (`js/growl.js`), and the phone's
  `needsList` (`m/js/home.js`).

## 1. Which tag hides a card, and where the filter sits

**The tag is `atrium:minion`.** Not `atrium:item:<id>`: that one means "belongs to item X", is also on non-minion
workers a human launched on purpose, and varies per item. A director keeps `atrium:director` and is never hidden.

**Stamping.** Add `MinionTag = "atrium:minion"` beside `DirectorTag` in `internal/link/control_mcp.go`. The launch path
that builds an item worker's tags (`internal/link/work_launch.go`, where `WorkTagPrefix` is applied) adds it when the
launch is an item minion. Recommended rule: a launch carrying `atrium:item:<id>` and no `atrium:director` is a minion.
The hub stamps it, not the recipe in `item-minion.md`, so a worker launched by an older recipe is hidden too. The tag
is plain data. Nothing in the room treats it specially.

**The predicate.** `isMinion(t)` in `js/cardrules.js`, beside `isSubagent`, with `MINION_TAG = "atrium:minion"`. Then ONE
function says whether a card is hidden right now:

```
cardHidden(t) =
  isMinion(t)
  && !minionShown()                       // the debug toggle, section 4
  && !cardNeedsYou(t)                     // section 2: it has something for the human
  && !(termTask && termTask.id === t.id)  // the attached one is never pulled out from under you
```

`cardNeedsYou(t)` is new, in `js/cardrules.js`. It is the union of `cardWaiting(t)`, `cardHasOpenQuestion(t)`, status
`needs-permission` and a stuck mark (section 2), for a card that is not `offline`. It reuses the existing predicates and
adds no new state.

**Where it sits, so the tab, the board and the phone agree.** One filter helper, `shownCards(list)` in `js/cards.js`,
next to `cardList()`:

```
function shownCards(list) { return (list || cardList()).filter(t => !cardHidden(t)); }
```

`cardList()` and `boardCards()` keep returning everything. They feed the waiting list, `lastTasks`, the growl and notice
code and the working count, and those must see a minion. `shownCards` is applied at the four places that DRAW cards:

| surface | file and function | what changes |
| --- | --- | --- |
| Terminals list | `js/terminal-list.js`, `termListed` | returns false for a hidden card. `termKept` is untouched, it already spares the attached card |
| board columns and groups | `js/board.js`, `renderBoard` | `live` is built from `shownCards(everything)`. `lastTasks` stays `everything` |
| stack | `js/stack.js`, `renderStack` | the same one-line change |
| phone home | `m/js/home.js`, the `visible` test beside `opts.hideSubs` | `if (cardHidden(t)) return false` |

`js/cardrules.js` is loaded by the phone too (it already uses `isSub`), so `cardHidden` is one definition and not a
copy. The column counts and the working indicator follow from `live`, so a hidden minion is not counted there. Its work
still counts toward spend and the items page, which read the hub and not the board.

The filter drawer's free text search (`js/filter-drawer.js`, the tag match near line 385) matches a card by its tags, so
searching `atrium:minion` finds hidden ones without the toggle (open question 4).

## 2. How a hidden card still reaches the human

The design keeps the alarm paths separate from the drawing paths, so nothing needs to be re-wired.

| signal | surface that already carries it | why it still works while hidden |
| --- | --- | --- |
| permission request | `permsLocal` (`loadPerms`), the perms tab and its count, the banner, growlers (`js/growl.js`, reason `permission`) | keyed by permission id, never filtered by card visibility |
| open question or `atrium_blocked` | growlers (`growlAsks`: `question`, `blocked`), `cardHasOpenQuestion`, the phone's `needsList` | read from `cardList()` and `store.cards()` before `shownCards` |
| needs-you count and `needs-you` column | `waitingRow` and `cardsWaiting()` in `js/cards.js` | operate on `cardList()`. Unchanged |
| header working count | `paintWorking(all)` (`js/board.js`), fed from `renderTermList` with everything | counts what is working. Unchanged |
| item blocked | the owner's notice, the item row's `blocked` state, the `needs you` stat (`itemStatCards`, `js/items.js`) | comes from the hub, not from the card |
| phone bell | `m/js/bell.js`, `onCards` and `onPerms` | read the store, not the filtered list |

What DOES change: the moment a hidden minion needs you, `cardHidden` turns false for it (`cardNeedsYou`), so its row
appears in the Terminals tab and the `needs-you` column with the usual chips, and goes away again when answered. A row
that appears and disappears is the whole point: it is exactly "surfaces where the human sees it". The row carries one
small chip, `minion`, so it is clear why an unfamiliar card is asking (drawn by `termRow` in `js/terminal-list.js` and
the board's card chips in `js/board.js`, which already draw the `subagent` kind of chip).

**Stuck.** `js/keepalive.js` and the long turn check-in (`docs/rnd/long-turn-checkin-design.md`) already mark a card that
has stopped moving. `cardNeedsYou` includes that mark, so a stuck minion un-hides. The exact field is confirmed in phase
1 (open question 2).

**Ready alert.** `cardsReady()` (`js/cards.js`) fires for a card at rest with a runner. A minion at rest between turns is
normal, not news, so `cardsReady` skips `isMinion(t)` unless it has an open question. A minion that ends with
`atrium_done` becomes an item notice, which is the signal the human wants.

## 3. The chip on the item detail page, and opening its terminal

The chip already exists: `itemCards(it)` builds `{name, id}` from `it.cards` and `itemChip(name, id)` draws a
`button.it-card[data-act="card"]`. `itemsOpenCard(cardId)` does `switchView("terms")` then `attachTask(cardId)`. A
hidden minion is reachable today the same way. What remains is making that path work for a hidden card and making the
chip honest:

1. **Attach works on a hidden card.** `attachTask` reads the card from the full list, not `shownCards`, and `cardHidden`
   spares the attached card, so the pane and its row stay while it is open. Nothing to add beyond a test.
2. **The chip says what the card is.** `itemChip` gets a second class `minion` and a short state word from the card
   (`running`, `needs permission`, `ended`), read from `lastTasks` as `itemCard` does. A card that needs you gets the
   existing hot colour. Tooltip: `open this minion's terminal`.
3. **A card that is gone.** `itemCards` already falls back to `c.name || c.id` when `lastTasks` has no row. When the
   lookup finds none, `itemChip` renders a disabled chip (`<span class="it-card gone">`) with the tooltip `this card has
   ended and was removed`. See section 5.
4. **The phone.** `m/js/items.js` draws the item's cards the same way. It opens `m/js/card.js` for a card by id, which
   fetches `/v1/tasks/<id>` directly, so it works for a hidden card with no change. The chip gets the same class and
   state word.

The item's list row (`itemRow`) keeps its chips as they are. Only the detail page needs the state word, so the row
stays compact.

## 4. Showing hidden cards, for debugging

ONE toggle, stored per device, in the same place the Terminals chips live.

- A fourth chip in the filter drawer, `minions`, added to `TERM_CHIPS` in `js/terminal-list.js` and drawn by
  `paintTermShow` with `fdChip("minions", on.has("minions"), "toggleTermShow('minions')", ...)`. Its count of hidden
  minions is written by `renderTermList`, as `paused` and `dead` are. It is OFF by default, and the stored list stays
  "the chips that are ON", so no stored value means hidden.
- `minionShown()` (`js/cardrules.js`) reads it. To keep `cardrules.js` free of terminal code it asks a getter that the
  terminal list registers (`setMinionShown(fn)`). On the phone the toggle is `showMinions` in `OPT_DEFAULTS` of
  `m/js/home.js`, next to `hideSubs`.
- Per tab view state: the Terminals chip is its own key (`termDeviceKey`, so phone and desktop differ). Recommended: the
  board and stack read the SAME key, so there is one toggle and not three (open question 3). `prefLive`
  (`js/prefs-live.js`) repaints every view when it changes.
- When on, minions are drawn as ordinary rows with the `minion` chip and nothing else special.

## 5. A hidden card whose item is gone or done

- **Item done (landed).** The minion exits (`atrium_done`) and the card goes cold. It was already hidden, and stays so.
  It is not un-hidden for being finished: its recap lives on the item. The item's chip turns to `ended`. A minion that
  does NOT exit keeps the exit guard (`changelog/runtime/2026-10-02-r-exit-guard.md`). Nothing in the board ends it.
- **Item gone (cancelled, deleted, or the id no longer exists on the hub).** The card still carries
  `atrium:item:<id>` and `atrium:minion`. It is NOT promoted to a visible row, since an orphan cluttering the tab is the
  very thing to avoid. It stays hidden. If it is still running it is the operator's to find: the `minions` chip lists
  it, and the archive sweep (`internal/daemon/sweep.go`, `archiveworkers.go`) ages it out like any other finished
  worker. A still running orphan that needs the human un-hides through `cardNeedsYou` as always.
- **Item done but a minion asks something.** Same, it un-hides. The ask is real.
- **Archive.** `archiveworkers.go` already archives an `origin:agent` worker with no `atrium:director`. The minion tag
  does not change that, so hidden cards do not pile up for ever.

## 6. Build plan

Each phase lands alone. Board phases ship before and after PNGs under `docs/changes/img/u-minion-cards-hidden/` with a
README line, headless units (`scripts/test-board-headless.js`) plus `bootClean`, and no one-off shooter scripts.

**Phase 1. The tag and the predicate (Go and rules).**
- `MinionTag` in `internal/link/control_mcp.go`, stamped in `internal/link/work_launch.go` for an item worker. Go unit
  test in `internal/link` (no store, under the 10ms budget): an item launch carries `atrium:minion`, a director launch
  does not.
- `isMinion`, `cardNeedsYou`, `cardHidden`, `MINION_TAG` in `js/cardrules.js`, `shownCards` in `js/cards.js`.
- Headless units: `minionPredicate` (tag matching: trim, case, a director never a minion), `minionNeedsYou`
  (permission, question, stuck, attached each un-hide). No visible change yet, nothing calls the filter.
- PNGs: none.

**Phase 2. The Terminals tab.**
- `termListed` uses `cardHidden`. `cardsReady` skips a resting minion.
- Headless units: `termMinionHidden` (a minion is not listed, a minion with a question is, the attached one is),
  `minionReadyQuiet`, plus `bootClean`.
- PNGs: `minion-terms-before.png` (a tab full of minions), `minion-terms-after.png` (only real sessions) and
  `minion-terms-asking.png` (one minion un-hidden with the `minion` chip).

**Phase 3. Board, stack and the phone.**
- `renderBoard`, `renderStack`, `m/js/home.js` `visible`. Counts follow `live`.
- Headless units: `boardMinionHidden` (column counts exclude, `lastTasks` keeps), `stackMinionHidden`,
  `phoneMinionHidden`, plus `bootClean`.
- PNGs: `minion-board-before.png`, `minion-board-after.png`, `minion-phone-after.png` (400px wide).

**Phase 4. The item chip.**
- `itemChip` state word and `minion` and `gone` classes in `js/items.js`, the css in the items stylesheets, the phone's
  `m/js/items.js`.
- Headless units: `itemMinionChip` (state word, hot when it needs you, disabled when the card is gone),
  `itemOpenHiddenCard` (`itemsOpenCard` attaches a hidden card and its row is kept), plus `bootClean`.
- PNGs: `item-minion-chip-after.png` (a detail page with a running chip and an ended chip).

**Phase 5. The `minions` toggle.**
- `TERM_CHIPS`, `paintTermShow`, `minionShown`, the board and stack reading the shared key, the phone `showMinions`
  option, `prefLive`.
- Headless units: `termMinionsChip` (toggle shows them, the count is right, default off), `minionToggleShared` (one key,
  all three views follow), plus `bootClean`.
- PNGs: `minion-chip-off.png`, `minion-chip-on.png`.

Phases 2 to 5 depend on phase 1 only. Order them 2, 4, 3, 5 if the Terminals clutter is the pain to relieve first.

## Open questions, each with a recommended answer

1. **Who stamps `atrium:minion`, the hub or the recipe?** Recommended: the hub, in `work_launch.go`, whenever a launch
   carries `atrium:item:<id>` and no `atrium:director`. An older recipe then needs no edit, and a hand launched worker
   without an item tag is never hidden.
2. **What is the "stuck" field?** Recommended: confirm in phase 1 against `js/keepalive.js` and the long turn check-in.
   If the board has no per-card stuck flag, drop it from `cardNeedsYou` and rely on the item going `blocked` (the
   owner's notice) plus the existing keepalive growler, which is not filtered.
3. **One toggle or one per view?** Recommended: one shared key across Terminals, board, stack and phone. Debugging wants
   "show me everything" and a key per view makes that three clicks.
4. **Should search find hidden minions?** Recommended: yes, a search that names `atrium:minion`, a card id or an item
   id matches them even with the toggle off, since a typed query is an explicit ask. Plain text queries do not.
5. **Should a `ready` (finished turn) minion un-hide?** Recommended: no. It is exiting. The item notice is the signal.
6. **Hide the item's other workers (a reviewer, a tester) too?** Recommended: no. Only `atrium:minion`. A human
   launches those or they are directors with their own purpose. Revisit if they pile up.
7. **Does a hidden card count toward "working"?** Recommended: yes, `paintWorking` keeps counting running minions, so
   the header tells the truth about load while the Terminals tab stays quiet.
8. **Should an un-hide animate?** Recommended: no. It is a normal row appearing, with the usual new card note
   (`newCardNote`). The `minion` chip explains it.
