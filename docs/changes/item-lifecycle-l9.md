## Test plan

Needs a hub restart (migration 0028, the collectors, the routes). Test on a throwaway hub on other ports with a temp data
dir, or READ the live hub's `GET /_hub/work/efficacy`. Never restart the live hub to try this. The board needs no build
step: `?itemsmock=1` draws the page from a fixture.

## @LETTER@. Efficacy

Before: the items tab had no efficacy page (`docs/rnd/img/item-lifecycle/items-tab-before.png`). After: the page below,
`docs/changes/img/efficacy-l9-1400.png` and `docs/changes/img/efficacy-l9-2400.png`, drawn to
`docs/rnd/img/item-lifecycle/efficacy-1400.png` and `efficacy-2400.png`. The PNGs are produced by
`node scripts/shoot-items.js` (view `efficacy`); they were not shot from this room, see the limits.

### @LETTER@1. The page matches the mockup

1. Open the items tab, press `efficacy`. Or load the board with `?itemsmock=1`.

**Expected:** four tiles, eight roll-up rows with an 8-period strip (the open bar is the current period), the send-backs
by step, where the time went, the abandoned list, and the landed items with a mark each. Every figure carries an
observed or reported tag.

### @LETTER@2. A dash is not a zero

1. Look at an item that has not been landed, or a week with no item that has an answer.

**Expected:** `-`, never `0%`. A bar for such a period is a stub. The item page shows `-` for what it has no outcome for.

### @LETTER@3. Too few to trend

1. Read a week with fewer than 5 landed items.

**Expected:** "too few to trend", no arrow, and a rate over fewer than 5 is greyed with its count.

### @LETTER@4. Observed only

1. Press `observed only`.

**Expected:** the page refetches with `observed_only=1`, every reported input (bug links, "not accepted" notes) leaves all
the figures, and no `reported` tag remains.

### @LETTER@5. A send-back counts once

1. `POST /_hub/work/<id>/event` `{kind: "not_accepted", ...}` twice with the same time and sha, and send the same
   design back twice.

**Expected:** the event is keyed by (work id, kind, at, sha), so one row exists and the item counts one send-back.

### @LETTER@6. A revert shows on the item and in its week

1. Land a commit on `claude/main`, then `git revert` it within 7 days and wait a collect.

**Expected:** the item's mark says reverted, its page shows "n reverted", and the week the revert happened counts it in
"went wrong after landing".

How it counts: a clean pass excludes unproven items (no test seen, or the 7 days not over). Reverts are read without a
bound. Bugs and CI count within 7 days of landing. A bug is an id `b-*`, dept `bugs` or `kind: bug`, linked by
`atrium:item:<id>` (observed) or "fixes <id>" (reported). `landed_at` is when the hub first read the branch on
`claude/main`. Past blocked-on-human windows are not kept, only the current one. The CI collector needs a forge login on the
hub, or items stay `unproven`, an honest dash. The test and review facts come from `work_fact` kinds `test` and `review`
(L4), so they stay empty until L4 lands.

Limits of this change as built: the migration is 0028 (0027 is L4's). The live hub was not reachable from this room, so the
Go side was checked on a fresh test hub rig (`internal/link/efficacy_integration_test.go`). This room could not start a
browser (Chromium dies with SIGTRAP at launch), so the PNGs and the `itemsEfficacy` headless unit were not run here, and the
page was only drawn in node against the fixture. Run `HEADLESS_UNITS=bootClean,itemsEfficacy` and
`node scripts/shoot-items.js` where a browser starts.
