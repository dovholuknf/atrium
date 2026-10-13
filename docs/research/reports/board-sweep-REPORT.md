# Board sweep, 2026-10-10

Read only. Nothing was exited, culled, archived, deleted or moved. Snapshot taken 2026-10-10 03:50 to 04:10 UTC (2026-10-09 23:50 local).

| Tier | Cards |
| --- | --- |
| 1 REMOVE | 244 |
| 2 PROBABLY REMOVE | 147 |
| 3 KEEP | 36 |
| 4 CLINT CHECKS | 63 |
| total | 490 (of which 61 are on the stack) |

## How I looked

- Cards: the hub's `/v1/tasks` for each of the four rooms (claude-sg4, m1mini, sg3, sg4-control), sent with the `X-Atrium-Room` header. The board has 490 cards.
- Stack: the `/v1/waiting` list of each room. It is the same cards as the board, filtered to those waiting on clint, so all 61 stack items are in the tables below, marked `(stack)` in the status column. There was no separate stack-only item. `/v1/prs` (the pulls tab) is empty on every room.
- A branch is **merged** when its tip is an ancestor of `claude/main-before-scrub` (MERGED-OLD) or of `claude/main` (MERGED-NEW). A branch is **landed by patch** when every commit of it since the old tip has its patch-id on `claude/main` (squashes and rewrites). A report sha counts when it is an ancestor of either tip or its patch-id is on `claude/main`. A merge commit `Merge claude/<name>` on the old main also counts.
- Branches on m1mini and sg3 that were never pushed to the hub are not visible here. Those cards fall back on their report sha, the branch named in the title, or a merge commit on old main.
- A card branch named `claude/main`, `main` or `hub-main` is no evidence (it is the base), and is ignored.
- Evidence I could not get: GitHub state of any upstream PR (only `git fetch hub` is allowed), and whether a session is alive beyond the hub's own status. `done` means the card exited; `needs-input` is a parked or waiting session.

## 1. REMOVE (244)

