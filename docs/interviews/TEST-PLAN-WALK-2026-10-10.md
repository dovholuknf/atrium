# Test plan walk, 2026-10-10: results

Walked with clint against the deployed build (every room and the hub on 3af76d87, board hash 22926d19). clint ended
the walk partway through the list: "let's just consider this done".

**Counts:** 22 pass, 7 fail, 20 skip. 28 later sections were not walked (listed below).

## Fails, in his words

- Repos page, reload three times: "on refresh the stack (first tab) is shown, pause for approx 1s then it flips to
  repos, pause for 1s , then i see data. it is not immediate, it does not stay on screen, it does seem to 'flicker' and
  doesnt show 'building' but it's overall 'atrium related refresh' reasons not repos"
- Speech, Win+H "new line": it pressed Enter, so "first part new line second part" sent "first part" on its own.
  "I don't think I mined to be honest. You will see that it screwed up the word mind. Maybe I'll be able to make a
  macro that would allow me to add new lines if I wanted them"
- Speech, Ctrl+Alt+D sketch in Brave: "i see dictaion: network. i don't see a grey ppreivew tho" (Brave blocks the
  speech service). He will use Windows dictation: "Kind of works seems like it's OK for now I'm going to use Windows
  dictation"
- Backlog headers: "i didn't see that header ... that needs better presentation it was not clear it was a table also
  the rows ned to be the same width". There is no `created` header. "a light ui pass is mandated"
- Card menu, show stopped: "show stopped works but it conflates with 'hide inactive' and it's in the same group ... so
  that's 'not quite right'"
- Room gate check on sg3 (run by the agent): one gate as intended, but one todo is left: "would delete
  ~/.claude/hooks/atrium-perm-hook.ps1"
- Close a card, one dialog: "does not work that's a bug still". Caveat: the agent had already closed the throwaway
  card from the CLI when clint said "consider this done", so his try may have been on a card already gone.

## Other findings from the walk

- A popped-out terminal window has no "details" pill and no gate line. Both need to be there.
- Stop on a pinned card takes it out of the pinned group. He expected it to stay there, dimmed.
- The copy icon after an @handle chip "needs tobe IN the bubble and needs TIGHT horizontal padding. right now the copy
  button is in nomans land".
- Settings UI: "it needs a fucking overhaul too. it's a disaster and hodgepodge of things we just never use/finished".
  `merged_cull_grace` is not on it at all (the API reads "off").
- `atrium backlog file --priority urgent` prints its refusal twice with the usage block in between, and the flag help
  leaves out unset.
- Left arrow and the agent view: "we made a feature for this? ... claude itsefl supports turning this off so - maybe a
  dumb idea".
- A push from the operator's shell in a card's worktree is refused: "this room's hub remote answers a card with its
  atrium token, and this request has none that is good". This worktree's `hub` remote points at :7787 and not at the
  room's forwarder on :7777, so the agent's own `git fetch hub` and `git push hub` are refused by its hook.
- Terminate and clean up could not be tested on a worktree with unsaved work. `atrium open` refuses to make a worktree
  in the operator-made clone without his yes to add atrium's remotes.

## Not walked

gitsync-full-run-hangs, peek-delivered-not-queued, new-context-clearing, background-wait,
queued-say-lost, repos-test-building, terminals-ended-cards, secret-name-suffixes, agent-loop-followups,
lean-agents-builtins, details-hover-text, timeline-dots, clear-mark-escape, settle-race-tests, macos-suite-red,
child-cards-colour, main-refreshed-flaky, subagent-parent-theme, two-window-size, clear-mark-ring, hub-gets-snapshot,
md-open-rendered, alias-chip-copy, room-hub-tools, details-side-by-side, idle-park-cog, alias-default-branch,
say-log-view phase 1.

---

# Morning test plan, 2026-10-10 factory run

One section per change merged into claude/main since 229a921d, headed by the merge SHA. Append new sections at the
bottom. Each question is one scenario: do this, then tell me what you saw and whether you liked it.

## 5f8086d1: Merge repos-page-fast

What changed: the repos list on the hub reads a cached snapshot instead of running git on every load. It answers with
`computed`, `age_seconds` and `building`, and the page polls again while the hub says `building`. A push or an init
rebuilds the snapshot.

