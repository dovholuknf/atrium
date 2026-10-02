# Review: u-hub-repos d3004c30 (the hub's repos tab, hub forge stage 1)

Range 1381fd98..d3004c30, read by @review on m1mini. The commit is unsigned, like every m1mini commit.

## What it does

- **The tab:** a hub-only "repos" tab, revealed from applyHubRooms beside the audit tab.
- **The data:** it reads `/_hub/git/repos` when you open the tab and on its refresh button. There is no timer and no
  stream event.
- **Each repo** shows its main sha and time, the branches rooms pushed (room, card, sha, when, released), and copy
  buttons for the ssh and http clone URLs.
- **Card titles** come from the board's own `lastTasks`, never from the answer.

## Checked

- **Escaping:** every value from the answer goes through `esc()`. That covers the host, owner and repo, the branch
  name, the sha, the room, the card, the title, the URLs and the `data-copy` attribute. The labels are constants.
  - There is no inline handler. Copy is one delegated listener reading `dataset.copy` (REVIEWER-NOTES:
    `data-*` only).
  - `location.origin + r.path` is text to copy, never a link or a navigation.
- **Helpers:** copyText, sinceSecs, ago, hubIsHub and lastTasks all exist. `node --check hubrepos.js` passes.
- **The headless unit hubRepos,** read rather than run, covers:
  - two repos, with ssh and http copy values;
  - main shown short, and "empty" for an empty repo;
  - a known card's title, and a gone card that was released;
  - a refresh.

  @ui reports hubRepos, bootClean and topNav pass alone.
- **u001Audit R1 and R2** aren't this change. That section is opt-in only (`HEADLESS_ONLY=u001Audit`), and it is
  written to fail until the u-001 wave fixes it: "checks that FAIL today", per test-board-headless.js:13689. R1 is
  the phone `?` chip and R2 is approve under the keyboard, and this commit touches neither.

## Lows

- **L1:** refresh has no in-flight guard, so two quick clicks can paint out of order. Drop a stale answer by a
  counter.
- **L2:** `plainFetch` doesn't exist in the board, so the window.fetch fallback is always taken. Name the board's
  fetch, or drop the branch.
- **L3:** built on fixtures only. When @fabric's endpoint lands, check its field names against this (path, url,
  branches[].released, main.at).

Closed: none (first read) / Open: L1, L2, L3

Verdict: HUB DEPLOY OK 1381fd98..d3004c30. It is inert until the endpoint exists, and then it says "not answering".

Quality: small and careful. All escaping is in place, it uses the data-* copy pattern, and the hub gating follows
the audit tab.
