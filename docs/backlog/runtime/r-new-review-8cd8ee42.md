# Review: r-stdio-launch-lineage 8cd8ee42

Range `47a73233..8cd8ee42`, one commit on `claude/r-stdio-launch-lineage`. Its background is
docs/backlog/runtime/r-new-report-no-launcher.md. Commits on m1mini are unsigned.

The stdio `atrium_launch` (`internal/cli/control_peers.go` `launchHandler`) now sends the lineage the hub's launch
already sent:
- `spawned_by`, from `ATRIUM_AGENT_NAME`;
- `origin:agent`;
- `atrium:subagent`, unless the requested tags carry director or subagent;
- the report line.

The hub's `launchOnRoom` now calls the same two helpers, `link.AgentLaunchTags` and `link.WithReportLine`.

Verdict: **OK** for room and hub. The hub half is a pure refactor. The Lows can follow.

## How it was checked

- I read the diff, the daemon's `/v1/launch` lineage path (`launch.go` around `SetLineage` and the `EventPrompted`
  `from_peer`), the hub's `agentOf` and `launchOnRoom`, and every reader of `ATRIUM_AGENT_NAME`.
- Gates, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` unset:
  - gofmt is clean on cli and link;
  - vet is clean;
  - cli passes in full;
  - the link launch, tag, report and control tests pass.
- Mutants:

  | Mutant | Result |
  |---|---|
  | stdio: the report line dropped | caught (`...RecordsItsLauncherAndIsAWorker`, `...WithABriefStillEndsWithTheReportLine`) |
  | stdio: raw tags | caught (`...RecordsItsLauncherAndIsAWorker`, `...OfADirectorIsNotMarkedASubagent`) |
  | stdio: `spawned_by` dropped | caught (`...RecordsItsLauncherAndIsAWorker`) |
  | stdio: a blank name sent as `spawned_by` | caught (`TestAHandRunStdioLaunchIsNotAttributedToABlankName`) |
  | helper: the director check removed | caught, in cli and link |
  | helper: no origin tag | caught (`TestLaunchStampsTheOriginTag`, `TestARelayedLaunch...`) |
  | helper: the subagent check removed, so the tag is doubled | caught in cli, survives in link. Harmless either way |
  | helper: the caller's slice appended in place | survives. Equivalent today, since no caller reuses the slice |

- The merge onto landing d5876dc1 is clean. The merged tree builds and vets. The test plan has one `## @LETTER@`
  section, after II.

## The five points

1. **`spawned_by` from `ATRIUM_AGENT_NAME`.**
   - The daemon sets the variable at launch, to the card's handle (`launch.go` `atrium` map), and `InheritedTaint`
     strips any inherited copy.
   - A card can still change its own environment, so the name is self-asserted.
   - That is not weaker than the hub. The hub's `agentOf` reads the `AgentHeader`, which the card's own MCP
     registration sends, so it is self-asserted the same way.
   - It grants nothing new. A card can already POST `/v1/launch` on its room's loopback API with any `spawned_by`,
     which is exactly what this handler does from the card's process.
   - What the name decides is all lineage: where reports go, who the prompt is owed to, and whom stdio `atrium_exit`
     lets exit the worker. Naming another card as the launcher only sends that card the worker's reports. The launch
     cap is the hub's, and it is not on this path.
   - The daemon resolves the handle with `GetByWireName(Qualify(by))`. That is the same form the hub sends for a
     same-room caller.
2. **Tags.**
   - `AgentLaunchTags` takes the **requested** tags (`in.Tags`), not the caller's own. The doc comment says
     "the caller's own", which is wrong (L1).
   - So a caller can ask for `atrium:director`, or any tag, and get a director that is not a subagent and is never
     culled. That is what the hub has always allowed, and stdio could send raw tags before this change too. Nothing
     is widened.
   - The hub's behaviour is unchanged. The helper is the same two expressions, moved, and the hub's tests still pass.
3. **The report line.**
   - It is added once, after `briefPrompt`, so a brief launch reads "Read BRIEF.md ... Then: <task>" followed by the
     report line.
   - It is not doubled. A stdio launch never goes through the hub, and a cross-room stdio launch goes to
     `launchAcrossRoom` before this code runs.
   - There is no resume on this path: `LaunchInput` has no resume field.
   - **With r-git-url-brief (b5e61438).** The stdio path writes BRIEF.md itself (cli `writeBrief`) and sends no
     `brief`. The daemon therefore takes b5e61438's no-brief branch and appends the `atrium_git_url` line to the
     prompt, after the report line. There is no doubling, and every stdio launch with a prompt gets both lines. A
     stdio brief file never gets the git line, though (L2).
4. **The hub launch cap is untouched.** No cap code is in the diff, and the hub path changes only by the refactor. The
   cap still counts running `atrium:subagent` cards on the hub, and stdio launches were never counted there.
5. **r-owed-answers (49488e18).**
   - From now on, a stdio-launched card has a launcher and a `from_peer` on its prompt event. So it can owe an answer,
     and its reports reach its launcher.
   - That is the intent, and it is the root cause named in r-new-report-no-launcher.
   - Cards launched before this change keep `@human` and owe nothing. Lineage is written only at launch.
   - When W6 lands, expect owed items for stdio workers that were silent before. The changelog says the lineage of
     every future stdio launch changes. Name the owed effect there too (L3).

## Lows

- **L1: the doc comment says "the caller's own" tags.** `AgentLaunchTags` takes the tags the launch asks for. Say
  "the requested tags", so nobody reads the director check as a check on who is asking.
- **L2: a stdio brief file has no `atrium_git_url` line once r-git-url-brief lands.**
  - The line rides on the prompt instead, so the card still sees it.
  - The design says "every card brief keeps the line", and a card that re-reads BRIEF.md will not find it there.
  - Either have the cli `writeBrief` call the same append, or send the brief to the daemon the way the hub does. For
    @runtime, whichever lands second.
- **L3: the changelog should name the owed effect.** Add one line saying that a stdio-launched worker now has a
  launcher and can be shown as owing an answer, so a new owed item is not taken for a regression.
- **L4: `WithReportLine` does not check whether the line is already there.** A prompt that already ends with the
  report line gets it twice. That is the same on the hub. r-git-url-brief's `withGitURLLine` checks before appending,
  and this one could too.

## Test-plan letter

`@LETTER@` becomes **IN**. That follows II, IJ (r-owed-answers), IK (r-git-url-brief), IL (r-launch-stuck, pending)
and IM (r-move-m1).

Atrium-Verdict: room-ok 47a73233..8cd8ee42
Atrium-Verdict: hub-ok 47a73233..8cd8ee42
Quality: a small fix to the real root cause, with one shared helper so the two paths cannot drift. 8 of 10 mutants are
caught, and the two that survive are harmless.
