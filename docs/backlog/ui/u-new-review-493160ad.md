# Review: u-scm5 493160ad

Range `29efa251..493160ad`, one commit. This is scm stage 5's board half:
- a Requests view in the repos area, with a strip of open requests into main that need the orchestrator, and a list
  and request page beside it;
- the Pushed line;
- a chip on each ledger branch row;
- the /m changes header;
- a phone requests sheet.

There are no Go changes. The new files are `js/changereq-core.js`, `js/changereq-mock.js`, `js/changereq.js`,
`m/js/changereq.js`, two stylesheets and a fixture. `hubrepos.js`, `m/js/changes.js`, `settings-spine.js` and the
headless test are changed.

Verdict: **hold** for room and hub, on M1. M1 is a small fix. The rest reads well. The Lows can follow.

## How it was checked

- I read the diff in full, with every HTML sink in it. Per the standing note, I read the board units and did not run
  them.
- `node --check` passes on all ten changed js files at the tip.
- `git merge-tree` onto `claude/landing` d5876dc1 is clean.
- I compared the API shape against @fabric's work in progress:
  - `claude/f-change-requests` 6f2bbecd, which is `internal/hubstore/changerequest.go` and hubstore migration
    `0009_change_request`;
  - the uncommitted `internal/gitsync/pushed.go` in that worktree.
- I looked at two shots: requests full at 2000 in dark, and the phone request page at 390 on paper.

## Points

- **Escaping.** It is clean.
  - Desktop: every value from the hub goes through `esc()`, in text and in attributes: title, why, note, branch
    names, repo, id, room, card, change id, shas, the 409 text and the timeline.
  - The phone sheet builds with `createElement` and `textContent`. Its one `innerHTML` is a fixed SVG.
  - There are no inline handlers. Clicks go through `data-cr` on one delegated listener.
  - The one unescaped value is `r.state` in a class name (`cr-ended ' + r.state`). The hub's CHECK constraint keeps it
    to four words, but see L4.
- **Writes.**
  - The sha is checked as 7-40 hex before the button is enabled, and checked again in the click handler.
  - `cr.busy` stops a double submit, and the buttons are disabled while it is set.
  - The writes are same-origin `fetch` POSTs with a JSON body. A cross-site form cannot send that without a
    preflight. Whether the hub checks `Origin` and `Sec-Fetch-Site` the way `docs_api.go` does is @fabric's half: the
    link routes are not on any branch yet.
- **The shape against @fabric's draft.** It matches.
  - `ChangeRequest` has the same fields the board reads: `id`, `repo`, `source{room,branch,sha}`, `target{branch}`,
    `title`, `why`, `change`, `state`, `created_by`, `created_at`, `closed_at`, `closed_by`, `note`, `merged_sha` and
    `owner`. The operator is `card: "operator"`, which the board's `by()` renders as "the operator".
  - The board's caps are within the hub's: title 200, why 2000 against 4000, note 300 against 1000.
  - `Pushed` has `state`, `hub_sha`, `room`, `card`, `at` and `released`, with the same five state words.
  - Migration 0009 is the hub store's next number (it ends at `0008_git_push` on landing), so it does not collide with
    the room store's `0009_permission_details`.
- **Fields the board reads that the draft does not yet serve:**
  - a request's `pushed`, on `GET /_hub/change-requests/<id>`;
  - the `change-request` event on `/v1/events`;
  - `repo`, `branch` and `head` on `/v1/tasks/{id}/changes`, for the phone's Pushed line;
  - the change record's four lines.

  The worker's caveats name the last three. The first lives in the link routes, which do not exist yet.
- **The view switch.** "Requests" is a fourth radio in the existing radiogroup, with an `aria-label`, so the roving
  tab index and the arrow keys carry over.
- **The phone sheet.** It is a full screen with history, like the changes sheet. Back pops it, and the form has no
  history entry of its own.
- **The design.** The desktop page reads at the bar of the repos redesign:
  - a lane from the source to its target;
  - a gate panel with three steps;
  - the Why, On the hub, Change record and History panels.

  On the phone page on paper, nothing is clipped and the colours stay in the paper palette. The disabled teal button
  is low in contrast, which is allowed for a disabled control.

## Medium

### M1: the mock gives no sign that it is on, and the localStorage switch stays on for good

`mockOn()` is true for `?crmock=1` or for `localStorage["atrium.crMock"] = "1"`. Nothing on either page says the mock
is on. Its seed rows look real: real-looking rooms, cards and branch names, and a "Needs the orchestrator or clint"
strip.

Every write then goes to the mock and nowhere else. So someone can follow the gate's three steps, type a sha, press
"Record that it was merged", see "Recorded as merged", and the hub knows nothing of it.

Two ways that happens:
- **The switch stays on.** The localStorage switch survives across visits, so a browser that turned it on once while
  building stays on fake data with no sign of it.
- **A link turns it on.** A link with `?crmock=1`, sent to the operator, does the same for that visit.