1. On the hub board, open the repos page. How long until the list was on screen? Did it feel instant, or did you wait?
2. Reload the page three times in a row. Did the list ever blank out or flicker, or did it stay put?
3. Push a branch to the hub from a room, then watch the repos page. How long until the new branch or commit showed
   up? Did you have to reload, or did it appear by itself?
4. Right after a hub restart, open the repos page. Did you see a "building" state, then the list fill in? Was it clear
   the hub was working and not stuck?

Result (1): pass
Result (2): fail, "on refresh the stack (first tab) is shown, pause for approx 1s then it flips to repos, pause for 1s , then i see data. it is not immediate, it does not stay on screen, it does seem to 'flicker' and doesnt show 'building' but it's overall 'atrium related refresh' reasons not repos"
Result (3): skip, the push could not run: the agent's push was blocked by the hook, and clint's push from this session was refused with "this room's hub remote answers a card with its atrium token, and this request has none that is good"
Result (4): skip, needs a hub restart

## 8d93f2bc: Merge spike-speech

What changed: a speech-to-text spike. Nothing is on by default. `docs/spikes/speech-to-text.md` says Win+H may already
do the job. `js/dictate.js` is a sketch: Ctrl+Alt+D toggles the browser's speech recognition and pastes each phrase into
the focused terminal with no Enter. It is off unless `localStorage["atrium.dictate"]` is `"1"`. Not tried in a browser
yet.

Test A, the doc's section 1 test, two minutes. Do this first.

1. On the hub board, open a terminal and click it so it has focus. Press Win+H and say "hello world comma this is a
   test". Did the text land on the input line? Was the comma a comma, or the word "comma"?
2. Still in Win+H, say "new line". Did it only add a line break, or did it press Enter and submit? If Enter fired, stop
   here and say so: that is the case the dictate.js sketch is for.
3. Say a command with atrium words, such as "atrium underscore say" or "claude slash main". What came out? Did you
   have to fix it by hand, and would you put up with that for a real interview answer?

Result (1): pass, dictated straight into this interview: "Hello world this is a test this came from Clint talking to you Using Windows H key". The text landed. Comma not tested: "I did not press or ask for a comma last time".
Result (2): fail, Win+H "new line" pressed Enter: dictating "first part new line second part" sent just "first part" to the session. The dictate.js case. His view: "I don't think I mined to be honest. You will see that it screwed up the word mind. Maybe I'll be able to make a macro that would allow me to add new lines if I wanted them"
Result (3): skip, Test A stopped at Enter firing, as the plan says.
Result (4): pass, screenshot: after opting in and reloading, the mic prompt ("http://localhost:7778 is asking you to use your microphones") and the red "listening" badge at the bottom right both appeared (browser looks like Brave).
Result (5): fail, "i see dictaion: network. i don't see a grey ppreivew tho". Likely Brave: it exposes webkitSpeechRecognition but blocks Google's speech service, so it fails with a "network" error. The sketch could detect this and say so.
Result (6-8): skip, "skip for now. Kind of works seems like it's OK for now I'm going to use Windows dictation"

Test B, the Ctrl+Alt+D sketch. Use Chrome or Edge on the Windows box.

4. Opt in: open devtools on the hub board, run `localStorage.setItem("atrium.dictate", "1")`, and reload. Focus a
   terminal and press Ctrl+Alt+D. Did a red badge appear at the bottom right, saying "listening"? Did the browser ask
   for the mic, and did you allow it?
5. Say two sentences, then pause. Did the grey interim text show in the badge while you spoke? Did each finished phrase
   land on the terminal's input line without pressing Enter? Did you have to press Enter yourself?
6. Stay quiet for 10 seconds, then say something. Did it keep listening through the pause, or did you have to press
   Ctrl+Alt+D again?
7. Press Ctrl+Alt+D to stop, then say something. Did the badge go away and the text stop? Do you want this on all the
   time, or push-to-talk (hold a key), or a mic button on the board?
8. Clean up: run `localStorage.removeItem("atrium.dictate")` and reload. Did Ctrl+Alt+D then do nothing?

## 55859aa9: Merge backlog-priority

What changed: backlog priority is one of high, medium, low or unset. The hub folds old rows (HIGH to high, blank and
junk to unset). The board backlog tab sorts by clicking a header, filters by priority chips, and changes a row's
priority from a dropdown. `atrium backlog file` folds a typed HIGH to high and refuses anything else. Hub deploy
needed first: it adds migration 0014_backlog_priority.

