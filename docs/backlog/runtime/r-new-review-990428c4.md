# Review: r-launch-stuck 990428c4

Range `8bfdcdb1..990428c4`, one commit on `claude/r-launch-stuck`. It adds:
- `internal/daemon/launchstuck.go`, which holds the three new escalation sources (launch-idle, launch-prompt and
  terminal-menu) and the `Prompt` field;
- the director rule and the never-heard rule in `stoppedSilently`;
- `sessionSpoke` on the session hook;
- `Agent` in `permSkipTools`;
- tests, the changelog and a test-plan section.

Commits on m1mini are unsigned.

Verdict: **hold** for the room, on M1. A footer that a card only quoted on screen reads as a real menu. The launcher is
then told the card is "STUCK at a menu", and that report hides a real silent stop. Everything else reads right, and 17
of 21 mutants die.

## How it was checked

- I read the diff in full. I also read the callers it relies on:
  - `notifyLauncher` and its `RecordNotice` dedup;
  - `watchWorkers`;
  - every `act.set` and `act.forget` path, to check that "heard" only ever comes from a hook and ends with the runner;
  - who can set `atrium:director`.
- Two probe tests in a scratch worktree at the tip. They are quoted under M1.
- The gates, run on the merge onto landing 110c224a with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset:
  - cli passes, and vet is clean;
  - daemon fails only on the reds that were already there: the hostterm socket set, the keepalive fork and
    `TestNoTestHereCanReachALiveRoom`;
  - gofmt flags only `internal/daemon/fyi_test.go` and `cmd/ptyhost-spike/pipe_windows.go`, and this branch changes
    neither.
- Mutants:

  | Mutant | Result |
  |---|---|
  | `neverHeard` dropped from `stoppedSilently` | caught (`TestACardNoHookHasHeardIsLaunchIdleNeverSilentStop`) |
  | The director exemption dropped | caught (three director tests) |
  | terminal-menu does not wait for a quiet pty | caught (`TestTerminalMenuNeedsQuietAndAMenu`, `TestBusyCardIsNeverRendered`) |
  | launch-prompt does not wait for a quiet pty | caught (`TestLaunchPromptNeedsQuiet`) |
  | launch-idle for every card, not only those with a launcher | caught |
  | launch-idle with no grace | caught |
  | No model-switch kind | caught |
  | No memo | caught |
  | `sessionSpoke` dropped | caught, by `TestASessionStartHookCountsAsHeard` and the older `TestAFirstTurnWithoutAReportIsASilentStop` |
  | `stuckNow` skips `launchStuck` | caught |
  | Cards with no launcher skip `launchStuck` | caught |
  | `Agent` dropped from `permSkipTools` | caught (`TestPermSkipsBothSubagentToolNames`) |
  | The `dialogOpen` skip dropped | caught (`TestAPendingPermissionIsNotAMenu`) |
  | The `hasPendingPermission` skip dropped | **survived** (L1) |
  | The `StatusNeedsPermission` skip dropped | **survived** (L1) |
  | The footer needs only "to navigate" | **survived** (part of M1) |
  | `scans.forgetExcept` dropped | survived. It is a memory leak only, and has no test. |
  | `delete(a.sessions, ...)` dropped from `forget` | survived (L2) |

- **The merge onto landing 110c224a is clean, but the letter is wrong.** The section is lettered IJ, and IJ is
  reserved for r-owed-answers, which has a fix out. r-git-url-brief has IK and r-move-m1 has IM, so this one gets
  **IL**.

## Your five points

