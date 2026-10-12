# One protocol for a worker's finish, report and stop

Status: design, 2026-10-12. Nothing in the plan below is built. Backlog items answered: r-agent-comms-one-protocol (with
r-launch-report-line-contradicts, r-done-report-closes-card, r-expected-end-notices, r-report-is-one-line,
r-worker-knows-commit-is-visible, r-worktree-hub-remote-refused folded in), r-agent-loop,
r-done-never-reached-orchestrator, r-new-context-turn-owes-report, r-worker-reply-reads-as-question, and the three
incidents of 2026-10-12 (a resident minion ended itself, workers could not file cost reports, branch pushes refused for
ownership).

Read first: `docs/runtime/agent-messaging.md` (the queue and the two delivery paths), `docs/how-atrium-works.md` (the
permission chain), `docs/rnd/owed-report-design.md` (what "owes a report" means) and `docs/rnd/accept-verb.md` (the
accept verb, which this design leans on and does not repeat).

Code read: `internal/daemon/launch.go` (`LaunchEndingLine`, `BriefPrompt`, `agentCheckoutRefusal` in `interview.go`),
`internal/link/control_mcp.go` (`reportLine`, the `atrium_say`, `atrium_report`, `atrium_exit`, `atrium_launch`
descriptions, `AgentLaunchTags`), `internal/link/ctl_end.go` (`doneToolDesc`, `blockedToolDesc`, `Told`),
`internal/link/ctlclass.go`, `internal/daemon/finish.go` (`finish`), `internal/daemon/endonce.go`,
`internal/daemon/exitonreport.go`, `internal/daemon/a2a.go` (`notifyLauncher`, `peerSaid`, `silentStop`,
`stoppedSilently`), `internal/daemon/fyi.go`, `internal/daemon/ledger.go` (`ledgerNotice`), `internal/store/ledger.go`
(`queueNotice`), `internal/store/a2a.go` (`promptOwes`), `internal/daemon/contextlog.go`,
`internal/daemon/hubremote.go`, `internal/gitsync/receive_rules.go` (`checkOwner`, `inherits`, `askOwners`),
`internal/gitsync/identity.go` (`pusherFrom`), `internal/gitsync/pushlog.go` (`released`),
`internal/link/backlog_mcp.go` (`atrium_reports`).

## 0. The protocol in one paragraph

A launched worker gets a task, does it, and says exactly one of two things to its launcher: `atrium_done` with a commit
or an artifact and an optional line, or `atrium_blocked` with a reason of up to 50 words. Then it stops. Atrium owns
these words: they live in one constant that atrium appends to every launch prompt and in the two tool descriptions,
and a brief does not restate them. A done or a blocked always reaches the launcher as typed input, never held, and the
tool answers `queued` until it has been typed, never `true` for something that has not landed. Atrium sends nothing to
a worker after it ends, and nothing to the launcher about a worker that ended, was told to stop, or had its context
cycled. A launcher's next say to a stopped worker wakes it and is delivered. A card marked resident hears different
words from the same tools: done and blocked report a task and leave the card open. The branch a worker was launched on
is the worker's to push, whoever pushed it first among its launcher's family.

## 1. What a worker hears today, voice by voice