1. On the hub board, open the backlog tab. Click the `priority` header, then click it again. What order did the rows
   take each time? Was it obvious which way it was sorted?
2. Click `dept`, then `created`. Did each header sort the way you expected? Which header did you want that is missing?
3. Light the `high` chip, then add `low`. Which rows stayed? Un-light both. Did the list come back whole? Did you like
   the chips next to the department and open-only filters, or did they crowd the row?
4. On one row, change the priority from its dropdown. Reload the page. Did the new value stick? Did the row move to its
   new place straight away, or did it jump later?
5. Pick the wrong value on purpose (if you can make the dropdown refuse). Did the board say why in a line under the
   filters, and was it readable?
6. In a terminal run `atrium backlog file x-test --dept fabric --title t --priority HIGH`, then `atrium backlog show
   x-test`. Did it read `high`? Then repeat with `--priority urgent`. Did the refusal list high, medium, low and
   unset?
7. Run `atrium backlog priority x-test unset`, then `atrium backlog list --priority unset`. Is x-test in the list?
   Close it afterward with `atrium backlog status x-test dropped`.
8. Look at the whole list, with no sort clicked. Is the default order (high first, then newest) what you want to see
   when you open the page?

Result (1): pass, corrected after he found the header row: "previous is 'pass' but the header is 'status' 'dept' 'id' priority, title...". He first clicked the priority chip and took it for the header: "it does not sort instead it is filtering however having a filter option is cool so like a show only sort of idea Also it would be nice if I could sort by priority it should just be a sort button and show the little arrow U or down"
Result (2): fail, "i didn't see that header ... the header is 'status' 'dept' 'id' priority, title... that needs better presentation it was not clear it was a table also the rows ned to be the same width". Screenshot: the header row is small grey caps (STATUS ▲ DEPT ID PRIORITY TITLE). The columns don't line up because id and title widths vary per row, and there is no `created` header.
Result (3): pass, "additive confirmed - pass"
Result (4): pass, "nice pass"
Result (5): skip, the dropdown offers only valid values, so a refusal can't be forced from the board
Result (6): pass, "pass". The agent ran it (no `atrium` on the bash PATH, the binary is at C:\Users\claude\.atrium\bin\atrium.exe). HIGH showed as `high`. "urgent" was refused with "it is high, medium, low, or unset (empty)". Nits: the refusal prints twice with the usage block in between, and the --priority help says "high, medium or low" with no unset.
Result (7): pass, run by the agent. "x-test priority is now unset" and x-test was first in `backlog list --priority unset`. Dropped afterwards.
Result (8): pass, "it's fine. the page overall is 'clunky' still but it's getting the job done so we'll leave it be fore now. justified columns will help so a light ui pass is mandated"

Known gap: the default order puts interviewed items first, but the hub does not send `interviewed` yet. Question 8
can only judge high-to-low, then newest.

## 96b7b0b5: Merge card-menu-impl

What changed: the terminals tab right-click menu is reordered and trimmed. A running card offers restart, new context,
settings, stop, and terminate and clean up. Stop takes a card out of the list with no question, and a "show stopped"
pill brings it back with a resume item. The phone gets the same menu as a long-press sheet.

1. On the hub board terminals tab, right-click a running card. What items did you see, in what order? Did the order
   match how often you use them? Did any (?) help text tell you something you did not know?
2. Pick `stop` on a card. Did it leave the list at once with no question? Was that what you expected, or did you want a
   confirm?
3. Turn on the `show stopped` pill. Where did the stopped card come back? Right-click it. Did the menu offer resume
   and not stop? Click resume. Did it come back alive?
4. Pick `terminate and clean up` on a card whose folder is clean. Did it close with no question? Then try it on a
   card with uncommitted work. Did the question say what would be lost? Tick "never show me this again": did the next
   one close without asking? Is that tick safe, or scary?
5. With custom grouping on, right-click a card. Are `add to group` and `remove from group` where you look for them?
   Did `remove from group` show only for a card in a group?
6. Click a card's title in the list. Did the alias dialog open? Was that what you expected from a click on a title?
7. On the phone view, long-press a card. Did you get the same menu in the same order as a sheet? Did tapping a (?)
   show its help?

