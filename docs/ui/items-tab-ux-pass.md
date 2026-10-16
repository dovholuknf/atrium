# Items tab UX pass

Status: built (phases 1 to 3, board only). Phase 4 waits on open question 2. Backlog item `u-items-tab-ux-pass`. Written 2026-10-16 after clint looked at the tab on
a wide monitor and said "not sure what this is showing me".

Read first: `docs/rnd/item-lifecycle-design.md` (the tab exists to serve that lifecycle: an item is a backlog id at work,
eight steps from designed to pushed, a card does each step). The code: `internal/api/web/js/items.js`, `items-data.js`,
`css/items.css`.

Images are in `docs/ui/img/items-tab-ux-pass/` (its README lists each). The before shots are the real tab, fed rows shaped
like the live hub. The after shots are the built tab over the same data.

| | 2338 wide | 1280 wide |
| --- | --- | --- |
| before | `before-2338.png` | `before-1280.png` |
| after | `after-2338.png` | `after-1280.png` |

## 1. What is wrong, and why

Clint's findings, each traced to the code:

| what he saw | cause |
| --- | --- |
| One row per card: `bug-lean-no-status-line` four times, `rd-test-layout` twice | `/_hub/items` answers one ASSIGNMENT per card (`item_id`, `work_id`, `card`, `state`, `note`). `itemsData.list` maps each straight to a row and lays the same backlog item's steps over all of them (`itemWithWork`), so four rows show identical bars |
| Stale worker and minion cards from the overnight run each make a row | An assignment stays `running` or `blocked` after its card has ended, because nothing closes it. The row has no idea whether the card is alive |
| "Needs you: 0" while 4 rows say "ASKS YOU (no reason given)" | Two different tests. The header counts `state === "blocked" && owner_kind === "human"` (`itemBlockedOnHuman`). Each row prints "asks you" for every blocked assignment, and when `note` is empty it prints the placeholder `(no reason given)` (`itemRow`). The header is right and the rows are wrong: those four are owned by the orchestrator, not by clint. `before-2338.png` shows the same split with a fixture (header 2, six rows asking) |
| Bars carry no labels, dashed vs solid has no legend | `itemProgress` draws eight blank spans with a hover tip. "step 3 of 8" never names the step on the bar, and the skipped state is a dashed outline nobody can decode |
| Spend and landed are empty on every row | The hub has no usage for items yet. The tab draws "no usage yet", "-" and "none yet" in two stat cards and two columns per row |
| Wide monitor: title left, bars mid-row, numbers far right | The row is a five-column grid (`1.25fr 1.5fr 200px 92px 64px`) with no maximum width |

## 2. What does clint DO from this tab

The test for every element: does it help clint do one of these? If not, it goes.

1. **Answer a question** a worker asked him, or approve or send back a design. This is the reason to open the tab.
2. **See what is stuck.** An item nothing is running on, which he cancels or relaunches.
3. **See where one item is** (its current step, how long it has been there) and open the card that is working on it.
4. **Skim what landed**, to know it is done.

Everything else is cut from the list. Spend, token splits, priority tags and coordination cost are facts about an item,
not things to act on from a list. They stay on the item's detail page and on the efficacy page, which already exist.

| today | decision |
| --- | --- |
| four stat cards (needs you, in flight, spend today, landed this week) | one line of count chips: needs you, working, idle, queued. Spend and landed are not shown until the hub sends data (section 7) |
| filter pills, a "sort: needs me first" pill that does nothing | the sort is fixed by the grouping, so the pill goes. The department pills stay in a small row only when there is more than one department |
| tokens column, dollar column | detail only |
| priority and dept tags on every row | the backlog id stays (mono, right of the title). Priority and dept move to the detail |
| age column far right | the age sits on the step line, beside what it measures |

## 3. One row per item

A row is a BACKLOG ID. Its assignments (cards) fold under it.

Phase 1 does this in the board, in `items-data.js`, by grouping the assignments by `work_id` after the list is read.
The hub changes nothing yet.

For each backlog id:

- **The driver** is the newest assignment whose card is live (the board holds the card and it is not offline). With no
  live card it is the newest card on an offline room, then the newest open assignment. The row's state, age and question come from the driver, and the "open
  terminal" button opens the driver's card.
- **Item state**, in this order:
  1. **finished**: the current step is past `pushed` (all eight done or skipped), or the item was cancelled. A landed item
     whose cards have all ended is NOT finished. Lifecycle 2.4 says the current step is the first that is not done, so an
     item that landed and has not deployed or pushed is still at step 7 or 8, and the row says "step 7 of 8 · deploying"
     or "step 8 of 8 · waiting to push" with the open steps on the bar
  2. **needs you**: a question for clint, or a design waiting on his approval
  3. **queued**: no assignment has ever started (every one is queued, and none has finished or run). A row that shows ended
     cards is never queued
  4. **idle**: the current step is a step a CARD does (designed, building, tested, reviewed, landed) and no live card holds it
  5. otherwise **working**
- **Which steps have a card.** Designed, building, tested, reviewed and landed are done by a card role (designer, builder,
  tester, reviewer, merger). Approved is the human's. Deployed and pushed have no card at all (lifecycle section 3). So
  only the first set can be idle. An item at deployed or pushed with every card ended is working, because atrium, not a
  card, is what it waits on. The row says so ("No card is needed for this step").
- **A card on an offline room is unknown, not idle.** The board holds the card flagged offline. The row stays in Working,
  the card chip reads "room m1mini offline" in muted text, and there is no relaunch (a relaunch would start a second
  worker next to one that may be fine). Whether it ever becomes idle is open question 7.
- **The cards fold** under a `details` line reading "1 live, 3 ended". Opened, it lists the live card first with its age,
  then unknown cards ("room m1mini offline"), then ended cards muted and struck through ("ended, never reported"). An
  assignment with no card yet adds nothing here. A card nobody reported on is never a row.
- **Idle is new, and it is not "stale".** The lifecycle already uses `stale` for a STEP that was done and has had commits
  since (section 2.4), drawn amber. Using amber for a stuck item too would give the colour two meanings, so the item state
  is called **idle** and drawn muted: a hatched segment, a dashed row border and the word "idle", never amber. Both can sit
  on one bar (an idle item can have a stale step behind it), so the legend carries both.
  It is the honest word for the overnight cards: unfinished, and nothing alive on them. Its two buttons
  are "cancel item" and "relaunch". It stops those rows counting as work. It also makes the cleanup of dead assignments a
  human choice rather than a silent sweep (open question 2).

The mockup shows each: `f-ptyhost-reconnect` with a real question, `rd-item-lifecycle` waiting on approval,
`bug-lean-no-status-line` as one working row with one live card and three ended chips, `rd-test-layout` and
`u-minion-cards-hidden` as idle, `u-items-tab` landed and deploying, `r-usage-export` waiting to push (both Working,
neither Finished), `f-room-handoff` on an offline room, `u-chat-tab` with a stale step, `f-session-ask` blocked on the
orchestrator with a live card, and `f-hub-forge-push` queued with no card.

## 4. An ask shows the question, or there is no ask

One function, `itemNeedsYou(item)`, answers "does this item need clint". Both the header chip and the group read it, so they
cannot disagree.

It is true when:

- the driver is blocked, **owned by the human**, and has a non-empty question (`note`, or `ask.text`), or
- the design gate is waiting on the human (the `approved` step is active with party `human`).

It is false for everything else, in particular:

- **A blocked assignment with no question.** There is nothing to answer. It is not an "ask". It is a blocked item whose
  worker stopped and said nothing, so it is **idle** (if no live card holds a card step) or sits under Working as
  "Blocked, waiting on <owner>" in small text (if a live one). The string "(no reason given)" is deleted.
- **A blocked assignment owned by a session** (the orchestrator). The question is the orchestrator's to answer. Clint sees
  "Blocked, waiting on orch@sg4, not on you". Whether he may answer on the orchestrator's behalf is open question 1.