1. **The behaviour change. Right, and it hides no real silent stop.**
   - *Never heard.* "Heard" means a tool or stop hook recorded after the runner started, or any hook activity in
     memory. Only hooks set it: the `act.set` and `turnResumed` callers are all hook paths. `act.forget` runs when
     the runner exits, so a resume starts unheard.
     - A card with no hook since its runner started has no turn end of its own to be a silent stop.
     - A card whose hooks are broken, and that sits quiet, now says launch-idle ("no activity since launch, N
       min"). That is the more accurate report.
   - *Directors.* The rule now covers any director, not only one with workers out. That matches r-hold-notices: every
     turn a director ends after the orchestrator prompts it read as a silent stop.
     - A director that really does leave a question from its launcher unanswered is no longer flagged by this
       check. W6 (r-owed-answers) is the mechanism for that debt, and it is not landed yet. So until W6 lands, an
       unanswered director shows nothing. Say so in the changelog.
   - *Can a card tag itself director?* Yes, but that is not new. `PATCH /v1/tasks/{id}` sets tags, and a card could
     already add `atrium:director` to dodge the merged-cull and the idle rules, and drop `origin:agent` (item 40).
     This change makes the dodge cover silent-stop with no workers out as well. That belongs to the tag trust model,
     not to this item (L3).
2. **Reading the screen. See M1.**
   - **Dedup.** `notifyLauncher` keys on the source and `Since`. For launch-prompt and terminal-menu, `Since` is the
     pty's last output time. So the launcher hears once for each distinct stuck screen, and once for each turn if
     the screen keeps coming back. Launch-idle is keyed on the start plus the grace, so it is told once per run.
   - **What text goes out.** The menu title is any one line of the screen, at most one row wide, and it is put
     into the launcher's notice in atrium's voice. A card can already `atrium_say` to its launcher, so this gives it
     no new reach. But it can now make atrium itself say anything after "STUCK at a menu:".
   - **The launch-prompt matches.** They read the whole screen, but only before the first hook. By then the screen
     is only claude's own start-up, so a card cannot fake those.
3. **Timings and the busy-pty skip. Right.**
   - The busy skip is one atomic read, and a quiet card is rendered once for each last-output time.
   - Launch-idle needs 60 s since the start and 30 s of quiet, so a slow start that is still drawing is left alone.
   - `sessionSpoke` on the session hook makes a card heard as soon as claude starts, so the session hook does not
     race launch-prompt.
4. **B++: `Agent` beside `Task`. Safe.** Skipping means the permission hook returns no decision for that tool. Spawning
   a subagent does nothing by itself, and each tool the subagent then calls passes through the same hook. `Task` was
   already skipped, and this only follows Claude Code's rename.
5. **The rewritten tests. Honest.**
   - Both director tests now assert the new rule, with no notice and no STUCK mark.
   - `TestUntaggedLauncherSessionStillOwesAndIsSilent` shows that the same card without the tag still trips. So the
     exemption is the tag and nothing wider.
   - `TestWorkerSilentStopUnchanged` and `TestAFirstTurnWithoutAReportIsASilentStop` still pass unchanged.

## Medium

### M1: a footer quoted on screen reads as a menu, tells the launcher, and hides a real silent stop

`readFrame` searches every line of the rendered screen, from the bottom up, for a line with "enter to select" and "to
navigate". The line does not have to be the screen's last line, or be below claude's input box.

A card that prints the footer as text is read as stuck at a menu once it goes quiet for 30 s. Ways it can print it:
- running `grep`, `cat` or `git diff` on this item's own changelog, test plan or tests (the frames in
  `launchstuck_test.go`);
- reading any Claude Code doc;
- echoing it.

In this repository, the first of those is everyday work.

Probe: a heard agent-launched card. The screen shows a Bash call, the output line
`17: " Enter to select · ↑/↓ to navigate · Esc" +`, a closing line, then claude's input box and `? for shortcuts`.
The pty has been quiet for 60 s.
- `launchStuck` returns `terminal-menu`, with `prompt` `other`, Count 1, and the text "stuck-worker is STUCK at a menu:
  ⏺ Bash(grep -n navigate launchstuck_test.go)".
- With the turn ended and a report owed, `stoppedSilently` is true. But `stuckNow` returns that terminal-menu, not
  the silent stop, because `launchStuck` is checked first. So the launcher gets "STUCK at a menu" in place of "stopped
  without reporting".

A line too wide for one row wraps the two phrases onto separate rows and escapes the check, so the false positive
depends on the width.

Fix:
- **Anchor the footer.** It has to be the last line of the screen, or within the last two lines. A real Claude Code
  menu draws its footer there, and while a menu is open there is no input box below it.