Result (1): pass
Result (2): pass, but "i stopped dotfiles. it is pinned now i can onlyh see it in the stack/board. it somehow is missing from the pinned group. i expected to see it ... like thise". His screenshot shows a dimmed, star-greyed row in PINNED (r-provision-sgg). He expects a stopped pinned card to stay in the pinned group, dimmed like that.
Result (3): fail (UI placement), "show stopped works but it conflates with 'hide inactive' and it's in the same group ... so that's 'not quite right'". Screenshot: the pill sits under the HIDE INACTIVE heading next to "agents (17)" and "subagents (10)", so a "show" pill lives in a "hide" group.
Result (3b, resume): pass, "i resumed it and it's fine pass"
Result (4): skip. First try was set up wrong by the agent: two Haiku cards via `atrium launch` in hand-made folders own no worktree, so both closed with no question and nothing was removed. A proper try needs `atrium open` for an owned worktree, and that was refused: "the clone of this repo in the scm folder was made by the operator and atrium has not been given a yes to add its remotes to it". clint chose to skip rather than give that yes. The question for uncommitted work is untested.
Result (5): pass, "yes all menu things on the terminal look fine"
Result (6): pass, with a CSS nit: "the fucking copy button needs tobe IN the bubble and needs TIGHT horizontal padding. right now the copy button is in nomans land". Screenshot: the copy icon floats in the gap between the @orchestrator chip and the room chip, outside the chip.
Result (7): skip

Known gap: `scripts/test-board-headless.js` fails two checks that came with this branch. It expects the hide pill to
have two segments and finds three (the new `show stopped`), and a terminated pinned card's right-click menu offers no
dismiss action within 15 s.

## 41eff3e0: Merge say-when-done-idle

What changed: a say sent with `when: done` to a session that is already idle is typed in at once, not held until a
turn ends that never comes.

1. Let a card finish its turn and sit idle at its prompt. From another card, say to it with `when: done`. How long
   until the text appeared in its terminal? Did it arrive at once?
2. Say to a card in the middle of a long turn with `when: done`. Did it wait for the turn to end, then arrive? Did the
   peek show "queued" while it waited, and did the label clear after delivery?
3. Interrupt a card so its turn ends oddly, wait for it to read idle on the board, then say to it with `when: done`.
   Did the message arrive at once? Did anything held before the interrupt arrive too?

Result (1): pass, run by the agent: `atrium tell @dotfiles ... --when done` to the idle card answered "typed into the terminal and sent", and dotfiles replied "ok" at once.
Result (2): pass, "pass. i don't have time to wait". The agent sent `--when done` to the busy card on sg3 and got "queued until that session's turn ends". His peek screenshot showed "working, running PowerShell" and the chatter line "27s ago, queued". Delivery at turn end and the label clearing were not watched.
Result (3): pass on his word, not run: "pass i'm sure it'll be fine"

## b68f1b94: Merge runner-settings-no-agent-view

What changed: cards that atrium launches run Claude Code with `leftArrowOpensAgents: false` and `disableAgentView:
true`, so Left on an empty prompt no longer sends the conversation into the agent view.

1. Launch a fresh card from the board. Wait until its prompt is empty. Press Left. Did anything happen? Did the
   conversation stay where it was?
2. Press Left three more times, then Ctrl+C once. Did the card stay up the whole time, with no agent view and no quit
   prompt?

Result (1-3): skip. "ugh we made a feature for this? ... claude itsefl supports turning this off so - maybe a dumb idea" and "i've turned it off so... i udnno if our test would work". His own Claude Code settings already turn it off, so a test can't tell atrium's launch setting apart from his. On a throwaway Haiku card: "seems to work but who knows".
3. Open a card that was launched before this build (a resumed one). Press Left on an empty prompt. Did the agent view
   still open there? Do you care, or does restarting the card fix it?

## dac34184: Merge merge-flow-impl (on branch claude/merge-flow-0016, not yet on claude/main)

What changed: the merge flow's state lives on the hub: who holds each role, each backlog item's stage, which checks
passed on which sha of main, and which room runs which sha. The recorded merger may move `claude/main` by
fast-forward only. A clint fetch and push round is an epoch, and the hub replays what landed in between so nothing is
lost. There is no board screen for it yet. Everything is the `atrium_flow` tool. Source: docs/design/merge-flow.md.
Needs a hub deploy: migrations 0015_merge_flow and 0016_flow_epoch.