| Voice | Where | What it says now | Status |
| --- | --- | --- | --- |
| Launch text | `daemon.LaunchEndingLine`, `link.reportLine` (kept equal by `TestTheLaunchLineNamesDoneAndBlockedAndNotAtriumReport`) | end with atrium_done or atrium_blocked, report to the launcher, never ask where to push, a stop is answered "with atrium_done, or with nothing" | shipped in b82b8f92d. The "or with nothing" clause is the one rule left that can still ring the launcher (section 4.3) |
| `atrium_done` | `link.doneToolDesc` | "End your work... THIS IS HOW TO END", sha or artifact, details in REPORT.md | shipped. Tells a resident it ends (the minion incident) |
| `atrium_blocked` | `link.blockedToolDesc` | same, with a 50 word reason | shipped. Same resident problem |
| `atrium_report` | `control_mcp.go` | use only when the launcher asks for this tool | shipped. Kept for `progress` and `question` |
| `atrium_say` | `control_mcp.go`, last paragraph | "IF ANOTHER SESSION LAUNCHED YOU, END WITH atrium_done..." | shipped. Duplicates the launch text, which is fine as long as it reads the same |
| `atrium_exit` | `control_mcp.go` | for a launcher to ask a session to leave | no protocol words in it |
| Nudger | `a2a.go` `silentStop` | removed: nothing is typed into a worker (b7a4d3f9a). The launcher is told once per prompt | shipped |
| Silent-stop notice | `a2a.go` `silentStop`, `stoppedSilently` | "X ended its turn without reporting", once per launcher prompt | shipped, but still fires after a launcher's `needs` stop answered with nothing |
| Context cycle | `newcontext.go`, `contextlog.go` | the continue prompt is recorded as `kind: fyi` so it owes nothing, and lists says already sent | shipped in e5c69b996, tests in `newcontext_debt_integration_test.go` |
| Reply to the launcher | `owed.go` `owedSaid`, `a2a.go` `peerSaid` | a plain say to the launcher settles the debt and opens no owed item | shipped in 07584b6a1 |
| Launcher's say to a finished worker | `messages.go`, `keep.go` | wakes it and delivers | shipped in ec988f2e3. An `fyi` does not wake it (48f58c4f7) |
| Runner-native menu | `terminal-menu` escalation | the launcher is told once, with the menu | shipped in ba020716b |
| Commit visibility | launch text and brief | "your commit is read from your worktree or any hub branch" | shipped in 30fbaf5b1, aa636bd94 |
| Hub remote | `daemon/hubremote.go` | per-worktree hub remote | shipped in 6b3bba6b6 |

So most of r-agent-comms-one-protocol and r-agent-loop has landed piece by piece since 2026-10-07. What is left is
small and is the subject of the plan:

1. The stop rule still has two answers ("atrium_done, or with nothing"), and "nothing" after a `needs` stop opens debt
   and rings the launcher. (section 4.3)
2. `launcher_told: true` means "queued", not "typed". No test proves a done reaches an orchestrator's terminal. (4.2)
3. A resident worker has no way to be told that done does not end it. (4.4)
4. What a done carries versus what `atrium_reports` carries is not written down anywhere a worker reads. (4.5)
5. A worker cannot push the branch it was launched on when its launcher, or an earlier worker on that branch, pushed
   it first. (4.6)

## 2. Decisions

Each decision with its reason. The operator was away, so these are the designer's calls.

**D1. Atrium owns the ending words. One constant, appended once.** `daemon.LaunchEndingLine` stays the single source,
and `link.reportLine` stays equal to it by test. A launcher's brief does not restate the ending. Reason: the t-ci-why
worker got "call atrium_report when you finish" from atrium and "never call atrium_report" from the brief. Three voices
for one moment made the operator the tiebreaker. The `atrium_launch` description gains one sentence telling launchers
not to write ending rules into a brief, because atrium appends them.

**D2. A stop is answered with `atrium_done`, never with nothing.** The ending line drops "or with nothing". A worker
told to stop calls `atrium_done` with the last commit it made (or an artifact). A done whose sha equals the card's
`report_sha` is a repeat: it is recorded, it settles any debt, and no notice goes to the launcher. Reason: "nothing" is
indistinguishable from a silent stop when the launcher sent its stop as `needs` (the default), and a rule that atrium
cannot tell apart from the failure it watches for will ring. One answer for every ending removes the special case. The
repeat is silent because the launcher already has that sha, so a second "done abc123" would be the noise r-agent-loop
asks to remove.

**D3. A launcher's stop sent as `fyi` still needs no answer.** Unchanged (48f58c4f7, `promptKind`). A worker that
answers an fyi stop with nothing owes nothing, and one that answers with a repeat done is silent by D2. Both are fine,
so the worker does not have to know which kind it got.

**D4. A done or a blocked is typed to the launcher, never held, and `launcher_told` says what really happened.** Typing
happens after the report's transaction commits, asynchronously, so `finish` cannot answer "typed". The values become:
`"queued"` when the notice is in the launcher's queue and will be typed at its turn end or carried by its Stop hook,
`"relayed"` when it went to another room's launcher through the relay outbox, and `false` when nobody was told. `true`
is no longer answered by `finish`. Reason: r-done-never-reached-orchestrator asks that `true` mean delivered. A done
cannot wait for an orchestrator's turn to end inside a tool call, so the honest answer is "queued", and the delivery
itself is proven by a test (Phase 3) and recorded on the worker's card when it lands. The worker's instruction is the
same for every value: stop. `"held"` stays in `link.Told` for an older room's answer and is never produced for a done
or a blocked, because `st.HoldNotice` already refuses to hold source `report` (daemon.go).