- **Require the whole footer string.** Match `Enter to select · ↑/↓ to navigate` in one piece, not the two halves.
- **Let a real silent stop win.** Check `stoppedSilently` before terminal-menu. Or skip terminal-menu once a turn has
  ended after the last prompt: a menu waiting for an answer is not a finished turn.
- Add the probe frame above as a test that expects nil.

## Lows

- **L1: two of the three "already on the card" skips have no test.** Dropping the `hasPendingPermission` skip, or the
  `StatusNeedsPermission` one, still passes. Add one case for each.
- **L2: `sessions` cleared on `forget` has no test.** If the delete were dropped, a resumed card would count as heard
  from its last run and never show launch-idle or launch-prompt. Add a test for a resume after an exit.
- **L3: the director exemption now covers every silent stop.**
  - Until W6 lands, a director that leaves its launcher's question unanswered shows nothing. Say so in the
    changelog.
  - The tag can be set by `PATCH` from a card, as before. If the tag trust model is ever tightened, this rule goes
    with it.
- **L4: "update available" is a banner, not a prompt.**
  - Before the first hook, a screen that carries claude's update notice is reported as "STUCK at the update prompt".
  - If the real cause is something else, for example a launch prompt that never arrived, the label misleads.
  - Call it launch-idle with the banner noted, or match only an update dialog that waits for an answer.

Atrium-Verdict: hold 8bfdcdb1..990428c4
Quality: a well-built watchdog, with a cheap memo, honest derivation each tick and good mutation coverage. The one hold
is that it trusts any line of the screen to be claude's own menu.

## Re-read: c65bec80

Range `8bfdcdb1..c65bec80`. The fix commit is c65bec80, on top of 990428c4.

Closed:
- **M1.**
  - **The footer is anchored.** `readFrame` looks for the whole `menuFooter` ("enter to select · ↑/↓ to navigate")
    on the last two non-blank lines only.
  - **A silent stop wins.** `launchStuck` returns nil for terminal-menu when `stoppedSilently` is true.
  - **Probes rerun:**
    - **The original probe frame** (`quotedFooterFrame`): `readFrame` reports no menu and `launchStuck` returns nil.
      With the turn ended and a report owed, `stuckNow` now gives `silent-stop`.
    - **A `cat` whose last output line is the whole footer, then the input box and `? for shortcuts`:** no menu. The
      same frame without the status line is also no menu, because the input box takes the last two lines.
  - **Mutants**, each failing a test:
    - dropping the anchor;
    - widening it to three lines;
    - matching on either half of the footer;
    - dropping the silent-stop check.
- **L1.** `TestAPendingPermissionOrNeedsPermissionIsNotAMenu` covers both skips. Dropping either one fails it. On
  this store `RecordPermission` works, so the test runs and is not skipped.
- **L2.** `TestAResumeAfterAnExitStartsUnheard` fails if `forget` stops deleting `sessions`.
- **L3.** The changelog says what a director shows until W6 lands.
- **L4.** "update available" counts as a prompt only on a menu. Treating a banner as a prompt fails
  `TestAnUpdateBannerIsNotAPromptButAnUpdateDialogIs`.

Left over, not a hold:
- **N1: a footer inside the input box itself.** One case still reads as a menu: the footer's own text sits on the
  last line inside the input box, with no status line drawn below it. The agent never types there. Only a person, or
  a prompt typed in but never sent, can leave it there, and only for 30 s of quiet. Let it be.

Test plan: the section is lettered IL, and it is the only IL.

Merge onto claude/landing a9307bc7:
- `docs/test-plan.md` conflicts at the end of the file, against IO and IP. Keeping both sides resolves it.
- The Go code merges cleanly, `go build ./...` passes, and store, cli and link pass.
- daemon shows only the known reds.

Atrium-Verdict: room-ok 8bfdcdb1..c65bec80
Quality: a tight fix. Each point from the hold now has a test that fails when the fix is reverted, and the
silent-stop ordering is handled where it belongs.