The ask box opens with **who asked and how long ago**: "pty-builder asked, 41m ago" (the card's handle, lifecycle
section 7.3). For a design it reads "lc-designer is waiting on your approval, 12m ago". Under that is the full question text (wrapped, never cut to one line), the worker's suggested options as buttons when
there are any, then "answer" (opens the answer box) and "open terminal". A design waiting on approval keeps "approve" and
"send back". The header chip `N needs you` is `items.filter(itemNeedsYou).length`, and the tab badge (`loadItemsBadge`)
uses the same count.

## 5. Name the step, and say what each bar is

- Each row carries one line: **"step 3 of 8 · building"**, with the time in the step on the right. The wording is the
  step's own verb (`ITEM_STEP_VERB`), or "asked a question at building", "waiting for your approval", "idle at landed"
  when the state says so. It names the step in words, never only a number.
- A legend, once, under the count chips: **done**, **current step**, **waiting on you**, **stale step (commits since)**,
  **idle: nothing running**, **not reached**, and "hover a segment for its step name" (the hover tip is already there).
- **Current must read apart from done in every skin.** In the default skin teal draws white, so done and current were
  both white. The current segment gets a different hue (`--blue`, never `--teal`), is taller (9px against 7px) and has a
  2px underline in `--head`. The same treatment, in `--wait`, marks the segment waiting on clint. A headless check
  walks `SKINS` and asserts the computed background colour of the current segment differs from a done one (and that its
  box shadow is set), so a skin that maps blue to teal fails.
- A skipped step is drawn as done. It is not worth its own legend entry on a list (the detail still says
  "skipped: <reason>"). That retires the dashed segment.
- A finished item (past pushed) has no current segment: its bar is full, with the word "pushed" or "cancelled" and
  "5h ago". An item that has landed but not deployed or pushed keeps its bar open, with the current segment on deployed
  or pushed.