1. Ask a card to run `atrium_flow role set merger <card>` and then `role get merger`. Did it name that card? After a
   hub restart, did it still name it? When that card is closed, does `role get` say `holder: gone` and not pick a
   new one by itself?
2. From the merger card, push a fast-forward of `claude/main` to the hub. Did it land? Then push something that is not
   a fast-forward. Was it refused with a sentence you understood?
3. From a non-merger card, push to `claude/main`. Was it refused, and did the refusal say who the merger is?
4. Look at one backlog item with `atrium_flow item get <id>` after its branch is merged into claude/main. Did it say
   `merged` without anyone reporting it? Does the answer read in words you would use?
5. Tell atrium you fetched claude/main (`epoch fetched <sha>`), land one more commit on claude/main, then re-sign
   and push as usual and run `epoch_close`. Did the commit landed in between survive? Was the old tip still reachable
   at `refs/atrium/epochs/<n>`? How many steps did you do by hand, and which one felt wrong?
6. Open the design doc (docs/design/merge-flow.md). Is there a stage, role or step in it that you would not
   recognise from how you work? Which follow-up (deploy actuator, sanity gate, branch cleanup, board view) do you want
   first?

Known gap: the full `internal/gitsync` integration run hangs (8 min timeout, same as t-gitsync-full-run-hangs). Only
`-run 'Flow|Epoch|Receive'` passes (26s). `TestReposIsOpenLikeGrowlsAndExactlyShaped` in internal/link fails on
claude/main too, from repos-page-fast: it expects an empty repo list and gets `{"building":true,"repos":[]}`.

Result: skip, not yet on claude/main (as the brief says).

## git-url-integration-branch (merge of 5e9a70ee)

1. From a room worker, call atrium_git_url {repo: dovholuknf/atrium, branch: claude/main}.
2. Does it answer found, source hub, readOnly? Does the URL fetch?