**D5. Nothing automatic follows an ending.** After a done, a blocked, a repeat done, a context cycle, a restart wake or
a resume, atrium sends no nudge to the worker and no silent-stop notice to the launcher. Only a prompt the launcher
wrote, as `needs`, opens debt (`store.promptOwes`). This is today's rule for everything but the "nothing" stop, which
D2 removes. Reason: r-expected-end-notices and r-new-context-turn-owes-report. Kept as one rule so the next door to
record a prompt cannot forget it.

**D6. A resident card is marked by one tag, `atrium:resident`, and hears different words from the same two tools.**
`atrium_launch` gains `resident: true`, which adds the tag. `atrium:director` and `atrium:orchestrator` imply resident.
For a resident caller the hub serves `atrium_done` and `atrium_blocked` with descriptions that say "report this task,
your card stays open, wait for the next message", the launch ending is `ResidentEndingLine`, `exitsOnReport` never exits
it, and `stoppedSilently` never counts it. Reason: the 2026-10-12 minion ended its own card because the only words it
had said "THIS IS HOW TO END". A separate verb for residents would be a second protocol. The same verbs with an
honest description keep one protocol and fix the words. A tag rather than a new launch field because tags already flow
from the launch to the hub's class lookup (`lookupClass` reads them) and to every daemon check, with no schema change.
`lifetime` stays for `work` (merge-ends-it) and is not overloaded.

**D7. A done carries a pointer and one line. Everything else goes to two other places.** `atrium_done` takes `sha` or
`artifact`, plus an optional `line` of up to 20 words that lands after the sha in the launcher's one line. Details go
in `REPORT.md` in the worktree. Cost and what slowed the run go to `atrium_reports add` with `to: "reports"`, BEFORE the
done, because the done is the last call. Reason: r-report-is-one-line wants the launcher's message to be one line,
and r-agent-loop wants the launcher to evaluate from the commit, not a recap. The work ledger is for the operator's
later reading, not the launcher's next move, so it does not ride on the done. Tokens and wall time are measured by
atrium (`recordUsage` runs in `finish`), so a worker files only what atrium cannot see: the slowest part and what would
have made it faster. Briefs that ask a worker to sum tokens with jq can drop that step once the follow-up in section 6
lands. Until then the brief's ask stands.

**D8. Report to the launcher, never the operator, unless the launcher is the operator.** Unchanged from the shipped
launch line. A blocked worker asks its launcher with `atrium_blocked`, and the launcher decides whether the operator is
needed. Reason: r-agent-comms-one-protocol. The operator is not a worker's tiebreaker.

**D9. Never ask where to push.** Unchanged from the shipped launch line. The worker pushes its own branch to `hub` when
its brief says so and otherwise leaves the commit where it is. Its done names the sha, and the launcher reads it from
the worktree or any hub branch (`validEnd` checks both).

**D10. The card launched on a branch owns that branch among its launcher's family.** A card launched by L on branch B
may push B and takes B over when the push log's owner is, on the same room, L itself or an earlier card that L launched
on B. Any other owner still refuses, as now. Reason: 3 of 4 workers on 2026-10-12 were refused because the orchestrator
(or an earlier worker it launched on the same branch) had pushed the branch first and is live, so `released` never
frees it. The launcher made the worktree and handed it over, so the handover is the launcher's word that the worker owns
it. The rule is computed by the room, which is trusted for its own cards exactly as it is for the `moved_to` chain
today, and the hub honours it only for an owner on the pusher's own room (the same limit `inherits` has).

**D11. The permission chain and resilience guarantees are untouched.** Nothing here adds a step to `onPermRequest`,
and nothing here can stop a session starting, ending a turn or making a tool call. Every new check fails toward today's
behaviour: a lookup that fails serves today's descriptions (classFull or classWorker), a room that sends no kin header
gets today's ownership rule, and a done whose repeat check cannot read the card sends the notice as now.

## 3. The loop, end to end

