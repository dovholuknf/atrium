# u-pr-paste report

## What was built

One paste now does the whole job. The three existing pieces meet in the launch dialog and one hub route.

- **Paste door** (`js/fixtures.js`). A link pasted on the board, outside any box, terminal or open dialog, opens the
  launch dialog and recognises it. A link no recogniser knows closes the dialog again and does nothing.
- **Launch makes the worktree** (`pastedPR`, `makePastedWorktree`, `launchNow`). When the recogniser that matched is a
  pull request row (tag `pull-request`, with org, repo and num captured), pressing launch calls
  `POST /v1/providers/{name}/pr-worktree` first, puts the returned path in the directory box and launches on it. The
  provider is the one with `worktrees` on whose host is the PR's host. The verb's `error` sentence is shown as is in
  the dialog's link note and nothing is launched. A directory typed over the recogniser's, the throwaway tick, or no
  matching provider skips the call and the launch goes as before.
- **Placement** (`internal/link/prworktreeroute.go`). On a hub with two or more rooms and none named, the pr-worktree
  request is placed by the PR claim: an existing claim sends it to the owner (a second paste folds in), otherwise
  `placePRRoom` picks the room and the hub records the claim, with the same key the room's own `/v1/prs` uses. The
  chosen room goes back in `X-Atrium-Placed-Room` and the dialog sends the launch to that room. The room box gains a
  "least busy" choice for a pasted PR so the first machine is not picked by default. Choosing a machine still wins.
  The body's `host` is added for the claim key and the room ignores it.
- **Tests.** `internal/link/prworktreeroute_test.go` covers least busy, fold into the owner, and a named room not
  being placed. `scripts/check-pr-paste.js` is the headless board test: it drives a real paste event, and checks the
  verb's body, the launch cwd and room, a refusal staying in the dialog, and a typed directory being left alone.
  Run it with `NODE_PATH=<dir with playwright> node scripts/check-pr-paste.js [shots-dir]`. It is not wired into
  `check-board.sh` because that script has no browser.
- Screenshots in `docs/screens/u-pr-paste/`: `before-paste.png` (the filled dialog, nothing made yet, as it was),
  `after-worktree.png` (worktree made, directory box shows its path) and `after-refused.png`.

## Decisions

- **Dialog, not the Pulls view.** The brief wants no PR list and no row. The Pulls view is made of rows, so wiring
  there would bring the list back. The dialog already has the recogniser, the room box and the launch.
- **Claim at the hub.** Folding a second paste into the owner needed the claim table, which only the hub holds, so the
  small hub route was the fewest pieces. No new storage, and `placePRRoom` is untouched.
- Pull request rows are recognised by the `pull-request` tag both shipped rows already set, so no server change and
  no new recogniser field.

## Not done

- **Provider name per room.** The dialog picks the provider by host from the merged list and sends its name. If the
  placed room has no provider of that name the room answers its own "no such provider". Names are per room, so
  this needs the same name on every room, or the hub looking the provider up per room.
- Placement and the claim happen before the worktree succeeds. A refused clone leaves the claim on that room, so a
  retry goes to the same room. It is only released by the existing move.
- A recogniser whose tags lack `pull-request` gets no worktree. Unchanged for issues and branches.
- The paste door fires once per paste even when the same PR is already open as a card. The claim folds the worktree,
  but a second card is started.
- Only `internal/link` was run. Its change-request tests fail here with `unable to access 'NUL'` from git on Windows, in files this
  work did not touch. The placement, claim and PR worktree tests pass.