Result: pass for the answer, fetch not tested. The agent called it: state "found", source "hub", readOnly true, sha 307fc2c7 (the hub's claude/main has moved past the deployed 3af76d87), url http://127.0.0.1:7777/git/hub/github/dovholuknf/atrium.git. The fetch was blocked by the agent's hook. This worktree's `hub` remote points at :7787 and not at this room's forwarder on :7777, so the hook refuses both `git fetch hub` and `git push hub`.

## typed-line-phantom (merge of a829d917)

1. Paste a big block into a card through the board and submit it. Let the turn end.
2. Does the gate line show no "N chars on your line"? Does a say to that card arrive?

Result: pass. clint pasted a big block (about 120 lines) into this card and the turn ended. The agent had dotfiles say "ping" to it, and "[atrium] dotfiles-41800 says: ping" arrived. clint did not report the gate line separately. He has never seen the phantom "N chars". New finding: the card's "details ^" pill ("this card's details") is missing from a popped-out window and needs to be there: "right now i'm popped out and that makes me realize the [details pill] is NOT on a popped window and it needs to be". The gate line is missing from the popped-out window too: "same is true for the gate. i checked it and then popped this bqack out and it's missing here".

## workers-never-auto-removed (fed92a18, merge of 02b28057)

1. Open Settings. Does merged_cull_grace show default "off"?
2. Merge a worker's branch. Does the worker's card stay (no auto-removal)?

Result (1): pass by API, not on the board. clint: "and where is that???". The setting is not in the board's Settings UI (no board js mentions it). The agent read this room's settings API: "merged_cull_grace":"off", "merged_cull_grace_default":"off". On the settings UI: "the whole settings ui... it needs a fucking overhaul too. it's a disaster and hodgepodge of things we just never use/finished right now...."
Result (2): skip, "skipo this i'm not merging anything"

## close-leaves-worktree (merge of d21ac7e7)

1. Close a PR card whose worktree holds an ignored node_modules. Is the folder gone, and does the freed size include it?
2. Does its pulls row read "closed <ago>" with no buttons, and leave the "open" filter?
3. With show stopped on, does the card still leave the terminals tab?

Result: skip, it needs a PR card with a worktree to close, which only an agent can set up (and the open-link route is refused on this clone without his yes).

## provision-one-gate (merge of 5c7ec583)

1. Run `pwsh -File scripts/room-gate.ps1 sg3 -Check`. Does it report one gate and no todo?
2. On a room with both gates, does it show "two permission gates" instead of "1 missing"?

Result (1): fail (minor), run by the agent. One gate: "atrium's hook --event permission is in PreToolUse", no legacy entry, the listener answers, done ok. But there is one todo: "script todo would delete ~/.claude/hooks/atrium-perm-hook.ps1" (the old hook script is still on disk on sg3, unregistered).
Result (2): skip, no room with both gates to hand.

## no-turn-end-nudge (merge of 6900f99c)

1. Let a worker end a turn without reporting. Is nothing typed into its terminal?
2. Does its launcher get one "fyi: ... ended its turn without reporting"?

Result: skip, needs a launched worker that ends its turn silently, which only an agent can set up.

## held-notices-visible (merge of af2e7f1c)

1. Hold notices for a card. Does its terminals row show a chip with the count?
2. Click the chip. Does "held notices" open with who, what and when?
3. Call atrium_done where the launcher holds notices. Does it answer launcher_told "held"?

Result (1-2): pass, "i saw the held notice already. it's always been visible though it used to be a little email?"
Result (3): skip, agent-only.

## wake-say-delivers (merge of fd64b032)

1. Say with wake to a parked card. Does it resume once, with nothing typed on the startup screen?
2. Does every held message arrive as one turn that the agent acts on?

Result: skip, needs a parked card and a cold-start wake, agent setup.

## gitsync-full-run-hangs (merge of 54d9db5c)

1. Run `go test -tags integration ./internal/gitsync/ -count=1`. Does it finish in about 200s instead of hanging?
2. Known flake: TestMainIsRefreshedAtMostOncePerFreshFor fails on a loaded box (3 of 4 runs here).

## room-agent-pack (merge of 337d8a1c)

1. Provision or set up a room. Does the agent pack reach it (room setup shows no missing pack)?

Result: skip, needs a room provision.

## close-one-dialog (merge of 84df6337)

1. Close a card. Does one dialog show progress and then the result, instead of several dialogs?
2. Force a close failure. Does the dialog say what failed?

Result (1): fail, "does not work that's a bug still". Caveat: the agent closed the throwaway card from the CLI when clint said "consider this done", so his try may have come after it was gone.
Result (2): skip

## peek-delivered-not-queued (merge of 7062cc11)

1. Say to a busy card, then peek it. Does the peek show the message as delivered, not queued, once typed?

## new-context-clearing (merge of 241d79b1)

1. Run a new context on a card. Does it get past the "clearing" step instead of sticking there?

## background-wait (merge of 80ff9a8e)

1. Launch a worker that starts a background command and has nothing else to do. Does it end its turn instead of waiting?

## queued-say-lost (merge of e6c9b4de)

1. Say to a card in the turn a new-context cycle cuts. After the wake, does the say arrive (handed back), not lost?

## repos-test-building (merge of d95b36f5)

Test only: `go test -tags integration ./internal/link/ -run Repos` passes (TestReposIsOpenLikeGrowlsAndExactlyShaped).

## terminals-ended-cards (merge of 0312a3fc)

1. Open the Terminals tab with ended cards. Do they group together with a one-click resume?
Known gap: scripts/check-board.sh flags native title tooltips in js/backlog.js and js/heldnotices.js:65.

## secret-name-suffixes (merge of 05341287)

1. Store a doc secret named "name.", "name " or "name:stream". Does the file land under the plain name on Windows?

## agent-loop-followups (merge of fc628d4b)

Test only: `go test -tags integration ./internal/daemon/ -run 'Finish|End|Done|Blocked|Hold'` passes.

## lean-agents-builtins (merge of ef86f1b7)

1. Launch a lean worker with a short lean_agents list. Does it start only those agents (others denied)?

## details-hover-text (merge of f63cdbdf)

1. Hover the card details pill on a plain, a dark and a light card, in a few skins. Does the label stay readable (terminal text colour, no fill)?
Merge note: one conflict in scripts/test-board-headless.js (the HEADLESS_ONLY map), kept both closeOneDialog and detailsHover.

## timeline-dots (merge of e992bd52)

1. Open a repo's branch timeline on the Repos page. Are the rings centred on the line?
Known gap: headless section hubRepos fails "a branch age has no exact-time tooltip" on the base too.

## clear-mark-escape (merge of 1c822b72)

1. Run /clear on a card whose runner splits an escape sequence across reads. Is the clear mark still placed once, with no stray characters?

## settle-race-tests (merge of f00779c3)

A launch settles on the runner it started. Tests: daemon Launch|Ledger|Settle|FailedStart and link Events|Everywhere|Scoped|Stream pass.

## macos-suite-red (merge of ef025645)

On macOS (a room with a long state dir path), does the pty host socket come up under a private /tmp dir? Tests: ptyhost and daemon Keepalive pass here.

## child-cards-colour (merge of 1a761a7c)

1. Launch a child card from a repo-coloured card in a repo the child has no colour for. Does the child take its launcher's repo colour?

## main-refreshed-flaky (merge of 258ece31)

Test only: gitsync TestMainIsRefreshedAtMostOncePerFreshFor freezes the clock; run the full gitsync package again to see the flake gone.

## subagent-parent-theme (merge of cced19a7)

1. Launch a child card whose repo has no colour entry, from a launcher with an explicit theme. Does the child wear the launcher's colour and theme?

## two-window-size (merge of e859bcc5, plus gofmt commit)

1. Open one card in two windows of different sizes. Does the terminal take the size of the window last focused or typed in?
2. Close one window. Does the terminal return to the remaining window's size?
3. Is the gap above the terminal gone (u-terminal-top-gap was folded in)?

## clear-mark-ring (merge of f6dbb4ff)

1. Run /clear on a card, then restart the room so the pty host is re-adopted. Is the clear mark still at its place in the scrollback?

## hub-gets-snapshot (merge of 4ff444a8, plus two follow-up commits)

1. Open a change request for the first time after a hub start. Does "reading the push log" turn into the pushed line within a couple of seconds, without any event?
2. Open the deploy-ready pill while the hub's pass runs. Does it ask again shortly and then show the answer?
Known gap: headless deployReady "the detail does not open" fails on the base too.

## md-open-rendered (merge of d8bd6ddb)

1. Click a markdown link to a .md file in a terminal. Does its menu offer "open rendered" with an out-arrow icon first?
2. Click a link to another file type. Does it still offer "open in a tab" with the icon?

## alias-chip-copy (merge of e65c9732)

1. Hover an @handle chip on a card row and in a terminal header. Is there no tooltip, and a copy icon after it?
2. Click the icon. Is @handle on the clipboard, a check flashed, and no toast shown?

## room-hub-tools (merge of 8cb7a0b4, plus hubAskPath fix)

1. From a room worker, call atrium_backlog and atrium_reports. Do they answer from the hub?
2. Ask the hub for an encoded path (/_hub/backlog/%2e%2e/rooms) through the relay. Is it refused?

## details-side-by-side (merge of 1a559008)

1. Open a card's details popover in a short terminal window. Do the panels sit side by side when stacked would be too tall?
2. Make the window taller. Do they go back to stacked?

## idle-park-cog (merge of 064f2578, plus saveIdlePark fix)

1. Open a room's cog. Is the idle-park setting there, and does it show that room's value?
2. Change it and save. Does the box reload from that room's settings (not another machine's)?

## alias-default-branch (merge of 276d9e8f, plus rejoin fix)

1. Start a session in a git worktree on branch claude/u-foo and join the board. Does the card get alias u-foo?
2. Clear that card's alias, then join again from the same session. Does the alias stay empty?

## say-log-view phase 1 (merge of ed489a92, plus cut-note fix)

1. Open a card's details and its chatter section. Do says to and from the card list newest first, 20 to a page?
2. Open a long say. Does it end "[cut: the say was N characters]" only when text was cut (emoji text is not a false cut)?

## Jobs 55-57 (2026-10-10)
- 55 tabs-order-hide: topNav (fixed test to skip the arrange button), tabsOrder, bootClean ok.
- 56 graceful-second-stop: gofmt, vet clean; daemon Wrap|Restart and cli Restart|Wrap integration tests ok.
- 57 reader-width-skins: readWidthSkin, bootClean, check-skins.sh ok.