```
launcher                     atrium                                   worker
   │ atrium_launch(brief, prompt) ─► prompt + LaunchEndingLine ─────────► reads BRIEF.md, works
   │                                 (ResidentEndingLine if resident)
   │                                                                     │ commits
   │                                                                     │ atrium_reports add (cost, slowest part)
   │                                 ◄──────────────────── atrium_done sha line
   │                                 records, settles debt,
   │                                 queues a one-line notice (never held)
   │                                 answers launcher_told: "queued" ───► stops. says nothing more
   │ ◄── typed at turn end: "@w done abc12345: line"
   │                                 not resident: runner exits after exitOnReportDelay
   │ evaluate
   ├── more work: atrium_say(w, "do X") ─► wakes w, delivers ───────────► works, ends with done or blocked again
   ├── stop: atrium_say(w, "stop", fyi) ─► nothing owed ─────────────────► atrium_done same sha (silent) or nothing
   └── accept: atrium_cull / accept ─► exits, removes worktree, prunes, reports bytes (docs/rnd/accept-verb.md)
```

## 4. The open pieces in detail

### 4.1 One text

`LaunchEndingLine` (and `reportLine`) becomes:

```
Before you end your turn, tell your launcher either:
- atrium_done <sha or artifact> [one line] when the work is finished
- atrium_blocked <up to 50 words> when something stops you, such as a question you need answered, a tool you need
  installed, or a permission you need.
Then stop. Say nothing more.
Run tests and builds in the foreground, with a longer command timeout if needed. If a command must run in the
background, wait for it in the same turn. Never end a turn while a command you started is still running.
Report to your launcher, never to the operator, unless your launcher is the operator. Your commit is read from your
worktree or any hub branch, so never ask where to push and never offer a push to origin. A launcher's "stop" is a
valid end: answer it with atrium_done and your last sha.
```

(The constant has no line breaks inside sentences. They are wrapped here for the 120 column rule.)

`ResidentEndingLine` is:

```
You are resident: your card stays open between tasks. When a task is finished, call atrium_done <sha or artifact>
[one line]. When something stops you, call atrium_blocked <up to 50 words>. Either one tells your launcher and leaves
your card open. Then wait for the next message. Never call atrium_exit on yourself.
Run tests and builds in the foreground... (the same two sentences as above)
Report to your launcher, never to the operator, unless your launcher is the operator. Never ask where to push.
```

### 4.2 Typed, not held, and told honestly

`finish` already queues a done or blocked with `WaitTurn` for every launcher, orchestrator included, and
`st.HoldNotice` refuses to hold source `report`. The 2026-10-10 failure predates 6e2b60bac (same day), which moved
reports for holding cards from held to queued. What is missing is (a) a test that the queued notice is then typed into
an orchestrator's terminal, and (b) an answer that does not say `true` for a queue row. Phase 3 does both.

### 4.3 The stop

Today: a launcher sends "stop" as `needs` (the default). The say records a prompt from the launcher, so the worker owes
a report (`promptOwes`). The worker obeys the launch line and answers "with nothing". Its turn ends, `stoppedSilently`
finds a launcher prompt newer than the last report and a turn end after it, and the launcher gets "ended its turn
without reporting". That is the t-ci-why false alarm, still reachable.

After: the line says to answer a stop with `atrium_done` and the last sha. `finish` sees the sha equals
`task.ReportSHA` from the previous done and treats it as a repeat (sets `in.viaSay`, the flag `endEchoesSay` already
uses to record without notifying). The debt is settled by the recorded report, no notice goes out, and `stoppedSilently`
finds `ReportedSince(OwedAt)` true. An artifact repeat is the same test on the artifact path, read from the work item's
outputs. A worker with no commit and no artifact answers a stop with `atrium_blocked "stopped by launcher, no work
made"`, which is a real report and is sent.

### 4.4 Resident

| Check | File, function | Resident behaviour |
| --- | --- | --- |
| tag | `link.AgentLaunchTags` | `resident: true` adds `atrium:resident` and no `atrium:subagent`, as a director gets none |
| tool words | `link/ctlclass.go` `lookupClass` | a caller tagged resident, director or orchestrator gets the resident descriptions |
| launch text | `daemon.BriefPrompt` and `link.WithReportLine` | `ResidentEndingLine` when the launch tags carry resident |
| exit on done | `daemon.exitsOnReport` | never for resident |
| silent stop | `daemon.stoppedSilently` | never for resident, as for director and interview today |
| cull | `atrium_cull` | refused for resident, as for keep-open |

### 4.5 What goes where