- Colour carries state and the word carries the meaning. Orange is only for waiting on clint (the board's `--wait`),
  amber only for a stale step, a muted hatch for idle, teal for done and blue for current. The same tokens as today, no new colours, so the skins repaint it.
  (The default skin draws teal as white, which is why done reads white in the shots.)

## 6. Layout: width and two columns

- The content has a **maximum width** (`1840px` in the mockup, centred) so a row never spreads across a 4K screen.
- Under 1700px it is one column in this order: needs you, idle, working, queued, finished.
- From 1700px it is **two columns**. Left: "Needs you" and "Idle", the things that want clint. Right: "Working",
  "Queued", "Finished". The left column is sticky, so the questions stay in view while he reads the rest. Each row is
  a compact card: title and id, bar, step line, then the question or the buttons. No empty middle.
- Under 700px the buttons stack under the question. The phone has its own page (`m/`) that follows the same grouping
  in a later phase.
- Finished stays folded after five rows, as today.

## 7. Hide spend and landed until there is data

`itemStatCards` draws a spend and a landed card with placeholders when `spend_today` and `landed_week` are null. In the
design they do not exist until the hub sends a number:

- No spend chip while `stats.spend_today == null`. Same for landed. When data arrives they appear as two more chips
  ("$12.40 today", "3 landed this week"), after the count chips.
- Row and detail money and tokens follow the same rule: no usage, nothing drawn. The "?" for an unpriced model stays on the
  detail page, where it explains itself.

## 8. Build plan

Four phases, each landing on its own. Board only until phase 4. Files under `internal/api/web/`.

**Phase 1: fold by item, the live-card test and one needs-you function.**
- `js/items-data.js`: add `foldItems(assignments, work, tasks)` returning one row per backlog id with `driver`, `cards`,
  `group` and `question`, and `itemNeedsYou`. The live-card test (`itemCard` plus the board's `lastTasks`, offline
  flag included) is part of this phase, because the driver and the cards line need it. `itemsStatsFrom` counts from folded
  rows. Keep the mock path (`items-mock.js`) working by making it return folded rows too.
- Groups in this phase: needs you, working, queued, finished, by the rules in section 3. A blocked assignment with no
  question sits under Working as "Blocked, waiting on <owner>". Nothing is idle yet.
- `js/items.js`: `itemListHtml` and `itemRow` draw a folded item, with the "who asked" line. Open, answer, cancel and the
  detail keep taking the driver's assignment id, so the POST routes do not change. `loadItemsBadge` uses `itemNeedsYou`.
- Tests in `scripts/test-board-headless.js` (extend `itemsRealShapeSection`, same fixture shape as
  `docs/ui/img/items-tab-ux-pass/fixture.js`): four assignments for one backlog id make one row. A blocked assignment with
  an empty note makes no "asks you" box. The header number equals the count of rows in "Needs you", checked on a fixture
  with a human question, an approval gate and a session-owned block. A landed item with every card ended is under
  Working, not Finished. An item with only queued assignments is Queued, one with an ended card is not. A poll that
  changed nothing repaints nothing (keep the existing check).
- A node unit test for `foldItems` and `itemNeedsYou` beside the other board unit tests in `scripts/`, since they are pure.

**Phase 2: idle, labels and legend.**
- The idle group (a card step with no live card and none on an offline room) and its two actions. "cancel item" uses the
  existing cancel POST. "relaunch" is open question 3 and ships disabled until answered.
- `css/items.css`: the step line, the legend, the hatched idle segment, the current-segment treatment. Headless: the
  legend lists both stale and idle, the step line names the step, idle rows never count in "needs you", a card on an
  offline room stays under Working with "room <name> offline" and no relaunch button, an item at deployed or pushed with
  every card ended is never idle, and the computed colour of the current segment differs from a done one in every skin in
  `SKINS`.

**Phase 3: layout and hiding empty stats.**
- `css/items.css`: a maximum width, the two-column grid from 1700px, the sticky left column, the compact row. The
  five-column grid and `.it-tok`, `.it-usd`, `.it-age` come out of the list (the detail keeps its own classes).
- `itemStatCards` becomes `itemCountChips` and draws spend and landed only when there is data. Headless at 2338 and
  1280: no horizontal scroll, two columns at 2338, one at 1280, no "no usage yet" string on the list.
- Regenerate the shots with `shoot.js` against the built page and replace the mockup shots.

**Phase 4 (hub, optional): close dead assignments.**
- `internal/link`: close an assignment whose card has ended and never reported (state `finished` with a note
  "ended without reporting"), so the board's idle group is a backstop, not the main path. Needs clint's decision
  (open question 2). Tests in the hub package for the sweep and for an assignment whose card is still live.

Notes for the builder: the board has a phone page and `items.js` is also read by `items-mock.js` (`?itemsmock=1`), so
change both. Add `changelog/ui/<date>-<item>.md` and `docs/changes/<item>.md` per phase, and run
`scripts/test-touched.sh` before handing back.

## 9. Open questions for clint

1. **A blocked item owned by the orchestrator.** Today every blocked item shows an answer button. In the design it says
   "waiting on orch@sg4, not on you" and has no answer button. Do you ever want to answer for the orchestrator from here?
   If yes it is an "answer for orch" link in the expanded card list, not a headline.
2. **Dead assignments.** Should atrium close an assignment on its own when its card has ended (phase 4), or should
   "idle" stay a human choice for good? The design works either way. Closing is quieter, idle is safer.
3. **Relaunch.** What should it do: start a fresh worker on the same backlog id with the same brief
   (`atrium launch --item`)? That is a launch from the board, which nothing does today.
4. **Finished.** Is five rows folded right, or should finished leave the tab after a day and live only on the efficacy page?
5. **Priority.** Gone from the list in this design. Do you sort by it anywhere (high first inside needs-you)?
6. **Phone.** The phone page gets the same grouping in a later change. Is that wanted, or does the phone only need
   "needs you"?
7. **Offline rooms.** A card on an offline room stays Working as "unknown". Should it move to Idle after some time (an
   hour? a day?), or stay unknown until the room is back?
