# Review of r-exit-on-report 5b0fb4b1..ef3ba730 (@runtime: a spawned card that reports done exits by itself)

Reviewed by @review, 2026-10-01, from `git diff 5b0fb4b1..ef3ba730` (9683dd5f, ef3ba730) and a scratch worktree at
ef3ba730. Room side.

Tests at ef3ba730: `go vet ./internal/daemon/` passes. `go test -count=3 -run 'Report|Finish|Exit|Launch'
./internal/daemon/` ok (118 s).

## What holds

- **The launcher's copy is durable before the exit.** The notice (or the cross-room relay) is written in the same
  `RecordReport` transaction, and the exit goroutine starts after it returns. The first test checks the launcher
  has the report queued at the moment of the exit.
- **The exit is `StopRunner`**, the same one `POST /v1/tasks/{id}/exit` uses, so the card stays `done` with its
  recap and no worktree or branch is touched.
- **Question, blocked, progress and `needs-input` do not exit.** Directors, the orchestrator and unlaunched cards
  do not either. A failed exit is logged, and the report has already landed.

## Findings

### 1. MEDIUM: a done report that carries an ask exits the card

`exitsOnReport` looks at the status only. `validReport` accepts `ask` on a `done` report, `finish` appends it to the
recap as "needs: ...", and the comment above `heldFYI` in finish.go says "one carrying an ask wants [an answer]
too". The acceptance line "A report with a question leaves the card running" covers it. But the card is stopped
5 s later, so the launcher's answer goes to a parked card, which a say refuses without `wake`.

PROVEN with a scratch test (not committed): `launchedPair`, `watchExits`, then `finishWith(FinishRequest{Status:
ReportDone, NoCommit: "x", Ask: "merge it or should I split the commit?"})` and `expectNoExit`. It fails with "exit
asked for <worker id>, want none".

Fix: return false from `exitsOnReport` when `strings.TrimSpace(in.Ask) != ""` (pass the request, not just the
status), and add that case to `TestAQuestionProgressOrBlockedReportLeavesTheCardRunning`.

### 2. LOW: a runner the operator is attached to and typing in is stopped under them

A human who opened a worker's terminal and is talking to it gets the runner stopped 5 s after the worker reports
done, mid-sentence. The delay is fixed and nothing checks whether a human touched the card. Either skip the exit
when the card saw operator input in the last few minutes (the input path in attach.go is where that is counted),
or say in the spec that this is accepted.

### 3. LOW: a resident card launched by another session, with no director tag, is exited on its first done

Only `atrium:director` and `atrium:orchestrator` are exempt. A standing session that an agent launched (a merger,
an interviewer, a persona) reports done at the end of each job and would be exited after the first one. If such
cards exist, they need a tag (an `atrium:resident`, or reuse an existing one) that this exempts. If none do, say so
in the spec.

## Verdict

HOLD 5b0fb4b1..ef3ba730 for finding 1. Re-read 5b0fb4b1..<tip>, then room-ok.

Quality: after the Sonnet switch, no drop seen in the parts built. The ordering against the launcher's notice was
thought through and tested. The miss is the second-order one again: "done" was taken from the status field alone
when the same file already says an ask changes what a done report means.
