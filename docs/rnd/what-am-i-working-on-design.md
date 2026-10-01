# What am I working on: finding the sessions clint is using right now

Written by @rnd on 2026-10-01, for `docs/backlog/rnd/rnd-new-what-am-i-working-on.md`. Design only. Nothing here is
built.

**Status, 2026-10-01: closed.** clint rejected option B, the "you, now" section, so W1 and its `hands_at` column are
not built. Option D, the "by started" sort, is his own ask and went to @ui as `claude/term-sort-started`. Options A
and C were not taken. The rest of this document is kept as the record of what was weighed.

## The answer

- **A "you, now" section at the top of the terminal list,** above every group and in every group mode. It holds the
  five cards clint put his hands on most recently in the last 12 hours, newest first. Everything below it stays as
  it is today, with the sort and grouping he already chose.
- **"Hands on" means his own input and nothing else:** a keystroke into the card, a prompt he typed, a message from
  the composer, a stored action, a resume. Agent activity says a card is busy, not that he cares. A permission
  answer is left out of the section on purpose (see option B).
- **The fact it needs is mostly there already.** `human_at` and `human_via` are stored on every card
  (`internal/store/store.go`, stamped by `humanTouch` in `internal/daemon/park.go`) for keep-alive and parking. The
  section needs one more column, one publish rule and one missing call site, listed under "Data the board does not
  carry yet".
- **His literal ask, group by project and sort by started, is cheap and worth adding as a sort,** but on its own it
  answers "what is new", not "what am I using". See option D.

## What is there today

- **Sorts:** by name and by activity (`termOrder` in `internal/api/web/js/terminal-list.js`). By activity puts a
  card that is working right now first, then one waiting on him, then the most recent agent activity. All three are
  facts about the RUNNER.
- **Groups:** project, pile, tag, your own groups, age (`recencyBucket` in `board.js`, which buckets on
  `last_activity_at`) and off. Pinned is a group of its own. A pinned row filed into a group is drawn in both places
  already (`terminal-list.js`, "ONE BRIDGE PER COPY OF THE ATTACHED ROW").
- **Hidden doers:** cards tagged `origin:agent` can be hidden from the list.
- **The human touch:** `humanTouch(taskID, via)` writes `human_at` at most once a minute per card and via, off the
  calling path, and never publishes the card. It is called today for `typed` (`attach.go`), `prompt`
  (`activity.go`), `permission` (`daemon.go`) and `resume` (`park.go`). The vias `message`, `action` and `enable`
  are declared, and `docs/rnd/keepalive-policy-design.md` section 1 lists them, but only the parked-card wake path
  stamps for a message or an action. An awake card that gets a composer message is stamped only if the message is
  typed into a terminal atrium owns and so shows up as a `prompt`.
- **`turn_seen`** records whether the latest turn was seen, and by what. It is written only when a turn was unseen,
  so it is not a "last touched" clock.
- **The phone home** (`m/js/home.js`) already reads `human_at` for its "reported and not touched since" row.

## The options

### A. A third sort, "by you"

Sort the whole list by `human_at`, newest first. A card never touched falls to the bottom in `created_at` order.

- **On today's board:** the card he typed into a minute ago is first, and the one before that is second. Workers
  and directors he never touches sink as a block, which is most of the 17 to 30 live cards.
- **What it gets wrong:**
  - A sort replaces the one he chose. He cannot have "by you" and "by activity" at once, so the card that wants him
    can drop below cards he touched this morning.
  - A permission answer on a worker counts as a touch, so a stream of approvals floats workers he is not working on.
  - Under a group mode the order holds only inside each group, so the most recent card is first in its project,
    not first on the screen.

### B. A "you, now" section at the top (recommended)

The five cards with the newest "hands on" touch in the last 12 hours, drawn as one section above everything else,
newest first. The rest of the list keeps his sort and grouping, unchanged.

- **On today's board:** the two or three cards he is going back and forth between sit together at the top
  whatever the grouping says, and the directors, the workers and yesterday's cards are left where they are. After
  lunch the section still shows what he was doing before it, because it is the last five and not the last hour.
- **Rules:**
  - Entry is a "hands on" touch: `typed`, `prompt`, `message`, `action` or `resume`. NOT `permission`, because
    answering a prompt on a worker is supervision, not work, and the approvals come in streams. NOT `enable`, which
    is a settings click. NOT a `viewed` mark, for the reason the keep-alive design gives: a window left open on a
    second monitor would hold a card in the section for a day.
  - A card in the section is also drawn in its usual place, as a pinned row already is, so turning the section off
    or scrolling down never loses it.
  - A hidden doer that he typed into is shown in the section anyway, because he touched it.
  - Five, so the section stays a section on a 1080p screen with the grouping still visible under it. 12 hours, so a
    card from last night does not open the morning as "now".
  - The section can be folded, like a group heading, and the fold is remembered as the group folds are.