| Carrier | Holds | Read by | When |
| --- | --- | --- | --- |
| `atrium_done` | sha or artifact, optional line of up to 20 words | the launcher, as one typed line `@w done abc12345: line` | last call of the turn |
| `atrium_blocked` | up to 50 words: what stops you and what you need | the launcher, as one typed line | last call of the turn |
| `REPORT.md` in the worktree | what the work decided, phases, anything long | the launcher, when it evaluates | before the done |
| `atrium_reports add`, `to: "reports"` | wall time, tokens (until atrium files them), slowest part, what would have helped | the operator, later | before the done |
| `atrium_say` to the launcher | only an answer the launcher asked for mid-task. Settles debt, opens nothing (07584b6a1) | the launcher | any time before the done |

### 4.6 Branch ownership

Today: `gitsync.checkOwner` refuses a push to a branch whose push-log owner is another live card, unless the owner is in
the pusher's `Chain` (its `moved_to` predecessors, which the room's `HubForwarder.Chain` would send but which
`daemon.hubRemote` never sets, so the chain is always the card alone). `released` frees a branch only for a gone, dead,
or seven-day-done owner. A live orchestrator that pushed the branch while preparing the worktree, or an earlier worker
launched on the same branch that is done but not culled, keeps it.

After: the room records the branch a card was launched on (`launch_branch`, read by `agentCheckoutRefusal` already),
and the forwarder sends two more headers on the card's push: `X-Atrium-Card-Branch` (that branch) and
`X-Atrium-Card-Kin` (the launcher's id, then the ids of earlier cards the same launcher launched on that branch, at
most `MaxChain`). The hub's `pusherFrom` reads them into `Pusher.LaunchBranch` and `Pusher.Kin`. `checkOwner` and
`askOwners` treat an owner as inherited when `p.Room == room` and either `inherits(p, room, card)` (today) or
`trimHead(ref) == p.LaunchBranch` and `card` is in `p.Kin`. The release is logged `by: "launched on this branch"`. The
forwarder strips both headers from the client's request first, as it strips the card headers today.

## 5. Implementation plan

Four phases, each landable alone, in this order. Each names files, functions, the change, and the test. Build with
`go build -o build.claude/ ./...`. Integration tests need `-tags integration`.

### Phase 1. One ending text, stop answered by done, repeat done is silent

Files and changes:

1. `internal/daemon/launch.go`, const `LaunchEndingLine`: replace the text with the section 4.1 text (one string, no
   line breaks inside sentences). The first bullet becomes `- atrium_done <sha or artifact> [one line] when the work is
   finished\n`. After the blocked bullet add `"Then stop. Say nothing more.\n"`. The last sentence ends `answer it with
   atrium_done and your last sha.`
2. `internal/link/control_mcp.go`, const `reportLine`: the same text, byte for byte.
3. `internal/link/control_mcp.go`, `atrium_launch` description: after the `brief` paragraph add the sentence
   `"Do not write how to finish into the brief: atrium appends the ending to every launch."`
4. `internal/link/ctl_end.go`: add `Line string` to `doneInput` with jsonschema `"optional: one line, up to 20 words,
   shown after the sha"`. Add `const EndLineMax = 20`. In `EndDoor.Done`, refuse a line over 20 words with
   `fmt.Errorf("the line is %d words, over 20. details go in REPORT.md", n)`, and when present add `"recap": "done " +
   sha + ": " + line` (or artifact) in place of `"done " + sha`.
5. `internal/link/ctl_end.go`, `doneToolDesc`: append `"Answer a launcher's stop with this and your last sha."`
6. `internal/daemon/finish.go`, `finish`: inside `if in.Ended {`, after the `endEchoesSay` check, add:
   `if in.Status == ReportDone && repeatsLastDone(task, in) { in.viaSay = true }`.
   Add the function to `internal/daemon/endonce.go`:
   `func repeatsLastDone(task *store.Task, in FinishRequest) bool` returning true when `strings.TrimSpace(in.SHA) != ""`
   and `task.ReportSHA != ""` and one is a prefix of the other (case folded), or when `in.Artifact != ""` and the
   card's last report recap equals `"done " + strings.TrimSpace(in.Artifact)`. Comment it with D2.

Tests:

- `TestTheLaunchLineNamesDoneAndBlockedAndNotAtriumReport` keeps the two constants equal. Run
  `go test -tags integration ./internal/link -run 'TestTheLaunchLine|TestDone|TestBlocked'`.
- New in `internal/link/ctl_end_integration_test.go`: `TestDoneTakesAnOptionalLineAndRefusesOver20Words`. Calls
  `EndDoor.Done` with a stub `Report` that captures the body. A 5-word line lands in `recap` as `done abc1234: <line>`.
  A 21-word line is refused and `Report` is not called.
