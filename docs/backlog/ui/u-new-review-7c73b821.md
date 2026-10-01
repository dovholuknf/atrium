# Review: pulls view P2 7c73b821 (@ui)

Range dafd4a5c..7c73b821, one commit on sg3/claude/pulls-p2. The desktop board's `pulls` tab over the API in
docs/rnd/pulls-api.md (3ecee7c8): rows, the `pr` event, start, abort, retry, log, the paste box, the findings list
with walk marks, the walker launch, and the nav count on the alerting path. Not built, as stated: `PUT
findings/{key}` and the walk.js re-point.

The commit is UNSIGNED (`%G?` is `N`). sg3 has no signing key. Noted as @ui asked. It is not a finding against the
code, and signing is the lander's call.

Board checks are @ui's. I read the `pulls` section of scripts/test-board-headless.js and did not run it. @ui reports
these green alone: pulls, bootClean, phoneHeader, gearTermList, topNav, eventDriven, pollsGone.

## What holds

- One render function for a list row and an event row (`pullRowHTML`), and one entry point (`pullsApplyRow`). An
  event replaces the row it names, and the index is read again when the stream reopens. No timer.
- The state words follow the API: `walking N of M` is done + skipped + deferred of the finding count, and `walked` is
  `open == 0 && deferred == 0` with findings. `running` shows `run_state` and never shows N of M. The buttons match the
  API's allowed states: abort on queued, fetching or running, retry on failed or aborted, review on queued only.
- The nav count is recounted from the rows on every change, ready plus failed, not archived. The title carries it
  through `retitleAgain`. The alert path is `alerting.check("pulls", ...)`, and the first load is a silent baseline,
  as for every other kind (`check` seeds on `prev === null`). A failed row's alert id carries `run_error`.
- Every value from a row or a finding goes through `esc`. A refusal shows the daemon's `error` sentence, and a 409 on
  a row action reads the index again.
- The headless section covers every state's words and buttons, an event without a refetch, the count after each
  change, retry, abort, a refused abort, review, all three paste answers, halted, findings, a walk mark, the walker,
  the log, a reconnect refetch, the repo filter and the empty state. Thorough.

## Findings

### 1. MEDIUM: no daemon serves /v1/prs, so a deploy ships a dead tab

`git grep '/v1/prs'` finds nothing in internal/ at 7c73b821 or on claude/main. Without the store, `GET /v1/prs`
falls through to `webHandler`, and the file server answers a plain-text `404 page not found`. The board calls
`loadPulls` on every stream open, whatever the view, so every board asks for it on every reconnect. The `pulls` tab
is always visible, and opening it shows the 404 under the header. Paste, the only thing on an empty tab, posts into
the same 404.

A hub-ok and room-ok verdict makes this deployable the moment it lands, and hub-only board deploys go out without an
ask. So the verdict cannot be OK until the routes exist or the tab handles their absence.

Fix (my pick, since it also frees the landing order): keep the tab `hidden`, as the `documents` tab is, until a
`GET /v1/prs` answers 200 with a `prs` array. On a 404 or a non-JSON answer, leave it hidden and send no further
reads until the next stream open. Add a headless case with the route unmocked: the tab stays hidden and nothing is
logged as a page error. The other way is to land after r-pr-store serves the routes, but that ties two directors'
landings together.

### 2. LOW: an open repo filter closes under the user while a review runs

`pullsPaintFilters` rebuilds `#pulls-repo` with `innerHTML` on every paint, and a paint follows every `pr` event and
every board repaint while the tab is open. A running review sends an event per step and per cost change, so a select
held open closes under the pointer. Not run. Rebuild the options only when the repo set changes.

### 3. NITS

- `loadPulls` returns at once while a read is in flight. A stream that reopens during that read loses its refetch, and
  the read in flight may predate the events it missed. Queue one more read instead of dropping it.
- The state filter's `open` means "not archived" and includes aborted and walked rows. `current` or `not archived`
  says what it does.

## Verdict

HOLD dafd4a5c..7c73b821 for finding 1. Re-read dafd4a5c..tip. The verdict will be hub-ok and room-ok.

Owed by me when P2 lands with the walk.js re-point: edit rules 6 and 10 of docs/review/review-memory-design.md for the
walk order of design 5.6.

Quality: after the Sonnet switch, no drop seen in what was built: the spec is followed field by field, and the test
covers every route it mocks. The miss is the second-order one: it was built against mocks, and what a deployed board
does with no route behind it was not asked.

## Re-read dafd4a5c..bfa9d0b3

One new commit, bfa9d0b3, on top of 7c73b821. Also unsigned, from sg3.

### Closed

- Finding 1: the tab ships `hidden` and is shown only when `GET /v1/prs` answers with a `prs` array. A plain-text
  404 throws in `api()` or fails the array check, so the tab stays hidden and the note is written to a pane nobody can
  open. The new `pullsAbsent` section loads the board against the harness, which has no `/v1/prs`. It checks that the
  read was made, that the tab stays hidden and that no page error is raised. `pulls` now also waits for the tab to show.
- Finding 2: `#pulls-repo` is rebuilt only when the repo set changes, keyed in `dataset.repos`. Otherwise only its
  value is set.
- Nit 1: a read asked for during one in flight sets `again`, and runs when the first one ends.
- Nit 2: the label reads `open (not archived)`.
- A race not in my list: a refused action's note used to be cleared by the read it asked for. Now a successful read
  clears only a note that a failed read wrote (`loadFailed`).

### New

NIT: `restoreWhereYouWere` (solo.js) reopens a remembered `pulls` view without looking at the tab. A browser that
used `pulls` on one daemon and then loads a board whose daemon has no route (a hub that does not proxy it, or an
older build) opens a view with its tab hidden, showing the 404 note. It can only happen after the tab was once
shown. Fall back to the board when the tab is hidden.

### Verdict

OK hub and room, dafd4a5c..bfa9d0b3, 1 nit.

Quality: after the Sonnet switch, no drop seen. Every point was taken, with a test for the medium, and it found and
fixed a race nobody had asked about.
