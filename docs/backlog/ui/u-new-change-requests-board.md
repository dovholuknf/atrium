# u-new-change-requests-board: change requests and the Pushed line on the board

Status: built, in review. Owner @ui. This is the board half of scm stage 5
(`docs/rnd/hub-forge-design.md` section 6). Sits on top of `u-new-hub-repos-list`.

## What is there

- **Requests view** in the repos area, the fourth switch beside shelf, ledger and feed. List (open and closed), a
  request's page, open a request from a pushed branch, close with a note, withdraw, and record that it was merged.
- **Needs the orchestrator or clint.** An open request into `main` is merged on the hub's side for now. The page says so
  in a strip and offers a sha field. The board never merges: "Record that it was merged" only writes down the sha the
  operator gives it.
- **Pushed line** from the hub's push log (`/_hub/git/pushed`): pushed and matching, behind, ahead, diverged, or not
  pushed, with room, card and time. It shows on a request's page (in the "On the hub" panel, to the right of the change
  record on a wide screen), on the ledger's branch rows as a chip, and in the header of /m's changes sheet.
- **Phone**: a requests door in /m with list and page screens, the same states, built with `textContent` only.

## Rules the board keeps

- Title, why, notes, branch names are text, never markup.
- Read-only without a write: a 401, or a 403 whose own words say the board is a read-only share, hides every write and
  says why. Any other 403 is that action's error.
- The sha for `merged` is a full 40 (or 64) hex, the hub's `ValidSHA`, and the field hint says so.
- Offline, an old hub and a branch the push log has never seen each read right.

## The mock

`js/changereq-mock.js` is a stand-in for the draft routes, one file to delete when the hub answers. It is on only for
a page loaded with `?crmock=1`, never remembered, and while it is on a fixed banner reads "Mock data: nothing here
reaches the hub" with a Turn off button.

## Open for @fabric

- A `can_write` flag on the list would let the board hide writes up front instead of learning it from a refusal.
- `/v1/tasks/{id}/changes` needs `repo`, `branch` and `head` for the phone's Pushed line.
- The change record's four lines are not served to the board yet, so the attached record is mock-only.
- List rows carry no `pushed`; the page fetches it per request.

Tests: `changeReq` and `mChangeReq`, each alone, with mutants of the new asserts.
