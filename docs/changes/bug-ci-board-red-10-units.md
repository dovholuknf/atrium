# bug-ci-board-red-10-units

Run 38890140075 failed the `board` step on all three OSes. Each of the ten units was rerun on claude/main 86e6d1a0. No
board code changed: every red was the test.

| Unit | On head | Cause | Fix |
| --- | --- | --- | --- |
| clearKeepsPage | green | already fixed since 1e3826e0 | none |
| usageWeekReset | green | already fixed | none |
| hubReposBranches | red | test bug: bare `hubMode`, the section meant `mock.hubMode` | use `mock.hubMode` |
| changeReq | red switcher, contrast green | test bug: the repos switcher gained a fifth view, `branches` | expect `branches:false` |
| mChangeReq | green | already fixed (contrast) | none |
| terminalsOneLine | green | already fixed | none |
| mViewer | green | already fixed | none |
| settingsOnce | green | already counts this page's reads only | none |
| mWorking | green alone, red in a full run | race: 5 s wait for the working line is too short on a loaded runner | wait 15 s (scaled by `slow`) |
| keepAlive | green | race: one fixed 150 ms wait before reading the first replay | poll up to 6 s |

mWorking and keepAlive are timing fixes in the test, not in the board: the board paints the line and the replay as soon
as the task and the socket arrive. macOS and Windows artifacts show failed Go packages (gitsync, daemon, link) in
addition to `board`, and their summaries do not name the board units, so no other unit was identified there.

## Test plan

## @LETTER@. Headless board units named in bug-ci-board-red-10-units

### @LETTER@1. The ten units pass

1. Run `HEADLESS_UNITS=clearKeepsPage,usageWeekReset,hubReposBranches,changeReq,mChangeReq,terminalsOneLine,mViewer,settingsOnce,mWorking,keepAlive node scripts/test-board-headless.js`.
2. Every unit prints ok.