- New in `internal/daemon/finish_end_delivery_integration_test.go`: `TestARepeatDoneAfterALauncherStopIsSilent`. Use
  `launchedPair`, `stubCommits(t, true, false)`. Finish done `abc1234` (Ended). Drain the launcher's pending messages.
  Record a launcher `needs` prompt on the worker (as `peerSaid` does through the say route used by
  `TestAFYISayIsQueuedForTurnEndForBothTags`, but kind needs). Finish done `abc1234` again. Assert: no new pending
  message on the launcher, then `stopSilently(t, d, "worker")` leaves no pending and no held notice on the launcher.
- Run: `go test -tags integration ./internal/daemon -run 'TestARepeatDoneAfterALauncherStopIsSilent|TestAnEnding'`.

### Phase 2. Resident marker

Files and changes:

1. `internal/link/control_mcp.go`: add `const ResidentTag = "atrium:resident"` beside `DirectorTag`. Add a field
   `Resident bool` to `launchInput`, json `resident,omitempty`, jsonschema `a long-lived card: atrium_done and
   atrium_blocked report a task and leave it open, and it is never exited or culled for a done`. In `launchHandler`,
   when `in.Resident`, append `ResidentTag` to the caller's tags before `AgentLaunchTags`. In `AgentLaunchTags`, treat
   `ResidentTag` like `DirectorTag` (no `SubagentTag` added).
2. `internal/link/control_mcp.go`: add `const residentReportLine` with the section 4.1 resident text, and
   `ResidentReportLine = residentReportLine`. Change `WithReportLine(prompt string)` to `WithReportLine(prompt string,
   resident bool)` choosing the line, and update its callers (grep `WithReportLine(`).
3. `internal/daemon/launch.go`: add `const ResidentEndingLine` equal to `link.residentReportLine` (a test keeps them
   equal, as for `LaunchEndingLine`). Add `const ResidentTag = "atrium:resident"` in `internal/daemon/exitonreport.go`.
   Add `func isResident(t []string) bool` returning true for `ResidentTag`, `DirectorTag` or `OrchestratorTag`. In the
   launch path that calls `BriefPrompt` (grep `BriefPrompt(` in `launch.go`), call `briefPromptEnding(file, prompt,
   ResidentEndingLine)` when `isResident(req.Tags)`. In `briefPromptEnding`, also trim a trailing `ResidentEndingLine`
   from the prompt, as it trims `LaunchEndingLine`.
4. `internal/daemon/exitonreport.go`, `exitsOnReport`: add `ResidentTag` to the tag list that returns false.
5. `internal/daemon/a2a.go`, `stoppedSilently`: add `|| hasTag(t.Tags, ResidentTag)` to the director and interview
   check.
6. `internal/link/ctlclass.go`: add `classWorkerResident` and `classFullResident` after `classWorker`. `inClass` treats
   `classWorkerResident` as `classWorker`. Add `func residentClass(c ctlClass) bool`. In `lookupClass`, after picking
   the base class, return the resident variant when `hasTag(best.Tags, ResidentTag) || hasTag(best.Tags, DirectorTag) ||
   hasTag(best.Tags, "atrium:orchestrator")`.
7. `internal/link/control_mcp.go`, `handler`: build four servers, one per class, and pick by `c.classOf(r)`. In
   `server`, register `atrium_done` and `atrium_blocked` with `residentDoneToolDesc` and `residentBlockedToolDesc` when
   `residentClass(class)`.
8. `internal/link/ctl_end.go`: add the two descriptions. Done: `"Report that this task is finished. Your card stays
   open: after the call, say nothing more and wait for the next message. Give exactly one of sha or artifact, and an
   optional line of up to 20 words. Details go in REPORT.md."` Blocked: `"Report that something stops this task, in up
   to 50 words. Your card stays open: after the call, say nothing more and wait for the next message."`
9. `internal/link/control_mcp.go`, `atrium_cull` handler: refuse a card tagged `ResidentTag` with the same wording as
   for keep-open (grep `keep-open` in the cull handler).

Tests:

- `internal/link/ctlclass_integration_test.go`: `TestAResidentCallerGetsResidentEndingWords`. Board stub returns a card
  tagged `atrium:subagent`, `atrium:resident`. `tools/list` holds `atrium_done` whose description starts `Report that
  this task is finished`, and does not hold `atrium_cull`. A plain subagent still gets `End your work`.