- **What it gets wrong:**
  - A card he only reads, attached in a pane without typing, never enters. That is the price of leaving `viewed`
    out, and reading the terminal in the board is a strong signal when it is not a forgotten window.
  - Two machines' clocks order the section, since each room stamps its own cards. NTP skew is seconds, and the
    section orders on minutes, so this is noise rather than a fault.
  - The five-card cap can drop a card he is still using while he does a short burst across six others. It comes
    back with his next keystroke into it.

### C. A decay score

Each touch adds weight by kind (typed 3, prompt 3, message 2, action 1, permission 1), decaying with a half-life of
30 minutes. The section, or a sort, ranks on the score.

- **On today's board:** a card he typed into for an hour outranks one he sent one message to a minute ago. That is
  closer to "the card I am working on" than any single timestamp.
- **What it gets wrong:**
  - It needs a history of touches, not one timestamp: a new table, or counters in memory that a restart empties.
    Restarts are frequent, which is why `human_at` is stored.
  - The order moves on its own as scores decay, with nobody doing anything. A list that reshuffles while he is
    reading it is the problem the sort tiebreak exists to prevent.
  - He cannot tell why a card sits where it does. A timestamp explains itself: "you typed here 4 minutes ago".

### D. His literal ask: group by project, sort by started

Add a sort "by started", `created_at` newest first, and pair it with the project grouping that exists today. Keep
"active" as the badge it already is.

- **On today's board:** in each project, the newest cards are first. Fresh workers a director launched sit on top
  of a card he has worked in all day because it was started yesterday.
- **What it gets wrong:** starting a card says when it was made, and most cards are now made by directors.
  `created_at` tracks the factory, not clint.
- **What it is good for:** finding the card he started an hour ago and lost. That is a real need, so "by started"
  is worth adding beside the section. It is a few lines in `termOrder` and one more button in the sort pair.

## Recommendation

Build B, and add D's "by started" as a third sort button, because it is nearly free and answers a different
question. Leave A and C. A is B without the top section and with the permission noise. C costs a table and makes
the list move on its own.

## Data the board does not carry yet

1. **A "hands on" clock.** `human_at` cannot be reused as it is: a permission answer after an hour of typing
   overwrites it with `via=permission`, and the board then sees only the permission. Add one column, `hands_at`, in
   a migration at the END of `schema.go`'s slice:

   ```
   ALTER TABLE task ADD COLUMN hands_at TEXT NOT NULL DEFAULT ''
   ```

   `humanTouch` writes it with `human_at` in the same statement when the via is `typed`, `prompt`, `message`,
   `action` or `resume`. The same once-a-minute throttle, the same swallowed failure. It goes on the task JSON as
   `hands_at`, next to `human_at`.
2. **A publish on entry.** `humanTouch` never publishes, so the board would not see a touch until something else
   republished the card, and the section would lag by minutes. Publish the card when a hands-on touch lands and the
   previous `hands_at` is older than 10 minutes. That is one SSE frame when he comes back to a card, and none while
   he keeps typing. The order inside the section refreshes on the card's next publish, which a busy card has within
   seconds.
3. **The missing `message` and `action` calls.** `handleMessage` with no `from` and the stored-action run call
   `humanTouch` with `ViaMessage` and `ViaAction`, as the keep-alive design said they would. Today an awake card
   that gets a composer message is stamped only when the text lands as a `prompt`, which a window-mode card or a
   queued delivery may not do.
4. **The hub carries it as it is.** The task JSON passes through the hub's proxy to the board and to `/m`, as
   `human_at` already does for the phone home. Nothing new on the hub.
5. **Nothing for "by started".** `created_at` is on every card already.

## Stages

**W1, the clock.** Owner @runtime. Size: small, one worker.

- Contents: the `hands_at` column, the write in `humanTouch`, the publish on entry, and the `message` and `action`
  call sites.
- Acceptance:
  - A keystroke on an attach sets `hands_at`, and a permission answer after it leaves `hands_at` unchanged while
    `human_at` moves.
  - A peer's say, a restart wake and a keep-alive fork leave `hands_at` empty.
  - A composer message with no `from` to an awake card sets `hands_at` with `via=message`.
  - One publish for the first touch after 10 quiet minutes, and none for the next 30 keystrokes.

**W2, the section and the sort.** Owner @ui. Size: small, one worker.

- Contents: the "you, now" section in the terminal list on the board, its fold, the "by started" sort button. On
  `/m`, the same five cards as the first rows of home.
- Acceptance, in the headless suite against mocked task rows:
  - Five cards with `hands_at` inside 12 hours show in the section newest first, and a sixth does not.
  - A card with only `human_at` (a permission) does not enter.
  - A hidden doer with `hands_at` shows in the section.
  - Every group mode still draws its groups under the section, and a card in the section is also in its group.
  - The fold survives a reload.
  - "By started" orders on `created_at`, and the tiebreak holds across two polls with the same data.

## Question for clint

Is "the last five cards you put your hands on today" what you mean by "actively working on", or do you want it to
empty out when you have been away, for example nothing older than one hour?