No data can be put in through the URL, so this is about being misled, not about injection.

Fix:
- Whenever `mockOn()` is true, show a fixed banner on both pages, in the warn tone, such as "Mock data: nothing here
  reaches the hub", with a "turn off" button that clears the key.
- Better still, drop the localStorage switch, or keep it only for the headless run, so that only the URL turns the
  mock on and it goes away with the next load.
- Add a headless assert that the banner shows exactly when the mock is on.

## Lows

- **L1: any 401 or 403 makes the whole page read-only.**
  - `noteWrite` sets `readOnly` on any 401 or 403 from any write. The hub's link rules are not written yet.
  - If the hub refuses one action for a reason that belongs to the request, the page hides every action until a
    reload. Two likely cases: a withdraw by someone who is not the owner, or `merged` from a card.
  - Treat only a 401, or a 403 whose body says the board is read-only, as read-only. Show any other 403 as that
    action's error.
  - A `can_write` flag on the list, as the worker suggests, fixes it properly. Note it for @fabric.
- **L2: "Withdraw" is shown to the operator, and says "by its owner".**
  - The operator's board offers Withdraw on every open request, with no confirm. The ended line then says "Withdrawn
    by its owner", even when the operator withdrew it.
  - If withdrawing is the owner's action, hide the button from the operator, who has Close with a note.
  - Otherwise, word the line from `closed_by`.
- **L3: the sha forms.**
  - The board takes 7 to 40 hex characters for `merged`, but @fabric's `gitsync.ValidSHA` takes a full 40 or 64.
  - If the hub's `merged` uses the same check, a short sha gets a 400, and a SHA-256 repo's 64-character sha is
    refused by the board before it is sent.
  - Agree one rule with @fabric. The likely one is a full sha, 40 or 64.
- **L4: `r.state` in a class name.** Use `esc(r.state)`, or map it through `crCore.STATE`, like everything else on
  the page.
- **L5: "The Pushed line is on the left".** On the desktop page, the On the hub panel is to the right of Change
  record. The phone wording ("above") is right.
- **L6: `crLoadAfter` clears `cr.inflight` by force.** A write followed at once by a `change-request` event can then
  run two list reads at the same time. They give the same answer, so this is cosmetic.

Atrium-Verdict: hold 29efa251..493160ad
Quality: careful work. Every value is escaped, the board never merges and says so, and it was built against a mock
that matches @fabric's store row field for field. The mock needs to say when it is on.

## The ui queue docs that rode with this batch

These are `cbd719c0` (the two lows left from the 147e7c70 review) and `b7ecc0f8` (the owed-chip item, filed and
held by the pause). Both are doc-ok.

The owed-chip doc describes `reported_at` as r-owed-answers 6b8ae15f had it. The fix in progress, 49488e18, stamps it
again for cards the operator launched, so re-read that paragraph before the chip is built.

Atrium-Verdict: doc-ok 29efa251..b7ecc0f8

## Re-read: b2c2a8bc

One commit on 493160ad, so the range is `29efa251..b2c2a8bc`.

Closed:
- **M1.** Only `?crmock=1` turns the mock on, for that load alone. The localStorage switch is gone. While the mock is
  on, a fixed banner with `role="status"` says "Mock data: nothing here reaches the hub." It is built with
  `textContent`, and its "Turn off" button reloads the page without the parameter. Both pages load
  `changereq-core.js`, so both show it.
- **L1.** A 401, or a 403 whose text says read-only or share, makes the board read-only. Any other 403 is shown as
  that action's error.
- **L2.** The ended line says "Withdrawn", with who from `closed_by`, on both pages.
- **L3.** A sha is exactly 40 or 64 hex characters, matching the hub's `ValidSHA`, on both pages and in the mock. The
  input's `maxlength` is 64.
- **L4.** `esc(r.state)` is used in the class name.
- **L5.** The doc wording is fixed.
- **L6.** `crLoad` keeps one read in flight and queues one rerun, and `inflight` is no longer cleared by force.

Checked:
- `node --check` passes on the four changereq files.
- `git merge-tree` onto `claude/landing` is clean.
- Per the standing note, I read the board units and did not run them.

Note, not a hold:
- **N1: the read-only test is loose.** `readOnlyWords` matches `/share/i` alone, so a 403 that only mentions a share,
  such as "that branch is not shared", would set read-only. Once @fabric fixes the refusal text, match that exact
  wording instead.

Atrium-Verdict: room-ok 29efa251..b2c2a8bc
Atrium-Verdict: hub-ok 29efa251..b2c2a8bc
Quality: every finding is closed. The mock now says it is on and cannot stay on by accident.

## Doc: u-new-peek-cold-twice ecfbef79

A backlog doc only, on claude/ui-director. It is a clear item with a "done when" that a headless check can hold.
There are no private paths or names. It landed with the u-scm5 and u-pin-shows batch.

Atrium-Verdict: doc-ok ecfbef79^..ecfbef79