- `internal/link/relay_kind_integration_test.go`: extend `TestTheLaunchLineNamesDoneAndBlockedAndNotAtriumReport` to
  check `residentReportLine == daemon.ResidentEndingLine`.
- `internal/daemon/finish_end_delivery_integration_test.go`: `TestAResidentDoneNeitherExitsNorRingsOnSilence`. A
  `launchedPair` with the worker tagged `atrium:resident`. `exitsOnReport` on a done is false. After the done, a
  launcher `needs` prompt and `stopSilently`, no silent-stop notice is pending or held on the launcher.
- `internal/daemon/launch_brief_integration_test.go`: `TestAResidentLaunchEndsOnTheResidentLine`. A launch tagged
  `atrium:resident` gets a prompt ending in `ResidentEndingLine` and not containing `LaunchEndingLine`.
- Run: `go test -tags integration ./internal/link -run
  'TestAResidentCaller|TestTheLaunchLine|TestWorkerSeesSix|TestDirectorUntagged'` and `go test -tags integration
  ./internal/daemon -run 'TestAResident'`.

### Phase 3. A done is typed to an orchestrator, and `launcher_told` is honest

Files and changes:

1. `internal/link/ctl_end.go`: add `ToldQueued Told = "queued"` and `ToldRelayed Told = "relayed"`. `UnmarshalJSON`
   maps `"queued"` and `"relayed"` to them and keeps `true` and `"held"`. `Value()` returns the string for the two new
   values. Add `const QueuedNote = "queued for your launcher. it is typed when its turn ends. stop now."`.
2. `internal/daemon/finish.go`, `finish`, where `told` is set: replace with
   `told := any(false)`, then `if res.Notice != nil { told = "queued" }`, `if res.Notice != nil && res.Notice.Held {
   told = "held" }`, `if res.Relayed { told = "relayed" }`. Leave everything else.
3. The hub's `controlMCP.endReport` (grep `endReport` in `internal/link`): set `Note: QueuedNote` when the room's
   answer is `queued`, `HeldNote` when `held` (as now).
4. `internal/daemon/ledger.go`, `ledgerNotice`: no change. `internal/daemon/pendinginject.go`: when a message whose
   `Source` would be a report lands via `terminal`, nothing new is required, because `MarkDelivered` already stamps
   `via`. The test below reads that.
5. Update the existing tests that expect `launcher_told == true` for a queued ending to expect `"queued"`:
   `TestAnEndingIsQueuedForTurnEndOnAHoldNoticesLaunchersCard`,
   `TestAnEndingResumesNoHoldNoticesLauncherAndQueuesForAnOrchestratorOne`, and any other hit of
   `launcher_told"] != true` in `internal/daemon` and `internal/link` (grep it). The cross-room one expects `"relayed"`.

Tests:

- New in `internal/daemon/finish_end_delivery_integration_test.go`:
  `TestADoneIsTypedIntoAnIdleOrchestratorsTerminal`. `holdingPair(t, d, OrchestratorTag)`, then
  `r, f := typedRunner(t, d, launcher.ID)`, set the launcher idle (status needs-input) and open the gate as
  `TestARetryIntoAnOpenGateLandsAndClears` does. Finish done `abc1234` (Ended). Assert the answer is
  `launcher_told == "queued"`. Call `d.pending.attempt(launcher.ID)`. Assert `f.written()` contains `done abc1234`,
  ends with `\r`, and `d.st.PendingMessages(launcher.ID)` is empty. Assert `heldOn(t, d, launcher.ID)` is empty.
- New: `TestADoneToABusyOrchestratorIsCarriedByItsStopHook`. Same pair, gate shut (unsent text). Finish done. Post a
  Stop hook for the launcher (as the `handleStop` tests do). Assert the hook's block reason contains `done abc1234`.
- Run: `go test -tags integration ./internal/daemon -run 'TestADoneIs|TestADoneTo|TestAnEnding'` and
  `go test -tags integration ./internal/link -run 'TestDone|TestBlocked|TestEnd'`.

### Phase 4. The launched card owns its launch branch

Files and changes:

1. `internal/store/schema.go`: add a migration `ALTER TABLE task ADD COLUMN launch_branch TEXT NOT NULL DEFAULT ''`
   following the newest migration's pattern. `internal/store/store.go`, `Task`: add a field `LaunchBranch string`
   with json `launch_branch,omitempty`, and read it wherever `branch` is scanned (grep `branch,` in `tasks.go` selects).
   Add `func (s *Store) SetLaunchBranch(id, branch string) error`.