| Card title | Room | Card id | Status | Last activity (UTC) | Evidence |
| --- | --- | --- | --- | --- | --- |
| u-context-cleared-line: a "context cleared" divider where a session was cleared | claude-sg4 | 01a12234-334a-7662-97cd-b775e57892b9 | done | 2026-10-09 19:54 | branch claude/u-context-cleared-line tip is an ancestor of claude/main-before-scrub |
| u-peek-hover-rest: the card peek opens only once the mouse rests | claude-sg4 | 01a12236-6999-7982-ac2a-4e6e4886152b | done | 2026-10-09 19:54 | branch claude/u-peek-hover-rest tip is an ancestor of claude/main-before-scrub |
| u-peek-status-truth: the peek says the truth about status, recap age and the ... | claude-sg4 | 01a12216-f2ab-7e60-a0bc-8b013266e022 | done | 2026-10-09 19:27 | branch claude/u-peek-status-truth tip is an ancestor of claude/main-before-scrub |
| ops-land-1009c: review and land the peek/docs/switcher and clear-mark branches | claude-sg4 | 01a121d0-a484-7478-9999-5643f8f73a4b | done | 2026-10-09 18:04 | ops card, report sha is an ancestor of claude/main-before-scrub |
| u-clear-keeps-screen: a screen clear pushes the screen into scrollback on the... | claude-sg4 | 01a121a7-cdcf-7b06-b55c-8f339eef58bb | done | 2026-10-09 17:57 | branch claude/u-clear-keeps-screen tip is an ancestor of claude/main-before-scrub |
| u-docs-peek-switcher: documents page polish, a small card peek, a clean switcher | claude-sg4 | 01a1219a-4c01-7505-90f9-ba82255c2ef7 | done | 2026-10-09 17:15 | branch claude/u-docs-peek-switcher tip is an ancestor of claude/main-before-scrub |
| r-paste-empty-image: a pasted screenshot sometimes lands as a 0-byte file | claude-sg4 | 01a1219c-84d2-73a7-96c4-71c414b43732 | done | 2026-10-09 17:03 | branch claude/r-paste-empty-image tip is an ancestor of claude/main-before-scrub |
| rnd-gate-exe-eval: should the PreToolUse gate be a standalone exe or `atrium ... | claude-sg4 | 01a12120-1b82-7569-871b-d5ca9593d9df | done | 2026-10-09 14:48 | branch claude/rnd-gate-exe-eval tip is an ancestor of claude/main-before-scrub |
| ops-deploy-1009b: deploy 8f204edb to the hub, sg3 and m1mini | claude-sg4 | 01a120ff-535f-7168-9fab-825c5d1b50da | done | 2026-10-09 14:13 | one-off ops card, its sha 8f204edb is on main |
| ops-land-1009b: merge the typing fixes and the interview launch kind | claude-sg4 | 01a120e8-3c4a-775e-a3c3-9674e8b05905 | done | 2026-10-09 14:08 | ops card, branch claude/ops-land-1009b tip is an ancestor of claude/main-before-scrub |
| r-typing-fixes: atrium stops typing into agents when it should not | claude-sg4 | 01a120bc-4823-7db8-ab36-f8e0ee59a06d | done | 2026-10-09 13:41 | branch claude/r-typing-fixes tip is an ancestor of claude/main-before-scrub |
| f-interview-launch-kind: interviews and durable work codified in atrium | claude-sg4 | 01a120c0-c1d2-707e-bb86-91fbc34840d9 | done | 2026-10-09 13:36 | branch claude/f-interview-launch-kind tip is an ancestor of claude/main-before-scrub |
| ops-land-1009: land restart menu + wake fix, deploy hub | claude-sg4 | 01a1204a-bb03-7811-a256-1c21d3fd1130 | done | 2026-10-09 12:32 | ops card, report sha's patch is on claude/main (patch-id) |
| r-no-wasted-wakes: wakes that deliver, and workers that never park on a backg... | claude-sg4 | 01a12028-38e4-7b15-a20f-6ac01ceef218 | done | 2026-10-09 10:50 | every commit of branch claude/r-no-wasted-wakes has its patch on claude/main (patch-id) |
| u-paste-in-switcher: paste a link from the Ctrl+K bar, with Enter and a button | claude-sg4 | 01a11d6f-55be-7ae6-acda-b1da21f7513c | done | 2026-10-09 02:50 | every commit of branch claude/u-paste-in-switcher has its patch on claude/main (patch-id) |
| ops-deploy-81e8696b: sg3 and m1mini to claude/main 81e8696b | claude-sg4 | 01a11db7-467b-7181-a1fb-5c8b287ffc6c | done | 2026-10-09 02:41 | ops card, report sha's patch is on claude/main (patch-id) |
| u-cycle-typing-queues: a context cycle never locks the operator out | claude-sg4 | 01a11d6f-78be-7822-82f7-7a88097d2a51 | done | 2026-10-08 22:11 | report sha's patch is on claude/main (patch-id) |
| u-terminal-last-read-marker: "new since you were here" line in terminals | claude-sg4 | 01a11d6f-9cf9-74ef-893a-33b0dfe01522 | done | 2026-10-08 21:37 | every commit of branch claude/u-terminal-last-read-marker has its patch on claude/main (patch-id) |
| ops-land-graceful: review, land and deploy r-graceful-room-restart (hub + sg3) | claude-sg4 | 01a11bd7-0113-7994-9b55-bed13f5bc274 | done | 2026-10-08 14:34 | ops card, branch claude/r-graceful-room-restart (named in the title) has every patch on claude/main |
| ops-deploy-1008: deploy m1mini, sg3, sg4-control rooms to 8918f846 | claude-sg4 | 01a11b60-62e9-76dc-afed-b6b5393f2a4f | done | 2026-10-08 12:08 | one-off ops card, its sha 8918f846 is on main |
| t-ci-green-for-good: CI on main green on Windows and Linux, and kept green | claude-sg4 | 01a1194d-d459-79b5-9dfd-97a64718191e | needs-input (stack) | 2026-10-08 05:36 | every commit of branch claude/t-ci-green-for-good has its patch on claude/main (patch-id) |
| H1 t-daemon-tests-hang: daemon tests hang to the timeout | claude-sg4 | 01a1183f-d975-7901-b55a-3ad647237073 | done | 2026-10-08 02:18 | every commit of branch claude/t-daemon-db-held-at-cleanup has its patch on claude/main (patch-id) |
| H14 r-hooks-all-in-atrium: the PowerShell guard hook becomes `atrium hook` | claude-sg4 | 01a1185c-5336-722e-9ba7-4fc1e0a07907 | needs-input (stack) | 2026-10-08 02:05 | every commit of branch claude/r-hooks-all-in-atrium has its patch on claude/main (patch-id) |
| H2 f-git-window-flash: git console windows flash on the desktop | claude-sg4 | 01a1183f-ff8f-7a2e-84e2-835bd25f22e4 | needs-input (stack) | 2026-10-08 00:29 | every commit of branch claude/f-git-window-flash has its patch on claude/main (patch-id) |
| f-hub-mirror-main-goes-stale | claude-sg4 | 01a117c9-d8b8-7a38-82b8-1949193e1dba | needs-input (stack) | 2026-10-07 21:53 | every commit of branch claude/f-hub-mirror-main-goes-stale has its patch on claude/main (patch-id) |
| r-comms-one-instruction | claude-sg4 | 01a117ce-7d64-74af-b7a1-0a82a484be7c | needs-input (stack) | 2026-10-07 21:48 | every commit of branch claude/r-comms-one-instruction has its patch on claude/main (patch-id) |
| t-ci-why: why ci fails on ubuntu at 80d2bb30 (diagnose only) | claude-sg4 | 01a11786-90cd-7ea8-acf0-b58b2a8e14eb | needs-input (stack) | 2026-10-07 21:15 | every commit of branch claude/t-ci-why has its patch on claude/main (patch-id) |
| r-recognisers-gwt: recognisers that work, modelled on gwt | claude-sg4 | 01a11745-4131-7b75-8af1-111fc5e98693 | needs-input (stack) | 2026-10-07 19:10 | every commit of branch claude/r-recognisers-gwt has its patch on claude/main (patch-id) |
| r-paste-a-link-card-lifecycle | claude-sg4 | 01a115fe-47b9-703b-bb22-82ee870e9b91 | done | 2026-10-07 16:29 | report sha's patch is on claude/main (patch-id) |
| t-ci-fix: fix failing GitHub Actions run 37531545156 | claude-sg4 | 01a116fd-bd3f-7382-bd57-a092927eb8de | done | 2026-10-07 16:29 | every commit of branch claude/t-ci-fix has its patch on claude/main (patch-id) |
| test | claude-sg4 | 01a10de2-f25b-7d0a-86d8-d32101373150 | done | 2026-10-05 21:08 | throwaway/test card: done, no branch, no commits |
| @ui director, night 10-04 | claude-sg4 | 01a109d3-293a-7ad0-9e11-142a63f76e19 | needs-input (stack) | 2026-10-05 15:41 | every commit of branch claude/director-ui has its patch on claude/main (patch-id) |
| u-rooms-pill-setup-drift: warn on the rooms pill when rooms are set up differ... | claude-sg4 | 01a10c27-e1d1-7386-b4a5-3268b50a4e17 | done | 2026-10-05 13:20 | every commit of branch claude/u-rooms-pill-setup-drift has its patch on claude/main (patch-id) |
| u-read-tab-text-size: A- / A+ buttons on the .md read tab | claude-sg4 | 01a10c36-cff8-7c57-8932-b404235ec478 | done | 2026-10-05 13:19 | every commit of branch claude/u-read-tab-text-size has its patch on claude/main (patch-id) |
| r-keepalive-stale-badge: a stopped badge clears on a real turn even while the... | claude-sg4 | 01a10c07-2cbc-728d-8a79-0d0e00573b43 | done | 2026-10-05 12:28 | every commit of branch claude/r-keepalive-stale-badge has its patch on claude/main (patch-id) |
| u-file-view-per-card | claude-sg4 | 01a10bf1-d098-7463-9339-f0a91ab1ffbb | done | 2026-10-05 12:09 | every commit of branch claude/u-file-view-per-card has its patch on claude/main (patch-id) |
| @fabric director, night 10-04 | claude-sg4 | 01a109d3-7572-7a4a-ac65-28d20be1185f | needs-input (stack) | 2026-10-05 10:11 | every commit of branch claude/director-fabric has its patch on claude/main (patch-id) |
| @runtime director, night 10-04 | claude-sg4 | 01a109d3-51e6-7040-8f52-00699d56bb00 | needs-input (stack) | 2026-10-05 08:25 | every commit of branch claude/director-runtime has its patch on claude/main (patch-id) |
| queue2: restartgate flake, prove it | claude-sg4 | 01a10a53-6aa6-7cf2-8ae7-6751978ad309 | needs-input (stack) | 2026-10-05 08:11 | every commit of branch claude/f-new-queue2-launcher-pin-and-restartgate-flake has its patch on claude/main (patch-id) |
| @rnd director, night 10-04: the hub PR review workflow | claude-sg4 | 01a109d3-b062-73b8-8f09-d799bce6f294 | needs-input (stack) | 2026-10-05 06:58 | every commit of branch claude/director-rnd has its patch on claude/main (patch-id) |
| r-new-resume-says-continue | claude-sg4 | 01a10a62-49ed-7b8b-8d35-62788a48a4b7 | done | 2026-10-05 04:56 | every commit of branch claude/r-new-resume-says-continue has its patch on claude/main (patch-id) |
| u-008 | claude-sg4 | 01a10a5f-41ce-7e59-b04c-0eff6dce6bd5 | done | 2026-10-05 04:50 | every commit of branch claude/u-008 has its patch on claude/main (patch-id) |
| r-new-sec-auth-guard | claude-sg4 | 01a10a54-2d1d-7f12-9240-0c06adf64359 | done | 2026-10-05 04:45 | every commit of branch claude/r-new-sec-auth-guard has its patch on claude/main (patch-id) |
| u-11 | claude-sg4 | 01a10a5c-e8eb-7e2f-aee7-8a04996a985d | done | 2026-10-05 04:42 | every commit of branch claude/u-11 has its patch on claude/main (patch-id) |
| u-new-terminals-css-row-bleed-resize | claude-sg4 | 01a109ec-79ec-7aee-a8df-955317ac2c77 | needs-input (stack) | 2026-10-05 04:40 | every commit of branch claude/u-new-terminals-css-row-bleed-resize has its patch on claude/main (patch-id) |
| u-002 | claude-sg4 | 01a10a5a-18ca-7300-bacc-db12764f9bcd | done | 2026-10-05 04:39 | every commit of branch claude/u-002 has its patch on claude/main (patch-id) |
| u-002 | claude-sg4 | 01a10a57-6020-7053-8730-66637b1b018f | done | 2026-10-05 04:37 | every commit of branch claude/u-002 has its patch on claude/main (patch-id) |
| 75: finish, git_root verb | claude-sg4 | 01a10a53-48ce-7e76-b068-f160eb404a02 | done | 2026-10-05 04:34 | every commit of branch claude/75 has its patch on claude/main (patch-id) |
| u-003 | claude-sg4 | 01a10a52-fd0b-7dc0-86e8-1f5633ae47f3 | done | 2026-10-05 04:33 | every commit of branch claude/u-003 has its patch on claude/main (patch-id) |
| r-new-sec-go-toolchain | claude-sg4 | 01a10a46-7210-7bf2-a786-429084808c48 | done | 2026-10-05 04:30 | every commit of branch claude/r-new-sec-go-toolchain has its patch on claude/main (patch-id) |
| u-new-web-push-build | claude-sg4 | 01a10a4c-16a4-703c-a6bd-39ca70b1a4f4 | done | 2026-10-05 04:28 | every commit of branch claude/u-new-web-push-build has its patch on claude/main (patch-id) |
| u-new-web-push-build | claude-sg4 | 01a10a35-83d4-7d3d-9741-e97ad1dbf054 | done | 2026-10-05 04:21 | every commit of branch claude/u-new-web-push-build has its patch on claude/main (patch-id) |
| f-029: notify input gate, prune body cut | claude-sg4 | 01a10a40-3032-78bb-9318-0c75711dd68c | done | 2026-10-05 04:16 | every commit of branch claude/f-029 has its patch on claude/main (patch-id) |
| 75: localai check and shared folder in provisioning | claude-sg4 | 01a10a41-c6c4-7cf6-898a-d9e6caa7c2e9 | done | 2026-10-05 04:15 | every commit of branch claude/75 has its patch on claude/main (patch-id) |
| r-new-forge-alert-test-order | claude-sg4 | 01a10a32-6e50-7840-85fe-262eb64c3d4e | done | 2026-10-05 04:15 | every commit of branch claude/r-new-forge-alert-test-order has its patch on claude/main (patch-id) |
| f-new-queue2: launcher pin and restartgate flake | claude-sg4 | 01a10a35-e0a9-7897-aefe-dd6b68b79c93 | done | 2026-10-05 04:10 | every commit of branch claude/f-new-queue2-launcher-pin-and-restartgate-flake has its patch on claude/main (patch-id) |
| f-025: hubstore notify test gaps, two script smalls | claude-sg4 | 01a10a3d-4ecd-70d2-bd0a-f23b811a0497 | done | 2026-10-05 04:08 | every commit of branch claude/f-025 has its patch on claude/main (patch-id) |
| f-new-d1-nits: @review's D1 nits | claude-sg4 | 01a10a3b-8548-730a-8200-26d9d6546da1 | done | 2026-10-05 04:05 | every commit of branch claude/f-new-d1-nits has its patch on claude/main (patch-id) |
| f-new-review-8df971e4: rewrap | claude-sg4 | 01a10a34-b13d-770d-88a1-9cb821ae9b49 | done | 2026-10-05 03:57 | every commit of branch claude/f-new-review-8df971e4 has its patch on claude/main (patch-id) |
| f-new-review-8df971e4: ATRIUM_HOSTS lows | claude-sg4 | 01a10a32-fa42-7a5a-b6a8-251ad333acbb | done | 2026-10-05 03:56 | every commit of branch claude/f-new-review-8df971e4 has its patch on claude/main (patch-id) |
| u-new-terminals-css-row-bleed-resize | claude-sg4 | 01a10a33-886d-70dd-a831-346f7e60cdb2 | done | 2026-10-05 03:56 | every commit of branch claude/u-new-terminals-css-row-bleed-resize has its patch on claude/main (patch-id) |
| r-git-safe-directory | claude-sg4 | 01a10a0c-a0f9-7dac-a971-7174d9192836 | done | 2026-10-05 03:32 | every commit of branch claude/r-git-safe-directory has its patch on claude/main (patch-id) |
| u-new-web-push-build | claude-sg4 | 01a10a12-a280-7a71-9752-c0b18c517058 | done | 2026-10-05 03:28 | merge commit on claude/main-before-scrub: Merge branch 'claude/u-new-web-push-build' into claude/main |
| u-new-details-debug-section | claude-sg4 | 01a10a06-eb27-788d-a3f4-cea5fe2b51be | done | 2026-10-05 03:18 | every commit of branch claude/u-new-details-debug-section has its patch on claude/main (patch-id) |
| u-new-growler-stack-fold | claude-sg4 | 01a109f7-58bc-7b3f-8984-525d25acbd92 | done | 2026-10-05 03:06 | every commit of branch claude/u-new-growler-stack-fold has its patch on claude/main (patch-id) |
| u-new-context-bar-on-rows | claude-sg4 | 01a109f3-16b6-76ab-aca3-3cc16d3d59c7 | done | 2026-10-05 02:49 | every commit of branch claude/u-new-context-bar-on-rows has its patch on claude/main (patch-id) |
| u-new-gate-readout-only-when-blocking | claude-sg4 | 01a109ef-3a3c-718a-ae54-3cf802797a01 | done | 2026-10-05 02:44 | every commit of branch claude/u-new-gate-readout-only-when-blocking has its patch on claude/main (patch-id) |
| u-new-terminals-css-row-bleed-resize | claude-sg4 | 01a109d5-e31a-763a-93fd-807cac85129b | done | 2026-10-05 02:37 | every commit of branch claude/u-new-terminals-css-row-bleed-resize has its patch on claude/main (patch-id) |
| r-new-room-response-time | claude-sg4 | 01a109d7-12e1-7ee4-bad2-7101ad717d64 | done | 2026-10-05 02:18 | every commit of branch claude/r-new-room-response-time has its patch on claude/main (patch-id) |
| r-new-edited-input-unparsed | claude-sg4 | 01a109d5-7b2b-73f4-a08a-8b4a31b9fb64 | done | 2026-10-05 02:13 | every commit of branch claude/r-new-edited-input-unparsed has its patch on claude/main (patch-id) |
| review: director of software review, on claude/review | claude-sg4 | 01a0f281-51d2-717e-9803-fa9f1b7bdd5c | done | 2026-10-01 19:09 | every commit of branch claude/review-runtime-0930 has its patch on claude/main (patch-id) |
| fabric: director of rooms, hub, cross-room and provisioning | claude-sg4 | 01a0eadc-c358-725f-bbb0-f06231c08fe1 | done | 2026-10-01 19:05 | every commit of branch claude/fabric has its patch on claude/main (patch-id) |
| rnd: director of research and design, on claude/rnd | claude-sg4 | 01a0f274-7bc4-7998-91ad-774c432c10e5 | done | 2026-10-01 18:35 | every commit of branch claude/rnd has its patch on claude/main (patch-id) |
| ui: director of the board, on claude/ui | claude-sg4 | 01a0f276-7403-7dde-8655-215396368b07 | done | 2026-10-01 18:33 | every commit of branch claude/ui-next has its patch on claude/main (patch-id) |
| r-msg-m1: the typing model can never wedge shut (messages plan, phase M1) | m1mini | 01a122f5-963a-70ab-a487-363bb72b026d | done | 2026-10-09 23:23 | branch claude/r-msg-m1 tip is an ancestor of claude/main-before-scrub |
| r-plan-messages-tests: plan for reliable message delivery and fast, hang-free... | m1mini | 01a12291-4d76-7084-befc-a8add798ff01 | done | 2026-10-09 21:45 | branch claude/r-plan-messages-tests tip is an ancestor of claude/main-before-scrub |
| r-codeman-comparison: atrium vs Codeman (Ark0N/Codeman) | m1mini | 01a12286-3261-7674-9125-451c81c3523a | done | 2026-10-09 21:22 | branch claude/r-codeman-comparison tip is an ancestor of claude/main-before-scrub |
| u-drag-no-click: a drag-select never fires a click; copy icon after the termi... | m1mini | 01a1226b-9014-744d-863e-83a5066d9efe | done | 2026-10-09 20:49 | branch claude/u-drag-no-click tip is an ancestor of claude/main-before-scrub |
| review u-paste-in-switcher 343bc518 | m1mini | 01a11d87-7778-72bb-a929-f7a31702cbaf | done | 2026-10-09 02:51 | branch claude/u-paste-in-switcher (named in the title) has every patch on claude/main |
| f-hub-ci-runs: the hub reads CI runs and failed-job logs for any room | m1mini | 01a11d86-29f9-72c3-9aa9-6cc9aa0ade06 | done | 2026-10-08 22:36 | branch claude/f-hub-ci-runs (named in the title) has every patch on claude/main |
| re-review f-hub-ci-runs a253fcb9 | m1mini | 01a11da1-7d38-7776-bfd7-380c1a300631 | done | 2026-10-08 22:31 | branch claude/f-hub-ci-runs (named in the title) has every patch on claude/main |
| re-review f-hub-ci-runs 68b8ab83 | m1mini | 01a11d99-9cca-7222-b57c-9c2f7951a14a | done | 2026-10-08 22:23 | branch claude/f-hub-ci-runs (named in the title) has every patch on claude/main |
| review f-hub-ci-runs 80cc8c38 | m1mini | 01a11d91-784f-7497-b656-efb2c831db63 | done | 2026-10-08 22:14 | branch claude/f-hub-ci-runs (named in the title) has every patch on claude/main |
| review r-agent-loop-tools 78393116 | m1mini | 01a11d74-e47e-76ab-b070-1c2416ae28c5 | done | 2026-10-08 21:46 | branch claude/r-agent-loop-tools (named in the title) has every patch on claude/main |
| review f-room-spec-d3 228aa118 | m1mini | 01a11d74-baee-71d4-a182-a6e536224a25 | done | 2026-10-08 21:40 | branch claude/f-room-spec-d3 (named in the title) has every patch on claude/main |
| ops-suite-68cda4c7: full go test on the f-room-spec landing | m1mini | 01a11cde-dcbc-731c-b0d7-85963efc668b | done | 2026-10-08 20:24 | ops card, branch claude/f-room-spec (named in the title) has every patch on claude/main |
| f-room-spec-hub: the hub keeps each room's spec and lock (phase B) | m1mini | 01a11ca5-e314-7461-9093-99cc4db1521f | done | 2026-10-08 18:36 | every commit of branch claude/f-room-spec-hub has its patch on claude/main (patch-id) |
| u-qa-ctrl-enter-sends: ctrl+enter on the last answer sends to the agent | m1mini | 01a11cb4-3c4c-7f1b-865f-adc39dfc9d82 | done | 2026-10-08 18:10 | every commit of branch claude/u-qa-ctrl-enter-sends has its patch on claude/main (patch-id) |
| f-paste-least-busy-scm: a pasted link never lands on a room that cannot clone | m1mini | 01a11beb-396f-701b-9d9d-bd58876191e3 | done | 2026-10-08 14:50 | every commit of branch claude/f-paste-least-busy-scm has its patch on claude/main (patch-id) |
| u-dropdowns-instant: every board dropdown opens with its options already there | m1mini | 01a11be9-b66f-7ec1-a375-704ef927e3a7 | done | 2026-10-08 14:31 | every commit of branch claude/u-dropdowns-instant has its patch on claude/main (patch-id) |
| r-graceful-room-restart: busy cards wrap up, get a restart wake, then the roo... | m1mini | 01a11bc8-e869-7dac-8216-f1a871b8c3ed | done | 2026-10-08 14:05 | every commit of branch claude/r-graceful-room-restart has its patch on claude/main (patch-id) |
| r-cycle-before-runner-compacts: atrium clears a card before its runner compac... | m1mini | 01a11b62-d694-7812-937a-8296f1f8223a | done | 2026-10-08 13:03 | branch claude/r-cycle-before-runner-compacts (named in the title) has every patch on claude/main |
| t-no-pwsh-on-linux: CI and tests never run PowerShell on Linux or macOS | m1mini | 01a11b62-a0f4-71dc-85da-8871eacb0f13 | done | 2026-10-08 12:12 | branch claude/t-no-pwsh-on-linux (named in the title) has every patch on claude/main |
| r-expected-end-notices: no "ended without a final report" for ends atrium cau... | m1mini | 01a1192e-7f09-77bc-aefc-c3c53064a283 | done | 2026-10-08 02:06 | branch claude/r-expected-end-notices (named in the title) has every patch on claude/main |
| H13 u-peek-answer-flyout: answer a card's open questions from the peek | m1mini | 01a11844-8590-760e-9bb7-3bd319f06b69 | done | 2026-10-07 22:45 | branch claude/u-peek-answer-flyout (named in the title) has every patch on claude/main |
| H7 r-nothing-gets-lost: agent messages, questions and reports never vanish | m1mini | 01a11840-ab2a-73dc-af8e-a96383a2aca9 | done | 2026-10-07 22:45 | branch claude/r-nothing-gets-lost (named in the title) has every patch on claude/main |
| H15 r-turn-cost-by-cause: what each atrium-caused turn cost, by cause | m1mini | 01a1184c-2093-7a29-9b25-56d8e2f75300 | done | 2026-10-07 21:45 | branch claude/r-turn-cost-by-cause (named in the title) has every patch on claude/main |
| H8 r-context-always-cycles: resumed and pinned cards run past 200k | m1mini | 01a11840-ccae-770c-86c6-0e4a01dfc9c1 | done | 2026-10-07 21:40 | branch claude/r-context-always-cycles (named in the title) has every patch on claude/main |
| u-card-colors-on-hub | m1mini | 01a11243-3ca0-7459-85c4-eb12a2c94f37 | done | 2026-10-06 18:03 | every commit of branch claude/u-card-colors-on-hub has its patch on claude/main (patch-id) |
| u-border-colour-lag | m1mini | 01a11257-8924-7591-b043-b43a3d8205d9 | done | 2026-10-06 17:53 | every commit of branch claude/u-border-colour-lag has its patch on claude/main (patch-id) |
| u-peek-launch-short | m1mini | 01a11243-80d1-7874-9a2b-991d6ac5b6a7 | done | 2026-10-06 17:39 | every commit of branch claude/u-peek-launch-short has its patch on claude/main (patch-id) |
| u-pin-tests: restore old pinned-card hide tests | m1mini | 01a0ff95-2529-7dcf-ad56-023a9d677b85 | done | 2026-10-05 19:56 | merge commit on claude/main-before-scrub: Merge claude/u-pin-tests b343ac5d: restore the pinned-card hide tests after the revert |
| ops-m1mini-deploy-fdfa4d8 | m1mini | 01a10d8e-8ab3-7b37-92d7-b6e1c04d4e6c | done | 2026-10-05 19:34 | ops card, report sha's patch is on claude/main (patch-id) |
| r-context-cycle: one limit, one terse cycle, atrium ready, no nudge | m1mini | 01a10ca2-7262-7f07-a79f-103ac5a096a1 | done | 2026-10-05 16:01 | every commit of branch claude/r-context-cycle has its patch on claude/main (patch-id) |
| review: director of software review, on claude/review | m1mini | 01a0f8d9-36c0-75e3-98a2-acd5825c13ac | done | 2026-10-05 15:40 | every commit of branch claude/review has its patch on claude/main (patch-id) |
| r-context-nudge-handoff: the context nudge blocks the handoff write | m1mini | 01a10c6c-2f93-7c96-8706-9c6d99bfc178 | done | 2026-10-05 14:17 | every commit of branch claude/r-context-nudge-handoff has its patch on claude/main (patch-id) |
| u-file-open-outside: open in system, open in a tab, render .md | m1mini | 01a10c06-3013-75f7-a7ca-9ddf8fb36310 | done | 2026-10-05 12:42 | report sha's patch is on claude/main (patch-id) |
| u-new-board-perf-workup | m1mini | 01a10a49-94c8-76b2-8cc5-0545ab626a21 | done | 2026-10-05 05:49 | report sha's patch is on claude/main (patch-id) |
| r-new-move-card-between-rooms (2) | m1mini | 01a10a6c-c728-77c4-a2d7-a0ff933cbf42 | done | 2026-10-05 05:08 | report sha's patch is on claude/main (patch-id) |
| r-new-move-card-between-rooms | m1mini | 01a10a66-5121-7537-aec4-442d0cf0a88a | done | 2026-10-05 04:57 | report sha's patch is on claude/main (patch-id) |
| r-new-held-message-escalation | m1mini | 01a10a61-05db-729b-a82a-fee4c7f7021d | done | 2026-10-05 04:49 | report sha's patch is on claude/main (patch-id) |
| r-new-sec-room-state-modes | m1mini | 01a10a5a-a3ba-7cd9-b01b-ab4416eea41f | done | 2026-10-05 04:43 | report sha's patch is on claude/main (patch-id) |
| u-new-board-gpu-cpu-fix | m1mini | 01a10a3a-dfea-75c0-b926-4e94a9589cfe | done | 2026-10-05 04:17 | report sha's patch is on claude/main (patch-id) |
| u-pr-move-action | m1mini | 01a10a38-2c63-7b90-b948-72928f70f2ba | done | 2026-10-05 04:08 | report sha's patch is on claude/main (patch-id) |
| f-003: resources inventory, stage 1 | m1mini | 01a10a32-db37-7084-a0be-d9aa2bfbb284 | done | 2026-10-05 04:02 | report sha's patch is on claude/main (patch-id) |
| u-new-resume-spinner | m1mini | 01a10a34-1c56-798d-835c-6f47890bbc7d | done | 2026-10-05 04:00 | report sha's patch is on claude/main (patch-id) |
| u-walk-phone-shots | m1mini | 01a10a32-de57-70c7-aa56-0eee1dab4f00 | done | 2026-10-05 04:00 | report sha's patch is on claude/main (patch-id) |
| u-new-reply-suggestions-build | m1mini | 01a10a0e-a084-7119-936c-f69db00f537f | done | 2026-10-05 03:23 | report sha's patch is on claude/main (patch-id) |
| u-new-browser-prefs-every-window | m1mini | 01a10a04-b14d-7055-bc32-f201209c01ba | done | 2026-10-05 03:14 | report sha's patch is on claude/main (patch-id) |
| u-new-keepalive-pie | m1mini | 01a10a01-af61-7835-89f7-f9bbdc33f781 | done | 2026-10-05 03:03 | report sha's patch is on claude/main (patch-id) |
| u-new-no-question-reminders | m1mini | 01a109fb-4603-7d2b-ba90-60dd4d52f597 | done | 2026-10-05 03:00 | report sha's patch is on claude/main (patch-id) |
| u-new-resume-spinner | m1mini | 01a109e2-9ffc-72bb-98de-0e3a256372b8 | done | 2026-10-05 02:53 | report sha's patch is on claude/main (patch-id) |
| r-hub-forge: only the hub talks to github and bitbucket | m1mini | 01a109d1-93a3-7a44-94b7-bb3bd8368897 | done | 2026-10-05 02:46 | report sha's patch is on claude/main (patch-id) |
| u-new-nest-indicator | m1mini | 01a109e1-c15b-77a5-9388-7c2317966c3e | done | 2026-10-05 02:26 | report sha's patch is on claude/main (patch-id) |
| u-new-usage-weekly-reset | m1mini | 01a109da-40ae-73a9-bdf1-88d0ad53b63b | done | 2026-10-05 02:25 | report sha's patch is on claude/main (patch-id) |
| r-new-telegram-notify-build | m1mini | 01a109d9-4712-7da4-a1c0-9eb6554f48b4 | done | 2026-10-05 02:17 | report sha's patch is on claude/main (patch-id) |
| u-new-nest-indicator | m1mini | 01a109d5-ad22-7115-815d-b78e10245b1e | done | 2026-10-05 02:16 | report sha's patch is on claude/main (patch-id) |
| r-new-macos-cpu | m1mini | 01a109d6-8566-78d5-b3c3-c67bba8cc142 | done | 2026-10-05 02:15 | report sha's patch is on claude/main (patch-id) |
| r-forge-wire: forge access alert wired, private PR fetch | m1mini | 01a10991-4ace-7c54-9ce8-52a09a8e0915 | done | 2026-10-05 01:04 | report sha's patch is on claude/main (patch-id) |
| r-forge-bb: Bitbucket forge | m1mini | 01a10991-725a-7069-adb3-a8728d325852 | done | 2026-10-05 01:00 | report sha's patch is on claude/main (patch-id) |
| f-pr-placement: which room runs a PR, one row per PR across rooms | m1mini | 01a108e9-81a6-7e11-aef5-8164f0c48d3a | done | 2026-10-04 22:08 | report sha's patch is on claude/main (patch-id) |
| r-pr-worktree: a worktree for a PR, code from the hub when the room has none | m1mini | 01a108f1-c4f2-74f5-a11d-9b4445fce920 | done | 2026-10-04 22:06 | report sha's patch is on claude/main (patch-id) |
| r-forge-gh: forge interface and the gh implementation | m1mini | 01a108e9-3c26-7d98-ad1d-1c644578e4a9 | done | 2026-10-04 22:03 | report sha's patch is on claude/main (patch-id) |
| r-scm-recognisers: ship the GitHub and Bitbucket recogniser rows and loader | m1mini | 01a108c4-a1a4-7ce0-97b0-e0f6016858b4 | done | 2026-10-04 21:16 | report sha's patch is on claude/main (patch-id) |
| rnd-scm-forge: a provider that knows its forge (design spike) | m1mini | 01a108bc-a728-75b1-a95f-ffab9e8475b9 | done | 2026-10-04 21:11 | report sha's patch is on claude/main (patch-id) |
| u-row-flood: remove LAND badge, ctx line as a thin bar not a row tint | m1mini | 01a0ff7d-0a4c-7d32-a94a-d5a10af18abf | needs-input (stack) | 2026-10-03 04:43 | merge commit on claude/main-before-scrub: Merge claude/u-row-flood 98326d70: the phone fold chevron gets its own column |
| u-scm5: scm stage 5 board half, change requests and the Pushed column | m1mini | 01a0ff3e-f573-77d3-90ad-d9c9f788a42a | needs-input (stack) | 2026-10-03 04:29 | merge commit on claude/main-before-scrub: Merge claude/u-scm5 ed29402c: the requests board follows the hub's change-request routes |
| u-m-reload: /m picks up a new build after a hub deploy | m1mini | 01a0ff62-c3c1-7c11-9743-80e2081a7a55 | needs-input (stack) | 2026-10-03 04:08 | merge commit on claude/main-before-scrub: Merge claude/u-m-reload 005dc4f1: /m reloads on a new build, re-reads after a missed read |
| fabric: director of rooms, hub, cross-room and provisioning | m1mini | 01a0f8d8-e23e-75d4-824a-d36783fe0cb8 | done | 2026-10-03 03:03 | every commit of branch claude/fabric has its patch on claude/main (patch-id) |
| r-launch-stuck: launch-idle, launch-prompt and terminal-menu escalations, roo... | m1mini | 01a0ff59-1d26-739d-adb0-91fa83d9de1c | done | 2026-10-03 03:03 | merge commit on claude/main-before-scrub: Merge claude/r-launch-stuck c65bec80: a launch stuck at a menu reads as stuck (IL) |
| rnd: director of research and design, on claude/rnd | m1mini | 01a0f8bd-af15-7b00-bad3-fc93eaaa8a2a | done | 2026-10-03 03:03 | every commit of branch claude/rnd has its patch on claude/main (patch-id) |
| r-usage-no-stop: record usage on report, exit and done, not only on Stop | m1mini | 01a0ff9d-9003-7772-9c12-a8b318dc48c9 | done | 2026-10-03 02:57 | merge commit on claude/main-before-scrub: Merge claude/r-usage-no-stop 18417530: usage is recorded when a card ends without a Stop ( |
| r-worker-tags: agent launches stamp origin, subagent and dept tags; launcher_... | m1mini | 01a0ff7f-1a6a-75e3-bd89-cbe02b637061 | done | 2026-10-03 02:57 | merge commit on claude/main-before-scrub: Merge claude/r-worker-tags-2 f7642322: launcher_id in one SELECT, a resolved card for the  |
| r-owed-answers: W6, owed answers that survive a context clear | m1mini | 01a0ff30-74ae-7655-a761-34759a356d38 | done | 2026-10-03 02:30 | merge commit on claude/main-before-scrub: Merge claude/r-owed-answers 8d1d31ed: owed answers survive (IJ) |
| u-pin-shows: a pinned card missing from the desktop terminals list | m1mini | 01a0ff89-8cc2-7ce8-8ebc-8ea6cff9a761 | done | 2026-10-03 02:24 | merge commit on claude/main-before-scrub: Merge claude/u-pin-shows 51ff1715: a pinned row is never hidden by a pill |
| r-notify-input-turn-end: idle_prompt gets its own waiting reason, notify gate... | m1mini | 01a0ff7c-b402-7e86-8770-d6d01a61c415 | done | 2026-10-03 02:22 | merge commit on claude/main-before-scrub: Merge claude/r-notify-input-turn-end 0f274fc9: an idle_prompt is not an ask; an asked card |
| f-cr2-caps-audit: f-027 launch caps case keys and PUT unknown fields, f-028 a... | m1mini | 01a0ff6b-6214-7451-947f-2d771e22439b | done | 2026-10-03 02:06 | merge commit on claude/main-before-scrub: Merge claude/f-cr2-caps-audit 79b22ded: launch caps and control audit line fixes (f-027, f |
| r-autocompact: every claude card starts with --autocompact (limit + 10%) | m1mini | 01a0fccb-5b47-7ae5-ad1f-8e563efe294d | done | 2026-10-03 02:06 | report sha's patch is on claude/main (patch-id) |
| f-c-preflight2: Msys2Dir probe and honoured GitUserEmail, small | m1mini | 01a0ff25-21d6-70a7-a7dc-8ff93c5e25fa | done | 2026-10-03 01:50 | merge commit on claude/main-before-scrub: Merge claude/f-c-preflight2 aa63b3f4: Msys2Dir probe and honoured git identity |
| r-hub-remote: the room's stable hub forwarder, cards push to hub only | m1mini | 01a0fee5-a748-7b75-9925-8555b451221e | done | 2026-10-03 01:19 | merge commit on claude/main-before-scrub: Merge claude/r-hub-remote 89847d7d: the room's stable hub forwarder, and cards push to the |
| u-agent-idle: agent-launched cards read idle, not waiting for you | m1mini | 01a0ff2c-a9da-7e92-9d7c-e580724e24b8 | done | 2026-10-03 00:50 | merge commit on claude/main-before-scrub: Merge claude/u-agent-idle 062c8034: agent cards with no open question read idle |
| u-m-bubbles: /m bubbles selectable, copy button, reply quote | m1mini | 01a0ff28-e938-7635-8ed7-4743dec99a96 | done | 2026-10-03 00:50 | merge commit on claude/main-before-scrub: Merge claude/u-m-bubbles f912861a: /m bubbles select, copy and reply |
| u-mobile-pass: phone CSS check of everything new, plus bubbles/chart lows | m1mini | 01a0ff33-870c-756a-b6ab-1f13f280439d | done | 2026-10-03 00:50 | merge commit on claude/main-before-scrub: Merge claude/u-mobile-pass 147e7c70: ledger strip on the phone, bubbles and chart lows |
| u-usage-chart: cumulative tokens chart axis, percent, hover | m1mini | 01a0fef0-55a2-7f0f-bae0-fc171de0b75b | done | 2026-10-03 00:50 | merge commit on claude/main-before-scrub: Merge claude/u-usage-chart 9f54d709: cumulative chart against the 5h limit, even pace, rat |
| r-restart-loopback then r-exit-guard | m1mini | 01a0fce3-9bb4-7a93-a671-038eb5eff294 | done | 2026-10-03 00:42 | merge commit on claude/main-before-scrub: Merge claude/r-restart-loopback bacf8929: restart keeps the daemon's bind, loopback defaul |
| r-scm-clone: scm folder, atrium_git_clone, hub on clones (forge stage 2) | m1mini | 01a0fef0-5d35-76d6-8a2b-27c9768a1f39 | done | 2026-10-03 00:33 | report sha's patch is on claude/main (patch-id) |
| r-opencode-bubbles: per-runner transcript reader, opencode first | m1mini | 01a0f990-3942-7386-9f25-c63278ba3943 | done | 2026-10-03 00:26 | report sha's patch is on claude/main (patch-id) |
| u-ctx-badge: context bar land-the-plane badge | m1mini | 01a0fedd-8eba-70c7-b12b-35db401178b6 | done | 2026-10-02 23:27 | merge commit on claude/main-before-scrub: Merge claude/u-ctx-badge 277fa3c9: land-the-plane LAND badge and row flood |
| u-switch-latency: terminal switch latency, measure then fix | m1mini | 01a0fe98-9b9a-729f-a750-162a2ce94400 | done | 2026-10-02 23:21 | merge commit on claude/main-before-scrub: Merge claude/u-switch-latency 3328490f: keep left terminals alive, switch back is a show |
| u-repos-pass | m1mini | 01a0fe1e-2d3f-758d-8e99-06f308a16952 | done | 2026-10-02 19:44 | merge commit on claude/main-before-scrub: Merge claude/u-repos-pass: repos tab visual pass, tray head one row |
| u-hub-repos | m1mini | 01a0fdf3-be25-7a35-b0a0-80f083f2af2d | done | 2026-10-02 18:52 | merge commit on claude/main-before-scrub: Merge claude/u-hub-repos: the hub's repos tab |
| u-ctx-bar | m1mini | 01a0fdef-8bd5-7eff-95ac-56bfa4de8af7 | done | 2026-10-02 18:49 | merge commit on claude/main-before-scrub: Merge claude/u-ctx-bar: context line on rows and cards |
| autocompact-probe-2 | m1mini | 01a0fdee-095e-7aa2-970f-d13cf2189aef | done | 2026-10-02 18:43 | throwaway/test card: done, no branch, no commits |
| u-clear-keeps-page: a cleared screen goes into scrollback | m1mini | 01a0fdc0-7012-7bfa-b2b0-1d8826cd2473 | done | 2026-10-02 18:05 | merge commit on claude/main-before-scrub: Merge claude/u-clear-keeps-page 31b6a8b0: a lone 3J leaves the screen alone, review OK |
| u-theme-lab: terminal "try a theme" reuses the board's skin lab | m1mini | 01a0fdb1-add0-7017-80ab-7b86ba6f2220 | done | 2026-10-02 17:42 | merge commit on claude/main-before-scrub: Merge claude/u-theme-lab 239e01e4: one lab panel for skins and terminal themes, review OK |
| u-blocker: a card that never starts is a red pinned blocker | m1mini | 01a0fd21-21e8-752c-915f-2bf47d77aaff | done | 2026-10-02 15:07 | merge commit on claude/main-before-scrub: Merge claude/u-blocker 2adda4fb: the blocker lows and the doer exemption, review OK |
| u-joined-click: a click on a joined row opens a dialog | m1mini | 01a0fd16-5366-76ab-b0bd-355526da1c73 | done | 2026-10-02 15:07 | merge commit on claude/main-before-scrub: Merge claude/u-joined-click c7f37de8: the idle-session wording, review OK |
| u-remind-me: "remind me" on growler and bell entries | m1mini | 01a0fcf5-0ea3-77c4-bcad-7bf67837f607 | done | 2026-10-02 14:18 | merge commit on claude/main-before-scrub: Merge claude/u-remind-me 2b7eaa5d: the open bell repaints, review OK |
| u-growler-off: growler off by default, alerts live in the bell | m1mini | 01a0fccd-2559-7079-9f49-8078027ab303 | done | 2026-10-02 13:38 | merge commit on claude/main-before-scrub: Merge claude/u-growler-off 59aefd05: the real-permission case and the deploy line, review  |
| autocompact-probe | m1mini | 01a0fcd0-f075-72fd-858f-962e799ebab0 | done | 2026-10-02 13:32 | throwaway/test card: done, no branch, no commits |
| u-no-toast: no toast for the terminal you are typing in | m1mini | 01a0f93e-38e9-7b17-9728-deb46293a10b | done | 2026-10-01 21:05 | merge commit on claude/main-before-scrub: Merge claude/u-no-toast 79b20421: no growler for the terminal you are on, review OK 9c7117 |
| u-m-card: /m card view, live working indicator, own messages, recap sheet | m1mini | 01a0f4a6-dbd0-79c0-859d-6b8e6b8671f3 | needs-input (stack) | 2026-10-01 07:08 | report sha's patch is on claude/main (patch-id) |
| u-card-urls: card URLs U1 and U2, the board and the phone read readable paths | m1mini | 01a0f40b-97cb-7eff-ace7-73184409dd8b | needs-input (stack) | 2026-09-30 23:25 | report sha's patch is on claude/main (patch-id) |
| review f-paste-open-speed 69563374 | sg3 | 01a11d93-11cf-7d4d-9794-4ac6e404d829 | done | 2026-10-08 22:37 | branch claude/f-paste-open-speed (named in the title) has every patch on claude/main |
| re-review u-terminal-last-read-marker 48ed0177 | sg3 | 01a11d94-1f3b-7816-8f87-3a2e313abbf1 | done | 2026-10-08 22:18 | branch claude/u-terminal-last-read-marker (named in the title) has every patch on claude/main |
| u-terminal-last-read-marker: "new since you were here" line in terminals | sg3 | 01a11d75-200c-745a-9608-91af1f002e29 | done | 2026-10-08 22:12 | branch claude/u-terminal-last-read-marker (named in the title) has every patch on claude/main |
| f-paste-open-speed: PR, Zendesk and Discourse links open in seconds | sg3 | 01a11d74-f9fe-7176-87bf-171d65ded211 | done | 2026-10-08 22:11 | branch claude/f-paste-open-speed (named in the title) has every patch on claude/main |
| review u-terminal-last-read-marker 6ede5398 | sg3 | 01a11d89-5512-7104-8c41-7724b0e0aadf | done | 2026-10-08 22:06 | branch claude/u-terminal-last-read-marker (named in the title) has every patch on claude/main |
| claude | sg3 | 01a11c44-55a1-7e5b-aa1a-0b9eb89a36cd | done | 2026-10-08 16:06 | throwaway/test card: done, no branch, no commits |
| r-worker-budget: per-worker dollar budget and default worker model, both sett... | sg3 | 01a11b64-3a28-73e7-9012-bed701e6d3a2 | done | 2026-10-08 13:03 | branch claude/r-worker-budget (named in the title) has every patch on claude/main |
| r-card-reclaim-on-done (H3+H11): done closes a worker card, merged+deployed r... | sg3 | 01a11b64-0c2d-7081-8f6c-3c2f243fc29f | done | 2026-10-08 12:50 | ops card, branch claude/r-card-reclaim-on-done (named in the title) has every patch on claude/main |
| u-paste-a-link-style: a design pass on the paste-a-link dialog | sg3 | 01a11b64-52e5-797e-88e9-9ed0abb30152 | done | 2026-10-08 12:10 | branch claude/u-paste-a-link-style (named in the title) has every patch on claude/main |
| H10 u-light-theme-dim-text: light theme hides Claude Code's grey text | sg3 | 01a11844-5fbd-747a-b50d-d4e49bef4a4c | done | 2026-10-07 22:45 | branch claude/u-light-theme-dim-text (named in the title) has every patch on claude/main |
| H12 r-worker-test-waste: workers burn turns on slow tests and sleep-and-poll | sg3 | 01a11844-7557-7e7b-8570-1ebd8620f8f4 | done | 2026-10-07 22:45 | branch claude/r-worker-test-waste (named in the title) has every patch on claude/main |
| H16 u-board-tab-disaster: board tab cards collapse, Running empty, headers sq... | sg3 | 01a11848-670e-73b4-94bc-f32bc1ac5b43 | done | 2026-10-07 22:45 | branch claude/u-board-tab-disaster (named in the title) has every patch on claude/main |
| H6 r-card-never-blocked-forever: a card can never stay blocked forever | sg3 | 01a11840-5245-7a43-84ef-a3ba56ccea77 | done | 2026-10-07 22:45 | branch claude/r-card-never-blocked-forever (named in the title) has every patch on claude/main |
| H9 r-throwaway-overwrites-whereami: a test room hangs every permission hook | sg3 | 01a11840-7d51-7877-802a-3a6026ec8c19 | done | 2026-10-07 22:45 | branch claude/r-throwaway-overwrites-whereami (named in the title) has every patch on claude/main |
| ops-sg3-deploy-1a7cb42 | sg3 | 01a10d56-883a-71c6-81c7-a119b35406a2 | done | 2026-10-05 18:35 | ops card, report sha's patch is on claude/main (patch-id) |
| u-read-tab-text-size: A- / A+ and a scrollbar on the .md read tab | sg3 | 01a10c38-4ecb-7708-8645-f77687b266c7 | done | 2026-10-05 13:23 | every commit of branch claude/u-read-tab-text-size has its patch on claude/main (patch-id) |
| f-new-reports-channel-any-room: finish 2 | sg3 | 01a10a53-04ff-71f9-87f5-a6e368385317 | done | 2026-10-05 04:58 | report sha's patch is on claude/main (patch-id) |
| u-new-review-3c1e46d6 | sg3 | 01a10a49-69c4-7a64-b32a-31e2655779b2 | done | 2026-10-05 04:43 | report sha's patch is on claude/main (patch-id) |
| r-new-sec-daemon-wide-bind (2) | sg3 | 01a10a51-47e2-7b30-a0df-c1fc706c26ca | done | 2026-10-05 04:37 | report sha's patch is on claude/main (patch-id) |
| m-new-release-0-0-1: finish 2 | sg3 | 01a10a53-29af-7436-9ec3-5b02e65bea67 | done | 2026-10-05 04:34 | report sha's patch is on claude/main (patch-id) |
| f-new-reports-channel-any-room: finish | sg3 | 01a10a32-5a71-7441-a627-8a7994cf5e43 | done | 2026-10-05 04:27 | merge commit on claude/main-before-scrub: Merge remote-tracking branch 'sg3/claude/f-new-reports-channel-any-room' into claude/main |
| r-new-sec-daemon-wide-bind | sg3 | 01a10a48-5ffa-7695-a5ac-d79863717b5f | done | 2026-10-05 04:27 | report sha's patch is on claude/main (patch-id) |
| r-new-mingit-no-http-backend | sg3 | 01a10a32-922d-7687-8455-cba51de0a9ea | done | 2026-10-05 04:17 | report sha's patch is on claude/main (patch-id) |
| u-new-context-bar-on-rows | sg3 | 01a10a33-d4de-7a20-a16e-428a332acd51 | done | 2026-10-05 04:17 | every commit of branch claude/u-new-context-bar-on-rows has its patch on claude/main (patch-id) |
| m-new-release-0-0-1: finish | sg3 | 01a10a32-7a90-740a-a772-9d9a3c279d97 | done | 2026-10-05 04:13 | report sha's patch is on claude/main (patch-id) |
| u-new-quiet-subagents-not-origin | sg3 | 01a10a10-d00a-7e47-a42b-30035235d59b | done | 2026-10-05 03:39 | report sha's patch is on claude/main (patch-id) |
| f-pr-review-move | sg3 | 01a109f5-b1a6-7dcd-9b48-785fb1c197f4 | done | 2026-10-05 03:21 | report sha's patch is on claude/main (patch-id) |
| u-new-quiet-subagents-not-origin | sg3 | 01a109f8-bfb8-762c-9f54-d729b14c600f | done | 2026-10-05 03:17 | report sha's patch is on claude/main (patch-id) |
| m-new-release-0-0-1 | sg3 | 01a10a05-0995-7450-aa0f-ca3034ac4238 | done | 2026-10-05 03:09 | report sha's patch is on claude/main (patch-id) |
| m-new-deploy-queue | sg3 | 01a109ee-4c0e-70c2-a4e5-899065718566 | done | 2026-10-05 03:04 | ops card, report sha's patch is on claude/main (patch-id) |
| u-new-new-card-late-in-terminals | sg3 | 01a109df-cd10-79a5-85cf-d94f1d8cf25b | done | 2026-10-05 02:50 | report sha's patch is on claude/main (patch-id) |
| f-new-reports-channel-any-room (fix) | sg3 | 01a109ea-f26a-7650-ae90-6b114773110c | done | 2026-10-05 02:39 | merge commit on claude/main-before-scrub: Merge remote-tracking branch 'sg3/claude/f-new-reports-channel-any-room' into claude/main |
| u-pr-finding-states verify | sg3 | 01a109dd-06d6-720e-9790-b0e632c5a468 | done | 2026-10-05 02:35 | report sha's patch is on claude/main (patch-id) |
| r-pr-paste-review | sg3 | 01a109d8-e215-7d97-ae3b-6749a75d572a | done | 2026-10-05 02:29 | report sha's patch is on claude/main (patch-id) |
| f-new-reports-channel-any-room | sg3 | 01a109d4-b107-729f-97cc-dcf7f34a7ebf | done | 2026-10-05 02:24 | merge commit on claude/main-before-scrub: Merge remote-tracking branch 'sg3/claude/f-new-reports-channel-any-room' into claude/main |
| u-new-tray-header-overlap | sg3 | 01a109d5-c855-7f0d-b844-ea22058fa2d5 | done | 2026-10-05 02:23 | report sha's patch is on claude/main (patch-id) |
| u-pr-paste: a pasted PR URL becomes a card on a PR worktree | sg3 | 01a10992-5d18-76a8-aa38-f05e9df79385 | done | 2026-10-05 01:18 | report sha's patch is on claude/main (patch-id) |
| f-place-cpu: PR placement breaks ties on idle CPU | sg3 | 01a10991-c6b1-7de3-8d1b-c77ee20f7b93 | done | 2026-10-05 01:12 | report sha's patch is on claude/main (patch-id) |
| rnd-forge-doc: make the forge design say what was decided and built | sg3 | 01a10991-eb7d-7b64-b2b2-414b4b2314e2 | done | 2026-10-05 01:04 | report sha's patch is on claude/main (patch-id) |
| f-forge-access: forge CLI access check, alert, message and configuration | sg3 | 01a108e9-c678-76e8-b720-435bc0692c5d | done | 2026-10-04 22:11 | report sha's patch is on claude/main (patch-id) |
| rv-scm-1004: review the recogniser rows and the forge answers | sg3 | 01a108ea-c978-7d59-8a64-4fbfc968e328 | done | 2026-10-04 21:58 | report sha's patch is on claude/main (patch-id) |
| r-load-room: recogniser load.ps1 takes a room | sg3 | 01a108ea-8a72-753c-8a6a-1618dd8afc5e | done | 2026-10-04 21:57 | report sha's patch is on claude/main (patch-id) |
| rr-pulls-p3: re-read of pulls P3 af2f5055 after the HOLD | sg3 | 01a108bd-0803-7a45-b084-9380c5d84639 | done | 2026-10-04 21:10 | report sha's patch is on claude/main (patch-id) |
| pulls-p3: wire the pulls view to the real routes | sg3 | 01a0f89b-7a84-7474-be79-baae0c8c4060 | needs-input (stack) | 2026-10-01 22:26 | merge commit on claude/main-before-scrub: Merge claude/pulls-p3 af2f5055: the walk drawer on the real /v1/prs routes |
| u-012: usage tab, too-old line, cache line, polish | sg3 | 01a0eeb6-f223-747f-8d70-5f99bb517c14 | needs-input (stack) | 2026-09-29 21:49 | report sha's patch is on claude/main (patch-id) |
| ops-deploy-hub-1009d: hub-only deploy of ce4d5b74 (cleared divider, hover rest) | sg4-control | 01a1223b-aee6-7676-83ae-8a001069d960 | done | 2026-10-09 20:20 | one-off ops card, its sha ce4d5b74 is on main |
| ops-deploy-hub-5e4f: hub-only deploy of 5e4f365a (peek status fix) | sg4-control | 01a1222f-d65f-7587-94b5-5b9111a77078 | done | 2026-10-09 19:44 | one-off ops card, its sha 5e4f365a is on main |
| ops-deploy-1009c: deploy 23f14ad0 to the hub, claude-sg4, sg3 and m1mini | sg4-control | 01a121d7-9d11-768a-9597-13502eefc327 | done | 2026-10-09 18:09 | one-off ops card, its sha 23f14ad0 is on main |
| ops-deploy-hub-paste: hub-only deploy of 1aa00037 (empty paste fix) | sg4-control | 01a1219f-b6db-7438-b89a-4a6106ac25b0 | done | 2026-10-09 17:06 | one-off ops card, its sha 1aa00037 is on main |
| ops-deploy-sg4-1009: deploy 8f204edb to the claude-sg4 room | sg4-control | 01a12129-2806-72c5-8831-a4d622e956b9 | done | 2026-10-09 14:56 | one-off ops card, its sha 8f204edb is on main |
| f-paste-open-speed: PR, Zendesk and Discourse links open in seconds | sg4-control | 01a11d6e-81ef-73d2-a96f-0cef6003e657 | done | 2026-10-08 21:37 | every commit of branch claude/f-paste-open-speed has its patch on claude/main (patch-id) |
| review r-agent-loop-tools 78393116 | sg4-control | 01a11d65-e352-7777-af18-119c90a111ec | done | 2026-10-08 21:37 | branch claude/r-agent-loop-tools (named in the title) has every patch on claude/main |
| review f-room-spec D follow-up a0ace162 | sg4-control | 01a11d39-7b3a-7f1e-b3ca-b8282a482667 | done | 2026-10-08 21:23 | branch claude/f-room-spec (named in the title) has every patch on claude/main |
| f-room-bringup: remote room bring-up, work drive + agent pack, proven from em... | sg4-control | 01a11c51-ca46-7aad-9093-be724587596f | done | 2026-10-08 21:22 | every commit of branch claude/f-room-spec has its patch on claude/main (patch-id) |
| f-room-spec-d3: path.Clean the other runners' pack folders | sg4-control | 01a11d53-13d4-7794-b88a-2d1eff66ab6b | done | 2026-10-08 21:22 | every commit of branch claude/f-room-spec-d3 has its patch on claude/main (patch-id) |
| r-agent-loop-tools: atrium_done and atrium_blocked | sg4-control | 01a11d42-ee5b-7181-807c-4f3f66e35d13 | done | 2026-10-08 21:15 | every commit of branch claude/r-agent-loop-tools has its patch on claude/main (patch-id) |
| review f-room-spec phase D 06e40cac | sg4-control | 01a11d23-1f7d-7e1f-bf6b-1f93cb2c5e70 | done | 2026-10-08 20:28 | branch claude/f-room-spec (named in the title) has every patch on claude/main |
| review f-room-spec phase C follow-up a3a8dba0 | sg4-control | 01a11d16-c112-74df-bf6e-8cc8c5aedee7 | done | 2026-10-08 19:59 | branch claude/f-room-spec (named in the title) has every patch on claude/main |
| review f-room-spec phase C 491dd4bd | sg4-control | 01a11cf7-6e48-79aa-816a-d827aebcd13a | done | 2026-10-08 19:49 | branch claude/f-room-spec (named in the title) has every patch on claude/main |
| review f-room-spec phase A a5dfeadd | sg4-control | 01a11cce-140e-7581-80eb-99be17f9d139 | done | 2026-10-08 18:52 | branch claude/f-room-spec (named in the title) has every patch on claude/main |
| review f-room-spec-hub b955db6f | sg4-control | 01a11cb2-515b-712c-91b3-f23326e2397d | done | 2026-10-08 18:23 | branch claude/f-room-spec-hub (named in the title) has every patch on claude/main |
| review f-room-bringup af914e9d | sg4-control | 01a11c81-e422-74e5-b886-4ec8e042103c | done | 2026-10-08 17:17 | every commit of branch claude/f-room-spec has its patch on claude/main (patch-id) |
| f-room-work-drive: remote room setup notes, agent files on the work drive | sg4-control | 01a11c46-d6a2-7b6a-9040-2178b35623cb | done | 2026-10-08 16:11 | report sha's patch is on claude/main (patch-id) |
| ops-land4: land f-pr-launch-fast, deploy hub and all rooms | sg4-control | 01a11c28-65b6-787f-9cb4-cdb984f7a481 | done | 2026-10-08 16:02 | ops card, every commit of branch claude/ops-land2 has its patch on claude/main (patch-id) |
| f-pr-launch-fast: paste a PR link to running card in seconds, like gwt | sg4-control | 01a11c04-d016-7cce-9d08-36fe15858b68 | done | 2026-10-08 15:33 | every commit of branch claude/f-pr-launch-fast has its patch on claude/main (patch-id) |
| ops-land3: land graceful restart + dropdowns + paste fix, deploy hub and all ... | sg4-control | 01a11c01-a693-7e49-9041-2a9c113770c6 | done | 2026-10-08 15:10 | ops card, every commit of branch claude/ops-land2 has its patch on claude/main (patch-id) |
| ops-land2: finish landing graceful restart + dropdowns, deploy hub, sg3, clau... | sg4-control | 01a11bef-56c5-7851-8f7c-e612cdcfb432 | done | 2026-10-08 14:53 | ops card, branch claude/ops-land2 (named in the title) has every patch on claude/main |
| ops-merge-1005: merge r-context-nudge-handoff and deploy the rooms | sg4-control | 01a10c6e-4c0e-758a-adf5-b92ed73e7958 | done | 2026-10-05 19:55 | ops card, branch claude/r-context-nudge-handoff (named in the title) has every patch on claude/main |
| u-alt-scroll throwaway | sg4-control | 01a0ff96-a012-764b-a05e-c369479f429d | done | 2026-10-03 02:53 | throwaway/test card: done, no branch, no commits |
| opencode runner test | sg4-control | 01a0f958-ec91-7f14-91db-785d42ad268f | done | 2026-10-03 02:36 | throwaway/test card: done, no branch, no commits |
| pr-review-story: how a human drives an LLM to a quality code review in atrium | sg4-control | 01a0f959-a97a-7e30-91a4-6403afb6c100 | done | 2026-10-01 23:38 | every commit of branch claude/pr-review-story has its patch on claude/main (patch-id) |

## 2. PROBABLY REMOVE (147)

Each row says the one fact that is missing.

| Card title | Room | Card id | Status | Last activity (UTC) | Evidence |
| --- | --- | --- | --- | --- | --- |
| slop-worker: sdk-golang e2ee-tls review drafts + applies | claude-sg4 | 01a12245-c6c9-74c3-aff7-979d7acbcce6 | done | 2026-10-09 20:41 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| review u-cycle-typing-queues c36d975d | claude-sg4 | 01a11d92-b383-7f01-b171-5fb92c41d44a | done | 2026-10-08 22:23 | done atrium review card (TSHA-NOT-MERGED:c36d975d); missing: whether the reviewed branch landed (not visible here) |
| pr-ziti-console-967: review openziti/ziti-console#967 | claude-sg4 | 01a11631-85b2-73d6-a4f0-97a2258f5b6f | done | 2026-10-08 17:04 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| ops-land-r-worker-budget: merge 735c531f into claude/main, test on sg4, push ... | claude-sg4 | 01a11b8e-ebd1-7b3b-9fac-3f9b45acd680 | done | 2026-10-08 13:07 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| ops-land-r-cycle-before-runner-compacts: merge b98cecbb into claude/main, pus... | claude-sg4 | 01a11b85-78e3-7a38-a1ab-0f91da0ae891 | done | 2026-10-08 12:42 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| ops-land-paste-a-link-style: merge af092677 into claude/main, push hub, deplo... | claude-sg4 | 01a11b6d-eb4f-7abe-adcb-f30ea321fa98 | done | 2026-10-08 12:17 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| ops-land: mirror fix + comms fix, hub deploy | claude-sg4 | 01a117ea-9e4f-7981-b8b0-1387d2e7a9f7 | done | 2026-10-08 03:40 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| review-24574d06: ubuntu CI fix | claude-sg4 | 01a117d0-9f9f-70f2-ada5-e52fd35fb3dd | needs-input (stack) | 2026-10-07 23:25 | review-panel card waiting on the stack since 2026-10-07; missing: whether the PR it reviews is merged or closed (no GitHub access) |
| ops-merge-all: land every unmerged claude/* branch | claude-sg4 | 01a1180f-8a09-7372-935a-e8b051135d11 | needs-input (stack) | 2026-10-07 22:35 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| pr-967 panel: completeness critic | claude-sg4 | 01a1163b-3bab-74c5-9549-fce70fc8f10d | done | 2026-10-07 12:02 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 verify s5: any-mount-path | claude-sg4 | 01a1163a-3179-70b4-b7e2-54324a5c11c7 | done | 2026-10-07 11:59 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 verify s2: isTest dropped | claude-sg4 | 01a11639-0c4f-7321-8a35-4dce1cc432ff | done | 2026-10-07 11:58 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 verify s3: 401 login loop | claude-sg4 | 01a11639-216f-75fe-8c2a-bbe5303559b4 | done | 2026-10-07 11:58 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 verify s4: permissions not loaded | claude-sg4 | 01a11639-32a8-73bb-8c30-43b147adaa6e | done | 2026-10-07 11:58 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 panel: functional-tester | claude-sg4 | 01a11632-c9a1-79c7-90a7-5c46c64c2ead | done | 2026-10-07 11:57 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 panel: web-security (client) | claude-sg4 | 01a11632-a360-766b-a2aa-87eab8ee84e7 | done | 2026-10-07 11:57 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 verify s1: Dockerfile legacy-api.js | claude-sg4 | 01a11638-fd54-7d9d-88f6-9057924a13b3 | done | 2026-10-07 11:57 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 panel: codebase-steward | claude-sg4 | 01a11632-b719-77a8-b135-83d237197765 | done | 2026-10-07 11:56 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-967 panel: web-security (server) | claude-sg4 | 01a11632-8d14-7a26-a476-0c8560b13adc | done | 2026-10-07 11:55 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-ziti-4544: review openziti/ziti#4544 | claude-sg4 | 01a1133c-905f-796d-a496-4b354bb7cb8c | done | 2026-10-07 10:46 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-4544 panel: codebase-steward | claude-sg4 | 01a1133d-8774-7e1b-b20b-7eed81604bdf | needs-input (stack) | 2026-10-07 00:05 | review-panel card waiting on the stack since 2026-10-07; missing: whether the PR it reviews is merged or closed (no GitHub access) |
| pr-4544 panel: coverage critic | claude-sg4 | 01a11342-8551-75f8-b2ea-0df719e19a86 | done | 2026-10-06 22:12 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-4544 panel: functional-tester | claude-sg4 | 01a1133d-9b05-784c-8c9c-f346d9f99346 | done | 2026-10-06 22:07 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-4544 panel: go-security-reviewer | claude-sg4 | 01a1133d-748a-776b-a4bf-f12350b5fcb9 | done | 2026-10-06 22:07 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| clintify mine 2/4 | claude-sg4 | 01a1114c-6a84-75e8-8664-e82ac30d6b6b | done | 2026-10-06 13:04 | done, no running session, 2026-10-06, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| clintify mine 1/4 | claude-sg4 | 01a1114c-5a3d-72b2-a84c-e509bea31ce2 | done | 2026-10-06 13:03 | done, no running session, 2026-10-06, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| pr-1443 panel: functional-tester | claude-sg4 | 01a108b5-5801-71f5-abc9-16b66199490b | done | 2026-10-05 23:37 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-ziti-tunnel-sdk-c-1443: review openziti/ziti-tunnel-sdk-c#1443 | claude-sg4 | 01a108b3-50de-7844-9d7b-f70def9ad3fd | done | 2026-10-05 20:05 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-tlsuv-383: review openziti/tlsuv#383 | claude-sg4 | 01a10c60-8a37-7f17-9d04-b46fa020ac10 | done | 2026-10-05 17:47 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-383 panel: codebase-steward | claude-sg4 | 01a10cac-00a2-74a9-90a8-dab47da42e30 | done | 2026-10-05 15:28 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-383 panel: functional-tester | claude-sg4 | 01a10cac-11a1-7d5f-981f-a95ab894dc04 | done | 2026-10-05 15:28 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-383 panel: c-systems-reviewer | claude-sg4 | 01a10cab-e9ec-74d7-a36b-ca9c983c1b64 | done | 2026-10-05 15:27 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| CI failed on main: Merge claude/r-scm-recognisers: GitHub and Bitbucket recog... | claude-sg4 | 01a108df-10ca-7c10-9d1d-caac41cbc501 | backlog | 2026-10-04 21:43 | backlog CI-failure note for an old main commit (2026-10-04); missing: whether main CI is green now |
| pr-1443 panel: c-systems-reviewer | claude-sg4 | 01a108b5-403e-702f-8fae-be1f85d9d7b8 | done | 2026-10-04 20:59 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| ZAC config builder: codebase fit and CSS review | claude-sg4 | 01a1089b-5f7a-7ee7-aaaa-64ee88319bc3 | done | 2026-10-04 20:30 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| claude/main:atrium | claude-sg4 | 01a0ff57-dc7c-7730-9581-f5b5648a81a7 | done | 2026-10-03 01:19 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| CI failed on main: terminals: group headings read shown/total, and the agents... | claude-sg4 | 01a0fe86-41b3-7ae1-a214-c32a5af405d0 | backlog | 2026-10-02 21:30 | backlog CI-failure note for an old main commit (2026-10-02); missing: whether main CI is green now |
| review-B: clock-skew fixes, tests and adversarial scenarios | claude-sg4 | 01a0fd30-1d09-7ad3-a7b9-3ed308ce2ef7 | done | 2026-10-02 15:24 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| review-A: clock-skew fixes, correctness and safety | claude-sg4 | 01a0fd2f-f383-729d-83e0-3aac85d65ff7 | done | 2026-10-02 15:22 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| CI failed on main: Merge branch 'claude/fix-joined' into HEAD | claude-sg4 | 01a0f7f1-cfb0-7594-9197-eacee3b06eb6 | backlog | 2026-10-01 14:50 | backlog CI-failure note for an old main commit (2026-10-01); missing: whether main CI is green now |
| CI failed on main: board sort pill, infer repo from path with override, scope... | claude-sg4 | 01a0f66d-a3fc-7ce3-9d69-f1e821e666a8 | backlog | 2026-10-01 07:46 | backlog CI-failure note for an old main commit (2026-10-01); missing: whether main CI is green now |
| merge: director of release, owns claude/main and deploys | claude-sg4 | 01a0ee2c-06ee-7b5c-9e5d-0c771dce2551 | done | 2026-09-30 10:55 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| pr-ziti-4397 | claude-sg4 | 01a0ee0b-a8cf-79e0-8bfe-ba4720cb6e2e | needs-input (stack) | 2026-09-29 23:37 | review-panel card waiting on the stack since 2026-09-29; missing: whether the PR it reviews is merged or closed (no GitHub access) |
| tlsuv sch-credentials: prepare commit(s) for review (no commit) | claude-sg4 | 01a0e8f0-ecda-7ac2-a6aa-08789c51d8d6 | needs-input (stack) | 2026-09-29 21:06 | review-panel card waiting on the stack since 2026-09-29; missing: whether the PR it reviews is merged or closed (no GitHub access) |
| apple-secure-transport-engine | claude-sg4 | 01a0ed8d-34c0-7e95-b616-a5e048ad668b | done | 2026-09-29 18:05 | done openziti-repo card from 2026-09-29, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| tlsuv/fix-ci | claude-sg4 | 01a0e9aa-92ff-7596-a5fe-a5be358f0762 | done | 2026-09-29 14:43 | done openziti-repo card from 2026-09-29, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| zrok2 on OpenZiti 2 (docker cluster) | claude-sg4 | 01a0d9f3-2328-7cc5-ac52-a9a78fba007c | done | 2026-09-29 14:28 | done openziti-repo card from 2026-09-29, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| saorch: merger, owns claude/main, workers report here | claude-sg4 | 01a0e80b-346a-7356-85af-368af744ee40 | done | 2026-09-29 03:04 | done, no running session, 2026-09-29, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| http-support | claude-sg4 | 01a0e81f-1a6c-7d70-823d-6656c0d6211b | done | 2026-09-28 16:25 | done, no running session, 2026-09-28, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| tlsuv GHSA: verify_cert_ca proof of exploit (red -> green) | claude-sg4 | 01a0d9d7-1acb-70af-b839-372a6da2f843 | done | 2026-09-28 13:47 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| atrium/claude/lean-workers | claude-sg4 | 01a0e833-0069-7661-b85f-4f70187c45e5 | done | 2026-09-28 13:36 | done, no running session, 2026-09-28, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| dotfiles/claude/usage-per-prompt | claude-sg4 | 01a0e7da-3e1c-7145-af05-f3df051d7d38 | done | 2026-09-28 11:50 | done, no running session, 2026-09-28, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| random-help-46400 | claude-sg4 | 01a081bc-e5c3-7e97-a807-61d9649c390b | done | 2026-09-27 13:12 | done, no running session, 2026-09-27, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| fix-ipc-pipe-dacl-and-dump-path | claude-sg4 | 01a0cad2-d5bb-77d8-a7b6-026eefdf6bf3 | done | 2026-09-25 13:52 | done openziti-repo card from 2026-09-25, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| advisory-ccq6-vc5v-4ghh-pr3 | claude-sg4 | 01a0d118-0d55-7f2a-a4af-5f556dc8b55a | done | 2026-09-24 02:06 | done openziti-repo card from 2026-09-24, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| log-level-tests | claude-sg4 | 01a0ca3c-d9f7-7578-8cac-007b3ea8aa79 | done | 2026-09-22 18:14 | done openziti-repo card from 2026-09-22, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| provision sgg: install codex/gemini/ollama + verified start-room script | claude-sg4 | 01a0c47c-8758-7391-b29d-c59979594a78 | done | 2026-09-21 15:19 | done, no running session, 2026-09-21, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| backport.v1.8.x.fix.1018.stateclosed-after-fin | claude-sg4 | 01a0b053-a429-7b82-9b5e-d6dbe996860f | done | 2026-09-17 17:27 | done openziti-repo card from 2026-09-17, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| registry-enabled-consults-root.502 | claude-sg4 | 01a0b053-ead5-79eb-bd65-14aa336beccb | done | 2026-09-17 17:20 | done openziti-repo card from 2026-09-17, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| add-cni-draft:docusaurus-shared | claude-sg4 | 01a08b7d-806b-7c6d-8f8c-f8ded16c8254 | done | 2026-09-14 11:45 | done, no running session, 2026-09-14, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| add-rekey-support:vercel-replacement | claude-sg4 | 01a06ccd-7901-7368-a743-1e255d2cf2bf | done | 2026-09-06 02:51 | done openziti-repo card from 2026-09-06, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/fix-arg-parsing | claude-sg4 | 01a0744d-5208-78fb-bc2a-9a3f08ae10d6 | done | 2026-09-06 01:20 | done openziti-repo card from 2026-09-06, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/fix-arg-parsing | claude-sg4 | 01a0689c-db16-7423-940f-d7ee9578dd3b | done | 2026-09-05 01:14 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/ext-signer-bug | claude-sg4 | 01a06f20-1a31-7290-b2d3-3160174781e7 | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/integrate-with-docker-sb | claude-sg4 | 01a06f20-26a2-7beb-9f68-29836f42f7bf | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/posture-bug | claude-sg4 | 01a06f20-2351-7d7d-9d28-9c1265c43272 | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/posture-bug-backport-2.0 | claude-sg4 | 01a06f20-1e89-7030-a5db-0843e8e3d7e6 | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/spike-merging-openziti-installations | claude-sg4 | 01a06f20-0fee-7267-a5d9-f2dfc4f2b92a | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/tunneled-acme | claude-sg4 | 01a06856-5cbc-7191-92fa-cbf862d94528 | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti-tunnel-sdk-c/approve-hosted-services | claude-sg4 | 01a06860-61a6-76a4-b3c7-ce6ada21754f | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti-tunnel-sdk-c/cert-renewal-tests | claude-sg4 | 01a0689e-8147-7b48-a9f6-f1c2e644e8c5 | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti-tunnel-sdk-c/nightly-failures | claude-sg4 | 01a0689e-5b85-71fe-8341-7377e0e4fd49 | done | 2026-09-05 01:12 | done openziti-repo card from 2026-09-05, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti-doc/idp-auth | claude-sg4 | 01a0689c-a3ce-79d5-bf98-78909bac73f3 | done | 2026-09-04 19:28 | done openziti-repo card from 2026-09-04, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti-doc/doc-enroll-to-token-url-and-why | claude-sg4 | 01a0689c-83c3-7960-95e4-7550119d22f1 | done | 2026-09-04 19:27 | done openziti-repo card from 2026-09-04, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| fix/streamable-session-leak:mcp-gateway | claude-sg4 | 01a06ccd-d34e-7a68-8867-9d13540f05d3 | done | 2026-09-04 18:35 | done openziti-repo card from 2026-09-04, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| help-prune-worktrees | claude-sg4 | 01a067dc-a12f-70c3-be85-0ae5fcf08bf1 | done | 2026-09-04 18:35 | done openziti-repo card from 2026-09-04, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| ziti/discourse-6036 | claude-sg4 | 01a06c46-3ec0-74bd-9590-36a7ac852d30 | done | 2026-09-04 18:35 | done openziti-repo card from 2026-09-04, no running session; missing: whether its upstream branch/PR merged (no GitHub access) |
| u-peek-launch-shorter | m1mini | 01a11281-54e4-71a1-bf44-74d6279be57c | done | 2026-10-06 18:37 | done, no running session, 2026-10-06, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| usage-week-compare | m1mini | 01a11237-0d04-7947-8cb5-766c40dbd29b | done | 2026-10-06 17:20 | done, no running session, 2026-10-06, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| ops-m1mini-deploy-703221b | m1mini | 01a1115b-30d4-73ff-867c-5943b7ded0bd | done | 2026-10-06 13:18 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| ops-m1mini-git-push: give m1mini cards atrium_git_push, push three finished b... | m1mini | 01a10cce-759a-7e5f-9df6-3b6a4559f5fd | done | 2026-10-05 16:05 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| f-new-git-url: hub-forge stage 4, atrium_git_url lookup | m1mini | 01a0ff68-d82b-7d69-94c7-fb19d2555045 | done | 2026-10-03 03:03 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| r-ctx-backstop: L0, claude's compaction must sit above atrium's clear point | m1mini | 01a0ff6a-4b76-78ca-bda2-a858be07e6a3 | done | 2026-10-03 03:03 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| u-alt-scroll: wheel/PgUp to alt-screen apps (opencode) | m1mini | 01a0ff95-f3f4-780e-867b-0ba9caa4363b | done | 2026-10-03 03:03 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| ui: director of the board, on claude/ui | m1mini | 01a0f8bd-c580-7a54-8551-ba3dc14a4a0d | done | 2026-10-03 03:03 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| u-nest: workers nest under the director that launched them | m1mini | 01a0ff83-dc28-7e8e-9eb9-06d9833469a1 | done | 2026-10-03 03:03 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| r-keepalive-cold-clear | m1mini | 01a0ffad-5ffd-704a-9cf9-2f98fe0a3d56 | done | 2026-10-03 03:01 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| f-new-change-requests: hub-forge stage 5, change requests between rooms (migr... | m1mini | 01a0ff69-18a0-71f0-a692-e996256909db | done | 2026-10-03 02:53 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| f-new-hub-fetch-pass: hub-forge stage 3, fetch passed through the hub to the ... | m1mini | 01a0ff24-940b-7222-bdda-44b49d8128d7 | done | 2026-10-03 01:37 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| u-term-scroll: phone terminals tab, list cannot scroll to the last row | m1mini | 01a0ff5b-99c1-7b90-ba75-d8b93b7ea59f | done | 2026-10-03 01:29 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| f-new-cross-room-wake: atrium_say wake=true works across rooms | m1mini | 01a0ff24-5d4f-7b52-8062-8cff6d0b9ab3 | done | 2026-10-03 00:48 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| f-new-notify-held: hub-held "desktop notifications held" boolean, writable fr... | m1mini | 01a0ff36-c869-71f4-9f7d-3e3392524cc5 | done | 2026-10-03 00:43 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| f-hub-timing: opt-in hub timing log for a terminal ws attach | m1mini | 01a0fe9c-6e62-7772-a1c0-2e6b8dd1a370 | done | 2026-10-02 21:58 | done, no running session, 2026-10-02, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| u-repos-pass2 | m1mini | 01a0fe29-d069-7222-964a-b23948051c80 | done | 2026-10-02 20:23 | done, no running session, 2026-10-02, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| pr-openwiki-zrok-2: review of claude/zrok-provider 4e27c55 | m1mini | 01a0fc9f-936e-7799-af5a-49dbbace0538 | done | 2026-10-02 13:28 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr-openwiki-zrok: review of claude/zrok-provider de2d5fe | m1mini | 01a0fc86-d05e-7cdc-bd70-5a0ed3be5af4 | done | 2026-10-02 12:23 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| f-016 | m1mini | 01a0efc1-123f-7af4-aa36-6b3992f39ed9 | done | 2026-09-30 00:42 | done, no running session, 2026-09-30, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| openziti/ziti-console#960 Fix cutoff close button for the help context side p... | sg3 | 01a11c02-c3fc-7ed9-b550-fa9da38df32d | done | 2026-10-08 16:40 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| ops-sg3-deploy-703221b | sg3 | 01a1115b-4387-7a5b-a192-dd51fa870bba | done | 2026-10-06 13:17 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| ops-sg3-deploy-fdfa4d8 | sg3 | 01a10d8e-9b80-7d5b-a59d-9079529d7b70 | done | 2026-10-05 19:55 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| sg3-profile: is Defender eating sg3 | sg3 | 01a0f474-13d2-708d-bedc-8326ab8be8cd | done | 2026-10-01 00:41 | done, no running session, 2026-10-01, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| u-028: images in the composer | sg3 | 01a0f026-cc76-795c-9bfb-5d2efe74646d | done | 2026-09-30 02:34 | done, no running session, 2026-09-30, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| u-015: UI test registry, CI headless, static poll check (build steps 0 and 1) | sg3 | 01a0efc0-09e7-75f5-8356-23c4f99dbb49 | done | 2026-09-30 00:48 | done, no running session, 2026-09-30, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| ops-deploy-68cda4c7: hub, claude-sg4, sg3, m1mini to claude/main 68cda4c7 | sg4-control | 01a11ce7-26e8-7c27-a160-d235b16ce7c3 | done | 2026-10-08 20:33 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| ops-deploy-sg4: deploy claude/main to the hub and the claude-sg4 room | sg4-control | 01a11c89-5c34-7782-8cc4-fc05010e55a2 | done | 2026-10-08 19:04 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| PR 1166 stringent C review: what ekoby will push back on | sg4-control | 01a11740-6184-73c3-a7c5-4bd2e898a7e7 | needs-input (stack) | 2026-10-07 19:06 | review-panel card waiting on the stack since 2026-10-07; missing: whether the PR it reviews is merged or closed (no GitHub access) |
| ziti-sdk-c: parallel Azure e2ee matrix + 1.18.7.1 compat cells | sg4-control | 01a112bb-ee30-7894-a21b-b490880d6f1c | done | 2026-10-06 19:53 | done, no running session, 2026-10-06, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| mx-v0450 mac: SDK on tlsuv v0.45.0, m1mini openssl + applesec | sg4-control | 01a11156-a500-7935-842e-194b2774b76e | done | 2026-10-06 18:15 | done, no running session, 2026-10-06, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| mx-v0450 linux: SDK on tlsuv v0.45.0, WSL + clintfips OpenSSL FIPS 3.1.2 | sg4-control | 01a11156-6eb2-7af9-82cd-45df28961bd5 | done | 2026-10-06 13:32 | done, no running session, 2026-10-06, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| pr382 panel: coverage critic | sg4-control | 01a10ee5-688f-7373-9c5e-22417d7615ea | done | 2026-10-06 01:50 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr382 panel: c-systems-reviewer | sg4-control | 01a10ede-d3fd-7c23-a011-b1c08d362052 | done | 2026-10-06 01:47 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr382 panel: functional-tester | sg4-control | 01a10ede-f072-7e9b-9be7-fba33b89b1cb | done | 2026-10-06 01:47 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| pr382 panel: codebase-steward | sg4-control | 01a10ede-e228-7b1c-b1f0-a78f68ae390a | done | 2026-10-06 01:45 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| e2ee gaps: api surface and mixed terminators | sg4-control | 01a10e0a-1775-72ba-90a0-64d5508821f5 | done | 2026-10-05 21:58 | done, no running session, 2026-10-05, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| e2ee gaps: tls interop with the C engine | sg4-control | 01a10e0a-0060-7e87-8b9b-5ff34e4dc9fb | done | 2026-10-05 21:56 | done, no running session, 2026-10-05, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| f-review-memory-trial: make reviewers self-improve, trial setup | sg4-control | 01a10e0f-b50a-78f5-b9d5-458f12fb5dd1 | done | 2026-10-05 21:55 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| e2ee gaps: trust anchors and peer identity | sg4-control | 01a10e08-9ee3-7d94-891b-0998be46b3e8 | done | 2026-10-05 21:53 | done, no running session, 2026-10-05, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| e2ee gaps: stream-end and framing integrity | sg4-control | 01a10e08-8063-75fa-a1dd-cea6aeb4e7e2 | done | 2026-10-05 21:52 | done, no running session, 2026-10-05, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| e2ee-panel: completeness critic | sg4-control | 01a10e00-e9b3-749f-90d0-f858e67708eb | done | 2026-10-05 21:48 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| e2ee-panel: verify FIPS auto-switch breaks interop | sg4-control | 01a10e00-d067-7ed8-bab4-2f4d76ff9ee3 | done | 2026-10-05 21:42 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| e2ee-panel: verify listener caches e2ee method | sg4-control | 01a10e00-b42a-79be-a9ec-613e68e27157 | done | 2026-10-05 21:39 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| e2ee-panel: nonfunctional-tester on sdk-golang e2ee-tls | sg4-control | 01a10dfb-658c-7464-bf18-3cb053694262 | done | 2026-10-05 21:37 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| e2ee-panel: functional-tester on sdk-golang e2ee-tls | sg4-control | 01a10df8-b58d-7c16-8f89-0c8e1e0eca18 | done | 2026-10-05 21:36 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel2: functional on ziti-sdk-c prod diff (introduced only) | sg4-control | 01a10dec-9c86-7896-93ca-02774e31a71c | done | 2026-10-05 21:33 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| e2ee-panel: go-security-reviewer on sdk-golang e2ee-tls | sg4-control | 01a10df4-210c-73ed-922d-d79ede068978 | done | 2026-10-05 21:32 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| e2ee-panel: codebase-steward on sdk-golang e2ee-tls | sg4-control | 01a10df4-148c-7461-b8a7-37731f20fcf1 | done | 2026-10-05 21:29 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel2: c-systems on ziti-sdk-c prod diff (introduced only) | sg4-control | 01a10dec-7862-7d6b-afcb-f82a4843900f | done | 2026-10-05 21:25 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel verify: failing run exits 0 | sg4-control | 01a10dd2-e38b-7389-bc55-12ad76bb68ae | done | 2026-10-05 21:12 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel verify: matrix current ignores pass | sg4-control | 01a10dd1-71ab-75a8-a699-4626b6aa0acb | done | 2026-10-05 21:12 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel critic: e2ee scripts completeness | sg4-control | 01a10dd3-434d-7d46-afaf-cf8967f612e3 | done | 2026-10-05 20:59 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel verify: unit verdict ignores exit code | sg4-control | 01a10dd1-55e0-72c7-9363-028bbcb90ef6 | done | 2026-10-05 20:47 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel: codebase-steward on e2ee FIPS scripts | sg4-control | 01a10dcc-6b4c-76dc-88f9-39e0cea393f0 | done | 2026-10-05 20:46 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| panel: functional-tester on e2ee FIPS scripts | sg4-control | 01a10dcc-c856-7a72-8060-c30d5e9f0c49 | done | 2026-10-05 20:46 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| panel: windows-enterprise-veteran on e2ee FIPS scripts | sg4-control | 01a10dcc-9ca2-7809-b956-779e48dbec27 | done | 2026-10-05 20:44 | done review-panel card for an upstream PR; missing: whether that PR is merged or closed (no GitHub access) |
| panel: functional-tester on ziti-sdk-c win32crypto-e2e | sg4-control | 01a10d93-25c1-7d20-8853-c5aeb75535c5 | done | 2026-10-05 19:42 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel: codebase-steward on ziti-sdk-c win32crypto-e2e | sg4-control | 01a10d90-c2f6-7f13-ab22-0b061d7a1ef9 | done | 2026-10-05 19:38 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| panel: c-systems-reviewer on ziti-sdk-c win32crypto-e2e | sg4-control | 01a10d90-d334-7edb-9611-0fde9c6d3ddc | done | 2026-10-05 19:38 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| r4-review: tlsuv win32crypto TLS 1.3 + SDK e2ee-tls (cold, independent) | sg4-control | 01a10d15-aafe-7e40-90e7-2a8956d70209 | done | 2026-10-05 17:27 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| ops-merge-night: merge every unmerged night branch, deploy | sg4-control | 01a10bf5-76d8-7a52-a672-de55a80ca933 | done | 2026-10-05 13:39 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| ops: orchestrator's land/build/deploy hands | sg4-control | 01a0ff87-6696-7b50-9edf-ea317611da87 | done | 2026-10-03 03:03 | done ops/deploy card, no sha of it found on main; missing: which sha it shipped (not in the title or not in this repo) |
| opencode: clint's "Resume scan for atrium repository" session | sg4-control | 01a0f984-03fc-7e22-98c3-c6d4e1beadf2 | done | 2026-10-03 02:16 | done, no running session, 2026-10-03, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| interview: hub forge (every repo on the hub) | sg4-control | 01a0fdcf-a3ef-72ed-a49c-34b8c1d6a1b0 | done | 2026-10-02 18:33 | done, no running session, 2026-10-02, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| openwiki: zrok for OpenWiki, on clint's fork | sg4-control | 01a0fc78-ec76-73eb-af48-1bef61ee63d1 | done | 2026-10-02 15:31 | done, no running session, 2026-10-02, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| r3-review: tlsuv win32crypto N1/T1/T2 + KeyUpdate finding + PR #382 comparison | sg4-control | 01a0fca1-2551-727e-9b59-69131dc30600 | done | 2026-10-02 14:40 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |
| branch-triage: where clint's 17 unmerged branches stand | sg4-control | 01a0f464-e558-7adb-b555-69ed16185f8f | done | 2026-10-01 00:17 | done, no running session, 2026-10-01, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| usage-2d: token usage analysis, 2026-09-29 and 09-30 | sg4-control | 01a0f400-a828-7d17-81b3-bb4e70f1c556 | done | 2026-09-30 22:28 | done, no running session, 2026-09-30, no landing evidence found; missing: whether its branch landed on that machine (branch not in this repo) |
| rnd: cold independent review of tlsuv win32crypto patches + SDK tests :276/:359 | sg4-control | 01a0f333-8a3d-7178-9803-171d673e6d6e | done | 2026-09-30 19:54 | done atrium review card (); missing: whether the reviewed branch landed (not visible here) |

## 3. KEEP (36)

| Card title | Room | Card id | Status | Last activity (UTC) | Evidence |
| --- | --- | --- | --- | --- | --- |
| main:dotfiles | claude-sg4 | 01a068e1-4602-79ed-89ba-317f50475b50 | needs-input (stack) | 2026-10-10 03:47 | pinned |
| ui-for-config | claude-sg4 | 01a0fd06-d7e5-7848-afe6-2c511103f517 | needs-input (stack) | 2026-10-10 03:31 | session active in the last day (needs-input) |
| discourse-6101 | claude-sg4 | 01a0c906-e53d-7b3b-8ae7-b25c5ded1b2a | needs-input (stack) | 2026-10-09 18:08 | session active in the last day (needs-input) |
| fix-fips | claude-sg4 | 01a1112e-dfc8-730c-8155-51d643d3692a | needs-input (stack) | 2026-10-09 18:08 | session active in the last day (needs-input) |
| hot-loop | claude-sg4 | 01a0fcae-cc75-7771-b074-03e75f0bfdf2 | needs-input (stack) | 2026-10-09 18:08 | session active in the last day (needs-input) |
| nf-self-hosted | claude-sg4 | 01a0d8f6-89c9-7b63-97be-5d731c8ccc87 | needs-input (stack) | 2026-10-09 18:08 | session active in the last day (needs-input) |
| dotagents: refresh stale atrium AGENTS.md files | claude-sg4 | 01a11712-4f0a-7998-bc51-ed1f22e503ad | needs-input (stack) | 2026-10-09 14:40 | session active in the last day (needs-input) |
| main:dotagents | claude-sg4 | 01a0fd0a-aa7d-71b6-b4d6-3be2f0e721ad | done | 2026-10-07 10:43 | pinned |
| ziti-sdk-c: clock-skew hot-loop fix | claude-sg4 | 01a11129-bcc4-7429-84ac-ea003fdd7d36 | needs-input (stack) | 2026-10-06 14:22 | clint's own openziti card, needs-input, last activity 2026-10-06T14:22 |
| atrium | claude-sg4 | 01a06dc7-0879-7068-b1eb-20f5c1979845 | done | 2026-09-30 16:02 | pinned |
| win32crypto-e2e | claude-sg4 | 01a0d3c9-84ad-71e9-88c9-a26e14c3f20a | done | 2026-09-30 15:16 | pinned |
| posture-check-in-native | claude-sg4 | 01a0a081-898f-7e06-8dab-e4556ce8ca4f | done | 2026-09-29 18:32 | pinned |
| desktop-edge-win/promote-2.11.3.1-and-beta | claude-sg4 | 01a0689f-2125-7a19-859f-3de5371b4514 | done | 2026-09-29 18:31 | pinned |
| docpreview/aug-revisions | claude-sg4 | 01a06ccd-a634-7850-9032-67e61b6fb0be | done | 2026-09-29 18:31 | pinned |
| fips-support-take-2 | claude-sg4 | 01a0a648-59e5-76e0-9565-1e971b175f65 | done | 2026-09-29 18:31 | pinned |
| zrok-research | claude-sg4 | 01a0771d-f3f5-7194-8f72-65dd8f351e93 | done | 2026-09-29 14:28 | pinned |
| ziti/discourse-6087 | claude-sg4 | 01a0a152-f6b6-76e4-aa3d-f55bdfcb3743 | done | 2026-09-25 12:27 | pinned |
| discourse-6080 | claude-sg4 | 01a0951d-4ba1-7ac6-a0ac-55a2f85ea27b | done | 2026-09-22 22:58 | pinned |
| discourse-6088 | claude-sg4 | 01a0a153-8b28-71e4-9068-3a6e484aadc5 | done | 2026-09-22 22:58 | pinned |
| css-changes | claude-sg4 | 01a0b4a1-7f73-74c3-8ee6-ba8fbee38587 | done | 2026-09-21 18:25 | pinned |
| debian-package-install-hang | claude-sg4 | 01a0ab96-71ad-7913-9d19-8f39b67c001e | done | 2026-09-19 12:42 | pinned |
| desktop-edge-win/advisory-GHSA-7gg5-xhgg-vxp8 | claude-sg4 | 01a06c50-3490-744c-ac82-b613d2524d32 | done | 2026-09-19 12:41 | pinned |
| desktop-edge-win/sec-report-sep | claude-sg4 | 01a08146-bb63-7183-83ce-b67c80963b01 | done | 2026-09-14 15:12 | pinned |
| ziti-openwrt | claude-sg4 | 01a07cf7-f0e5-7403-a924-1fcbd29710c7 | done | 2026-09-14 15:12 | pinned |
| f-new-openwiki-zrok: zrok provider for OpenWiki | m1mini | 01a0fc7e-2801-734b-aae2-879d4ea1f588 | needs-input (stack) | 2026-10-09 18:09 | session active in the last day (needs-input) |
| u-peek-redesign: the card peek shows only what a human reads or acts on | m1mini | 01a12050-1634-7a87-880e-f8b66a83ff80 | needs-input (stack) | 2026-10-09 13:06 | session active in the last day (needs-input) |
| board-sweep: every card and stack item, sorted from "remove" to "clint checks" | sg4-control | 01a123ee-7b40-7236-9ed4-39e9e9a3f5d0 | running | 2026-10-10 03:51 | running session, last activity 2026-10-10T03:51 |
| interview: card right-click menu redesign | sg4-control | 01a120bb-c16b-754d-bfd8-21fb4f48f2d0 | running (stack) | 2026-10-10 03:51 | running session, last activity 2026-10-10T03:51 |
| orchestrator (sg4-control) | sg4-control | 01a0f2da-2455-7345-a4f5-629b06f7c109 | running (stack) | 2026-10-10 03:51 | running session, last activity 2026-10-10T03:51 |
| win32crypto-e2e | sg4-control | 01a0f2e3-7a27-7d64-8bd5-2c5e88adebe5 | needs-input (stack) | 2026-10-10 03:06 | pinned |
| sdk-golang: e2ee-tls crypto method (C SDK interop, FIPS) | sg4-control | 01a0f2e3-2874-793d-bf19-483040effa1a | needs-input (stack) | 2026-10-09 23:24 | session active in the last day (needs-input) |
| PR 1166: legacy-auth controllers (1.6 + 2.0) and the full e2ee matrix against... | sg4-control | 01a1193a-6c0c-7487-b73b-7e51d1b56883 | needs-input (stack) | 2026-10-09 17:17 | session active in the last day (needs-input) |
| ziti-sdk-c: opt-in e2ee FIPS transition mode (old libsodium peers + tls) | sg4-control | 01a112c0-8922-772b-84ae-6e431d0dce5a | needs-input (stack) | 2026-10-09 17:17 | session active in the last day (needs-input) |
| tlsuv require_fips fallback: rebase onto v0.45.0 | sg4-control | 01a11810-029b-7cc3-801e-8201bfb5ae7d | needs-input (stack) | 2026-10-07 22:34 | clint's own openziti card, needs-input, last activity 2026-10-07T22:34 |
| PR 1167 tlsuv 0.45: diagnose and fix CI test failures | sg4-control | 01a11181-e694-7de5-a142-2f6f1fe3cb56 | needs-input (stack) | 2026-10-06 17:51 | clint's own openziti card, needs-input, last activity 2026-10-06T17:51 |
| r-provision-sgg: put every hand step from adding sgg into provision | sg4-control | 01a108fc-080a-72f9-85ad-be41ecb58ce5 | done | 2026-10-05 01:25 | pinned |

## 4. CLINT CHECKS (63)

| Card title | Room | Card id | Status | Last activity (UTC) | Evidence |
| --- | --- | --- | --- | --- | --- |
| update-tlsuv-0.45 | claude-sg4 | 01a11198-f1f2-771f-bfea-a343ef9dffa6 | done | 2026-10-09 20:57 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| t-tests-fast: the Go test suite runs in minutes, never 30 | claude-sg4 | 01a120e2-c50f-7a52-bcd9-9f82d54163f5 | done | 2026-10-09 14:54 | work not on claude/main: UNIQUE:2 commit(s) on claude/t-tests-fast; land it or drop it? |
| f-agent-comms: done takes a sha or an artifact, any hub sha counts, one messa... | claude-sg4 | 01a12037-95e2-77a6-98ab-b87217d0eff3 | done | 2026-10-09 11:00 | work not on claude/main: UNIQUE:1 commit(s) on claude/f-agent-comms; land it or drop it? |
| eval-tokens-1008: tonight's burn as % of the weekly allotment | claude-sg4 | 01a12028-0848-74c3-9d96-b6a723ec772f | done | 2026-10-09 10:28 | done 2026-10-09, no landing evidence found; decide: still wanted, or exit it? |
| clintify: own the clint style-model experiment | claude-sg4 | 01a117dd-7241-7a51-8e4b-9f378c989d9c | needs-input (stack) | 2026-10-08 22:29 | needs-input since 2026-10-08T22:29; decide: still wanted, or exit it? |
| claude/main:atrium | claude-sg4 | 01a11d4b-165b-7b42-903b-44cfc94828c1 | done | 2026-10-08 21:00 | done 2026-10-08, no landing evidence found; decide: still wanted, or exit it? |
| clintify bakeoff round 2: five blind tasks | claude-sg4 | 01a11b97-10a8-7235-a9b8-f04508dbaa02 | needs-input (stack) | 2026-10-08 15:36 | needs-input since 2026-10-08T15:36; decide: still wanted, or exit it? |
| clintify bakeoff: blind drafts | claude-sg4 | 01a1194c-f74b-78cf-80b6-8aa81cb1a93e | needs-input (stack) | 2026-10-08 04:20 | needs-input since 2026-10-08T04:20; decide: still wanted, or exit it? |
| token-eval-2026-10-08: where the weekly allotment went, and what to cut | claude-sg4 | 01a1194e-a475-7b89-bae6-8e4f591e685e | done | 2026-10-08 03:44 | done 2026-10-08, no landing evidence found; decide: still wanted, or exit it? |
| u-resume-menu-missing | claude-sg4 | 01a11806-ea2b-7911-b9ef-8c807bfdc1df | needs-input (stack) | 2026-10-07 23:00 | work not on claude/main: UNIQUE:1 commit(s) on claude/u-resume-menu-missing; land it or drop it? |
| m-doc-overhaul: full atrium doc overhaul | claude-sg4 | 01a116bc-938c-709b-94c2-fbeaa41e6681 | needs-input (stack) | 2026-10-07 21:11 | work not on claude/main: UNIQUE:1 commit(s) on claude/m-doc-overhaul; land it or drop it? |
| feature/966-node-proxy-server | claude-sg4 | 01a11807-b75a-7472-b535-f5aad7253c7f | done | 2026-10-07 20:22 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| feature/966-node-proxy-server | claude-sg4 | 01a11803-53c1-7835-8bf2-8ca468b7e921 | done | 2026-10-07 20:21 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| ziti-console/feature/966-node-proxy-server | claude-sg4 | 01a11804-51ca-7d38-9e9a-ca27211309c0 | done | 2026-10-07 20:18 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| f-hub-fetch-forge-branch: the hub fetches a forge branch a card asks for | claude-sg4 | 01a1177a-e88e-7adc-a369-09572252d802 | done | 2026-10-07 18:21 | done 2026-10-07, no landing evidence found; decide: still wanted, or exit it? |
| discourse-6171 | claude-sg4 | 01a11741-ba21-7c98-8bde-f59a0ac63583 | done | 2026-10-07 18:15 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| r-open-support-kinds-board: the board for opening every link kind | claude-sg4 | 01a11707-eeb1-75ee-b9a8-5d98a47abd99 | done | 2026-10-07 16:29 | done 2026-10-07, no landing evidence found; decide: still wanted, or exit it? |
| f-board-views: one board view per worker worktree | claude-sg4 | 01a11699-02e0-796d-adc7-7da5265cf6c4 | done | 2026-10-07 15:23 | done 2026-10-07, no landing evidence found; decide: still wanted, or exit it? |
| clintify mine 3/4 | claude-sg4 | 01a1114c-7d07-7f11-925c-f05000fc51ee | needs-input (stack) | 2026-10-06 15:04 | needs-input since 2026-10-06T15:04; decide: still wanted, or exit it? |
| clintify mine 4/4 | claude-sg4 | 01a1114c-9203-74d3-824e-b9558d946e7e | needs-input (stack) | 2026-10-06 15:03 | needs-input since 2026-10-06T15:03; decide: still wanted, or exit it? |
| ziti/ui-for-config | claude-sg4 | 01a0fd06-5966-7893-a9bc-d51ed4dc57c2 | done | 2026-10-02 14:30 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| runtime: director of the daemon and store, on claude/runtime | claude-sg4 | 01a0f274-e450-77ff-9af6-5606f0ab8e24 | done | 2026-10-01 18:49 | work not on claude/main: UNIQUE:9 commit(s) on claude/r-local-operator; land it or drop it? |
| live412-scratch: scratch card for phone tests | claude-sg4 | 01a0f722-373b-7bb4-a427-08762974d6ab | needs-input (stack) | 2026-10-01 13:25 | work not on claude/main: UNIQUE:1 commit(s) on claude/live412-scratch; land it or drop it? |
| u-code-review-mock: two phone diff view prototypes | claude-sg4 | 01a0f554-4344-72aa-ba16-45002ba842cf | needs-input (stack) | 2026-10-01 07:46 | work not on claude/main: report sha patch not on main; land it or drop it? |
| ziti-sdk-c: e2ee-tls docs, fill Azure matrix results (retry) | claude-sg4 | 01a0d9b7-9ec4-7a9a-b754-eafdfc6b5c12 | needs-input (stack) | 2026-09-29 21:06 | openziti card waiting since 2026-09-29; decide: still wanted, or exit it? |
| t-unit-vs-integration | m1mini | 01a1231c-3959-794a-b8a0-93e3c37a3194 | done | 2026-10-10 01:04 | work not on claude/main: report sha patch not on main; land it or drop it? |
| interview: forge access, paste-open, private repos | m1mini | 01a1204f-00d2-7399-9fd5-ef001b7ae858 | done | 2026-10-09 12:47 | done 2026-10-09, no landing evidence found; decide: still wanted, or exit it? |
| u-last-read-exact: the divider lands exactly where the operator's scrollback was | m1mini | 01a12028-8e9d-7a64-ba5f-d93dceda4d29 | done | 2026-10-09 10:55 | work not on claude/main: UNIQUE:1 commit(s) on claude/u-last-read-exact-w; land it or drop it? |
| r-cycle-prep-notice: atrium tells a card to write its handoff well before the... | m1mini | 01a12028-e028-7afc-ac6d-7c280f3d83b1 | done | 2026-10-09 10:28 | work not on claude/main: UNIQUE:1 commit(s) on claude/r-cycle-prep-notice-w; land it or drop it? |
| H16b u-board-running-column-empty: working cards sit in Ready, Running stays ... | m1mini | 01a1184f-aeee-78cd-b721-d071b7adba41 | done | 2026-10-07 21:42 | done 2026-10-07, no landing evidence found; decide: still wanted, or exit it? |
| p-ziti-push: prove atrium_git_push from a hand-made clone | m1mini | 01a10ceb-7e40-793b-abe9-7e69cbf73111 | done | 2026-10-05 16:35 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| ziti-embedded-controller-2: verdict, fatal-hook experiment, ziti-only routes | m1mini | 01a10c80-cb58-7b4a-94ca-d06150d2e00d | done | 2026-10-05 16:06 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| ziti-embedded-controller: PRD, case for and against, design ideas, spike | m1mini | 01a10c6c-5dc3-7455-9857-7dbd94a5cb31 | done | 2026-10-05 14:38 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| r-new-mercurius-one-shot | m1mini | 01a109d8-8b65-73d0-8ad7-32392af1ac72 | needs-input (stack) | 2026-10-05 04:16 | needs-input since 2026-10-05T04:16; decide: still wanted, or exit it? |
| r-new-exit-guard | m1mini | 01a109d5-6082-7639-bb3f-0b50e7285183 | done | 2026-10-05 02:12 | work not on claude/main: report sha patch not on main; land it or drop it? |
| runtime: director of the daemon and store, on claude/runtime | m1mini | 01a0f8cc-0760-7e2c-9ac6-8c0b7a5ab38e | done | 2026-10-03 03:03 | work not on claude/main: UNIQUE:10 commit(s) on claude/runtime; land it or drop it? |
| codex: 0.159.0 to 0.159.3 | m1mini | 01a0f86c-9916-7451-9280-dc1af852a681 | backlog | 2026-10-01 17:04 | backlog item 2026-10-01; decide: still wanted, or exit it? |
| u-card-menu-restart: "restart" on the card right-click menu | sg3 | 01a12029-c3de-7d01-a0a6-b559244986fb | done | 2026-10-09 10:42 | work not on claude/main: report sha patch not on main; land it or drop it? |
| f-paste-open-no-gh: opening a pasted PR makes zero gh reads, like gwt | sg3 | 01a12028-6bd7-7344-b7f4-de5415b71f22 | done | 2026-10-09 10:37 | work not on claude/main: UNIQUE:3 commit(s) on claude/f-paste-open-no-gh-w; land it or drop it? |
| openziti/ziti-console#964 New list-table features: column-menu control and au... | sg3 | 01a11c03-bcfc-7092-a262-c73e4247adf9 | done | 2026-10-08 16:40 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| openziti/ziti-console#967 Replace the node server intermediary API with pass-... | sg3 | 01a11c04-2da1-746f-b925-d566389ff12f | done | 2026-10-08 16:39 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| sg3-probe: can sg3 build ziti-sdk-c? | sg3 | 01a0fd18-0791-71c0-94ce-e280074c97a3 | needs-input (stack) | 2026-10-02 17:02 | needs-input since 2026-10-02T17:02; decide: still wanted, or exit it? |
| mg-leaks: mcp-gateway leaks, why 136 discourse backends piled up | sg3 | 01a0f85e-db35-7361-8cce-c27341649df6 | needs-input (stack) | 2026-10-02 16:17 | needs-input since 2026-10-02T16:17; decide: still wanted, or exit it? |
| u-suite-webgl: headless suite without software WebGL, /m picks up a new build | sg3 | 01a0f4a7-6220-7344-8af1-89a31bfe664d | done | 2026-10-01 05:51 | work not on claude/main: report sha patch not on main; land it or drop it? |
| u-031: Shift+right click terminal menu with new context | sg3 | 01a0f032-6c05-72e1-ab3d-dffe5ab49084 | done | 2026-09-30 03:09 | work not on claude/main: report sha patch not on main; land it or drop it? |
| u-029: key bar labels, no stuck highlight, tap sends no Up/Down | sg3 | 01a0f026-ec72-7852-9777-d0e6f188bc12 | done | 2026-09-30 02:40 | work not on claude/main: report sha patch not on main; land it or drop it? |
| r-029: design, atrium cycles a card's context by itself at the threshold | sg3 | 01a0f01b-ec90-7438-9013-3490d2fa51c9 | done | 2026-09-30 02:25 | work not on claude/main: report sha patch not on main; land it or drop it? |
| u-023: phone terminal gets 75% of the screen | sg3 | 01a0efba-912d-78b8-8329-522a121f9194 | done | 2026-09-30 01:22 | work not on claude/main: report sha patch not on main; land it or drop it? |
| u-024: /m phone page, home and card view (Path 2 M1a) | sg3 | 01a0efc4-43e7-710a-a1df-a1e023215c68 | done | 2026-09-30 01:22 | work not on claude/main: report sha patch not on main; land it or drop it? |
| u-025: /m composer and permission rows (Path 2 M1b) | sg3 | 01a0efc4-5829-797a-9899-b058cebf00c0 | done | 2026-09-30 01:22 | work not on claude/main: report sha patch not on main; land it or drop it? |
| u-026: phone lists and tab row fit, toasts become a bell nudge | sg3 | 01a0efc8-a0a1-7bd6-92af-765ba2cdf048 | done | 2026-09-30 01:22 | work not on claude/main: report sha patch not on main; land it or drop it? |
| interview: how work is forked, built, merged, landed and deployed | sg4-control | 01a120b3-5c10-7e1a-8508-0d9c1d9c0890 | done | 2026-10-10 03:50 | work not on claude/main: UNIQUE:2 commit(s) on claude/interview-merge-flow; land it or drop it? |
| tlsuv require_fips: SCHANNEL_CRED fallback for Server 2016 | sg4-control | 01a117cd-eeef-7689-a914-e932dd105aa1 | done | 2026-10-07 19:34 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| mx-v0450 windows: SDK on tlsuv v0.45.0, local + Azure FIPS matrix | sg4-control | 01a11156-29a1-7697-b208-d50e491f8916 | needs-input (stack) | 2026-10-06 15:49 | needs-input since 2026-10-06T15:49; decide: still wanted, or exit it? |
| ziti-sdk-c: tlsuv v0.45.0 bump + API changes, split out of PR #1166 | sg4-control | 01a1115c-90d0-7db6-9696-bbac38f31a06 | done | 2026-10-06 13:24 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| qa-functional: sdk-golang e2ee-tls fix diff | sg4-control | 01a10e1d-afff-781e-8d6c-b80403eeab27 | done | 2026-10-05 22:15 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| qa-nonfunctional: sdk-golang e2ee-tls fix diff | sg4-control | 01a10e1d-d018-7e78-b0ac-dff46b5547ee | done | 2026-10-05 22:13 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| tlsuv: win32crypto TLS 1.3 post-handshake fix (e2ee-tls support for ziti-sdk-c) | sg4-control | 01a10db5-24da-79be-ad36-095e7b4002fa | done | 2026-10-05 20:19 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| card-audit: every card, keep or ditch, and the disk behind it | sg4-control | 01a0f404-04a6-7986-9a87-480631aa957c | needs-input (stack) | 2026-10-03 01:13 | needs-input since 2026-10-03T01:13; decide: still wanted, or exit it? |
| ghsa-rg2j-wgmp-vxr5 | sg4-control | 01a0f2e3-6732-785b-8888-708ce20d917b | done | 2026-10-01 18:16 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| discourse-6148 | sg4-control | 01a0f346-da74-73a7-95b8-9d42641aa146 | done | 2026-10-01 12:42 | done openziti-repo card, no landing evidence here; decide: still wanted, or exit it? |
| ziti-sdk-c: Linux e2ee-tls on the OpenSSL FIPS provider (proof) | sg4-control | 01a0f2e3-3bc0-7136-9129-077f7ed3e28b | needs-input (stack) | 2026-09-30 17:16 | openziti card waiting since 2026-09-30; decide: still wanted, or exit it? |
| tlsuv win32crypto: RSA PKCS#1 identity key fails set_own_cert (NCryptImportKe... | sg4-control | 01a0f2e1-d1b6-715f-b193-06ad2fd8f28c | needs-input (stack) | 2026-09-30 17:14 | openziti card waiting since 2026-09-30; decide: still wanted, or exit it? |

## Other junk noticed on the way (not removed, not on any card)

### Worktrees with no card (25)

Listed by `git worktree list` and not the worktree of any card on any room. "Dirty" is the count of modified or untracked paths: those hold uncommitted work, so look before removing.

| Worktree | Branch | Branch status | Dirty paths | Evidence |
| --- | --- | --- | --- | --- |
| D:/tmp/atrium-verify | DETACHED | - | 98 | detached or unmerged-unknown: no branch to test; 98 uncommitted path(s) |
| D:/tmp/ops-land-1009b-base | DETACHED | - | 0 | detached or unmerged-unknown: no branch to test |
| D:/tmp/ops-land-1009b-f-interview-launch-kind | DETACHED | - | 0 | detached or unmerged-unknown: no branch to test |
| D:/tmp/ops-land-1009b-r-typing-fixes | DETACHED | - | 0 | detached or unmerged-unknown: no branch to test |
| D:/worktrees/atrium-minion | claude/minion | - | 1 | detached or unmerged-unknown: no branch to test; 1 uncommitted path(s) |
| D:/worktrees/atrium-t-guard-memo | claude/t-guard-memo | LANDED-PATCH | 0 | every commit of the branch has its patch on claude/main |
| D:/worktrees/claude/atrium/land-spam | DETACHED | - | 0 | detached or unmerged-unknown: no branch to test |
| D:/worktrees/claude/reviews/github-dovholuknf-atrium/branch-runtime-4316d33 | DETACHED | - | 0 | detached or unmerged-unknown: no branch to test |
| D:/worktrees/github/dovholuknf/atrium/b2-04-animated-states | b2-04-animated-states | MERGED-OLD | 7 | branch tip is an ancestor of claude/main-before-scrub; 7 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b2-07-save-session | b2-07-save-session | MERGED-OLD | 5 | branch tip is an ancestor of claude/main-before-scrub; 5 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b3-05-which-conversation | b3-05-which-conversation | MERGED-OLD | 3 | branch tip is an ancestor of claude/main-before-scrub; 3 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b3-19-spinner-replay | b3-19-spinner-replay | MERGED-OLD | 2 | branch tip is an ancestor of claude/main-before-scrub; 2 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b3-43-prompt-args | b3-43-prompt-args | MERGED-OLD | 2 | branch tip is an ancestor of claude/main-before-scrub; 2 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b4-15-dir-picker | b4-15-dir-picker | MERGED-OLD | 3 | branch tip is an ancestor of claude/main-before-scrub; 3 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b4-22-throwaway | b4-22-throwaway | MERGED-OLD | 17 | branch tip is an ancestor of claude/main-before-scrub; 17 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b4-23-two-panes | b4-23-two-panes | MERGED-OLD | 6 | branch tip is an ancestor of claude/main-before-scrub; 6 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b4-42-scrollback-display | b4-42-scrollback-display | MERGED-OLD | 2 | branch tip is an ancestor of claude/main-before-scrub; 2 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b4-44-chip-skin | b4-44-chip-skin | MERGED-OLD | 2 | branch tip is an ancestor of claude/main-before-scrub; 2 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b5-18-paste-pipe | b5-18-paste-pipe | MERGED-OLD | 10 | branch tip is an ancestor of claude/main-before-scrub; 10 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b5-33-three-names | b5-33-three-names | MERGED-OLD | 6 | branch tip is an ancestor of claude/main-before-scrub; 6 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b5-36-focus-hover | b5-36-focus-hover | MERGED-OLD | 3 | branch tip is an ancestor of claude/main-before-scrub; 3 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b5-39-wording-fix | b5-39-wording-fix | MERGED-OLD | 4 | branch tip is an ancestor of claude/main-before-scrub; 4 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/b5-41-perms-badge | b5-41-perms-badge | MERGED-OLD | 2 | branch tip is an ancestor of claude/main-before-scrub; 2 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/rb1 | rb1 | MERGED-OLD | 62 | branch tip is an ancestor of claude/main-before-scrub; 62 uncommitted path(s) |
| D:/worktrees/github/dovholuknf/atrium/rbase | DETACHED | - | 62 | detached or unmerged-unknown: no branch to test; 62 uncommitted path(s) |

### Hub claude/* branches already merged (7)

Tip is an ancestor of the old main. Safe to delete from the hub.

| Hub branch | Tip | Evidence |
| --- | --- | --- |
| claude/base-0b4eb8a5 | 0b4eb8a5 | MERGED-OLD: tip is an ancestor of claude/main-before-scrub |
| claude/r-codeman-comparison | 0fdc8b76 | MERGED-OLD: tip is an ancestor of claude/main-before-scrub |
| claude/r-msg-m1 | 71e5be9f | MERGED-OLD: tip is an ancestor of claude/main-before-scrub |
| claude/r-plan-messages-tests | 9e6e1329 | MERGED-OLD: tip is an ancestor of claude/main-before-scrub |
| claude/t-unit-vs-integration | 6bf77cba | MERGED-OLD: tip is an ancestor of claude/main-before-scrub |
| claude/u-drag-no-click | ce4d5b74 | MERGED-OLD: tip is an ancestor of claude/main-before-scrub |
| claude/u-drag-no-click-w | f9b3d0a0 | MERGED-OLD: tip is an ancestor of claude/main-before-scrub |

### Hub claude/* branches landed by patch (57)

Not an ancestor of either main (history was rewritten and squashed), but every commit on them has its patch-id on claude/main. These are dead the same way.

| Hub branch | Tip |
| --- | --- |
| claude/base-167d46d0 | 167d46d0 |
| claude/f-hub-ci-runs | a253fcb9 |
| claude/f-hub-ci-runs-win | b9b558e6 |
| claude/f-hub-restart-pause-only | 1a44a272 |
| claude/f-paste-least-busy-scm | 09ee35d6 |
| claude/f-paste-open-speed | a5bccdb8 |
| claude/f-paste-open-speed-w | 69563374 |
| claude/f-pr-launch-fast | 8b2768b5 |
| claude/f-room-bringup | 17c1f4a4 |
| claude/f-room-spec | a0ace162 |
| claude/f-room-spec-d3 | 228aa118 |
| claude/f-room-spec-hub | cc3be09f |
| claude/f-room-spec-landed | 68cda4c7 |
| claude/f-say-resolve-across-rooms | 918cc1fd |
| claude/r-agent-loop-tools | 78393116 |
| claude/r-card-never-blocked-forever | 704efd9c |
| claude/r-card-reclaim-on-done | 14cfa89a |
| claude/r-card-restart | e4ff1ac7 |
| claude/r-context-always-cycles | 3cd8096a |
| claude/r-context-cycle | fb62c770 |
| claude/r-context-limit-one-source | 9408628f |
| claude/r-context-nudge-handoff | 7b3ba864 |
| claude/r-cycle-before-runner-compacts | b98cecbb |
| claude/r-expected-end-notices | 61e1d050 |
| claude/r-git-push-probe | 13724a31 |
| claude/r-graceful-room-restart | 8eaad37a |
| claude/r-nothing-gets-lost | 4967964d |
| claude/r-silent-stop-self-nudge | 3f570d22 |
| claude/r-throwaway-overwrites-whereami | d6322abd |
| claude/r-turn-cost-by-cause | 69001ac1 |
| claude/r-worker-budget | 735c531f |
| claude/r-worker-test-waste | ca31bcc2 |
| claude/t-no-pwsh-on-linux | 361db150 |
| claude/u-board-tab-disaster | f9b64df4 |
| claude/u-border-colour-lag | 44e591c3 |
| claude/u-card-color-default | 64d8a736 |
| claude/u-card-colors-on-daemon | b9738bef |
| claude/u-card-colors-on-hub | a4ad4961 |
| claude/u-card-menu-restart-w | 681f00b0 |
| claude/u-discrete-group-sort | f645a289 |
| claude/u-dropdowns-instant | 2de1f919 |
| claude/u-grouping-per-view | e46b79aa |
| claude/u-last-read-off-w | 7014bfa2 |
| claude/u-light-theme-dim-text | f57996c9 |
| claude/u-one-hover-all-state | 5493933f |
| claude/u-paste-a-link-style | af092677 |
| claude/u-paste-in-switcher | fe99f98f |
| claude/u-peek-answer-flyout | 1708590b |
| claude/u-peek-launch-short | 7fc66b2f |
| claude/u-peek-zero-counts | af5f9b71 |
| claude/u-qa-ctrl-enter-sends | f1b4385d |
| claude/u-restore-tab-bridge | 5fe32f98 |
| claude/u-term-debug-and-lag | a50103eb |
| claude/u-terminal-last-read-marker | a5bccdb8 |
| claude/u-terminal-last-read-marker-w | 48ed0177 |
| claude/u-theme-debug | 41780f85 |
| claude/u-theme-picker-preview | c612e15e |

### Hub claude/* branches with work not on claude/main (8), for information

| Hub branch | Tip | Status |
| --- | --- | --- |
| claude/f-paste-open-no-gh-w | 0fd6d227 | UNLANDED: 3 commit(s) not on claude/main |
| claude/interview-card-menu | c69b3e5e | UNLANDED: 3 commit(s) not on claude/main |
| claude/interview-merge-flow | b550e917 | UNLANDED: 2 commit(s) not on claude/main |
| claude/r-cycle-prep-notice-w | 7e91fd6d | UNLANDED: 1 commit(s) not on claude/main |
| claude/t-parallel | d65e369d | UNLANDED: 3 commit(s) not on claude/main |
| claude/u-last-read-exact-w | 52a26f58 | UNLANDED: 1 commit(s) not on claude/main |
| claude/u-peek-redesign-w | ee515430 | UNLANDED: 1 commit(s) not on claude/main |
| claude/u-room-header-caret-w | e3ac3402 | UNLANDED: 1 commit(s) not on claude/main |

### Local claude/* branches in the main checkout

117 local claude/* branches: 14 merged by ancestry, 86 landed by patch, 17 with commits not on claude/main (the same unlanded ones the cards above name). Counted only, not listed: the card tables carry the per-branch evidence.