2. `internal/daemon/interview.go`: split `agentCheckoutRefusal` into `agentCheckoutBranch(cwd) (branch, why string)`
   and keep `agentCheckoutRefusal` as a wrapper. In `launch.go` at the `agentCheckoutRefusal` call, keep the branch and,
   after the card is created, `d.st.SetLaunchBranch(created.ID, branch)`. A scratch launch records nothing.
3. `internal/gitsync/identity.go`: add `HeaderLaunchBranch = "X-Atrium-Card-Branch"` and `HeaderKin =
   "X-Atrium-Card-Kin"`. Add `LaunchBranch string` and `Kin []string` to `Pusher`. In `pusherFrom`, refuse more than one
   value of either (as for the card headers), validate each kin id with `ValidCardID`, cap at `MaxChain`, and fill the
   two fields. A branch that fails `checkRefName("refs/heads/"+b)` is ignored, not refused.
4. `internal/gitsync/hubremote.go`: add `Kin func(card string) (branch string, kin []string)` to `HubForwarder`. In
   `ServeHTTP`, delete both headers from the outgoing request, then set them when `Kin` returns a branch. Add both names
   to the strip list in `internal/gitsync/receive.go` (the loop over `HeaderCard, HeaderChain`).
5. `internal/gitsync/receive_rules.go`: add `func kin(p Pusher, ref, room, card string) bool`: false for the operator or
   another room, true when `trimHead(ref) == p.LaunchBranch` and `card` is in `p.Kin`. In `askOwners`, skip an owner
   for which `kin(...)` is true, as it skips `inherits`. In `checkOwner`, after the `inherits` branch, add the same
   release with `by: "launched on this branch"`.
6. `internal/daemon/hubremote.go`, `hubRemote()`: set `Kin: d.pushKin`. Add `func (d *Daemon) pushKin(card string)
   (string, []string)`: read the card, return `""` when `LaunchBranch` is empty or `SpawnedByID` is empty. Otherwise
   kin is `SpawnedByID`, then each id in `d.st.WorkerIDs(SpawnedByID)` other than `card` whose card has the same
   `LaunchBranch` and an earlier `Created`, newest first, at most `gitsync.MaxChain - 1` of them.

Tests:

- `internal/gitsync/receive_integration_test.go`: `TestALaunchedCardTakesItsLaunchBranchFromItsLauncher`. Owner
  `room1/L` pushes `claude/x`. Pusher `room1/W` with `LaunchBranch: "claude/x"`, `Kin: ["L"]` pushes a fast-forward:
  accepted, the push log names W. `TestKinDoesNotReachAnotherBranchOrRoom`: same kin pushing `claude/y` is refused, and
  an owner on `room2` with the same card id is refused.
- `internal/gitsync/hubremote_integration_test.go`: extend `TestAClientsOwnCardHeadersNeverReachTheHub` with the two
  new headers.
- `internal/daemon` (integration): `TestPushKinNamesTheLauncherAndEarlierSiblingsOnTheSameBranch`. Launcher L, worker
  A on `claude/x` (earlier), worker B on `claude/y`, worker C on `claude/x`. `pushKin(C)` returns `claude/x`, `[L, A]`.
- Run, as one line:
  `go test -tags integration ./internal/gitsync -run
  'TestALaunchedCard|TestKin|TestAClientsOwn|TestAnotherCardCannot|TestTheOwnersSuccessor'`
  and `go test -tags integration ./internal/daemon -run 'TestPushKin'`.

## 6. Not in this plan

- **Atrium files tokens and wall time to the work ledger itself.** `finish` already calls `recordUsage`. A follow-up can
  have `finish` add an `atrium_reports` row for the card's run, so a brief stops asking a worker to sum jsonl usage
  with jq. Not a phase here because it touches the hub's reports store and the four-phase limit is spent on the
  protocol.
- **Accept as one verb.** Designed in `docs/rnd/accept-verb.md`. The loop in section 3 ends there.
- **Orchestrator recaps.** The operator saw each worker report followed by an orchestrator recap. The report is now one
  line and typed at the orchestrator's turn end. Whether the orchestrator recaps is its own brief's choice, not
  atrium's protocol.
- **The `CLAUDE.md` files.** They still say peer messages are never typed (agent-messaging.md "Known gaps"). Edited in
  their own repository, not here.
