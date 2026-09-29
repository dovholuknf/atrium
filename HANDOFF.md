# HANDOFF: sa32, a say's lifecycle on record (backlog-2 item 32)

Read `BRIEF.md` first, then `docs/say-lifecycle-design.md` (the design, done).

## State

- **Design:** `docs/say-lifecycle-design.md` is written and reviewed. Mercurius session `s_wSTYc1pOQBOh`, round 1:
  `ready_to_build`, no concerns. One advisory (A1, `lapsed` not documented) fixed and recorded. No more rounds needed.
  Design deviations from what was built, still to fix in the doc: `sent` state is never written (a row is born
  `queued`/`delivered`/`held`/etc, the CHECK still allows `sent`), `ask`/`answer` doors write no row (an `answer`
  only settles owed replies), `lapsed` and `relay_id` are columns.
- **Migration:** `0069_say` taken, at the END of the slice in `internal/store/schema.go`. Tell @runtime.
- **Build:** `go vet ./internal/daemon` FAILS on one thing only: `internal/daemon/peers.go:332` calls `d.sayAcross`
  without the new trailing `reply bool` argument. Nothing else known to be broken, but daemon has not compiled yet
  since the edits, so expect a few more errors after this one.
- **Tests:** `go test ./internal/store -run TestSayRecord -count=1` passes (3 tests in `say_record_test.go`).
  No daemon tests yet.

## Built

- `internal/store/say.go` (new): table access. `RecordSay` (adopts a delivery that raced ahead), `SayQueuedAs`,
  `SayDeliveredAs`, `SayHeldAs`, `SayRelayed`, `SaysFor`, `SayByID`, `AnswerSaysFrom`, `RepliesOwed`,
  `NoteContextReset`, `LapseSaysFor`, `SweepSays`. Constants for states, channels, caps.
- `internal/store/messages.go`: `MarkDelivered` also calls `markSaysDelivered`, so all three delivery paths
  (permission hook, stop hook, pendinginject) update the row with no daemon change.
- `internal/store/schema.go`: `0069_say`.
- `internal/daemon/saylog.go` (new): `sayTrace` on the inner request context, `recordSay`, `sayRecordFor`,
  `saySettled`, `sayLapsed`, `sayReset`, `candidatesFor`, `missSentence`, `writeMiss`, `handleTaskSays`.
- `internal/daemon/messages.go` `handleMessage`: `reply` field, records refused / delivered-terminal / queued,
  settles owed replies, answers with `say` and `via`.
- `internal/daemon/relay.go`: `sayIn.Reply`, `localTargetVia` (handle|alias|card), `handleSay` uses `writeMiss`
  and passes the trace, `sayAcross` takes `reply` and records held/handed/unconfirmed/delivered, `drainOnce` and
  `giveUpRelay` call `SayRelayed`.

## Left, in order

1. Fix `peers.go:332` (`sayAcross(..., in.When, in.Reply)`), add `Reply bool json:"reply"` to `handleTell`'s
   input, then `go vet ./internal/daemon` until clean.
2. `handleTell` (peers.go): on miss use `d.writeMiss(w, from, in.To, "tell", text, in.When, in.Reply)` (change
   `resolvePeer`'s miss branch, which `handleAnswer` also uses, to write the candidates sentence). On delivery record
   a row: needs the queue row id, so add `deliverPeerWhenID` returning `(typed bool, msgID string, err)` and make
   `deliverPeerWhen` a wrapper. Typed => delivered/terminal, queued => `MessageID`. Call `d.saySettled(from, target)`.
   `handleAnswer` (help.go) also calls `d.saySettled(from, target)` after delivery.
3. `session.go`: in `case "compact"` call `d.sayReset(task.ID, "compact")`. In `case "end"` when
   `!EndsTheSession(in.Reason)` and reason is `clear`, call `d.sayReset(task.ID, "clear")`. When a card ends for
   real (same `end` case, after `SetStatus`), call `d.sayLapsed(task.ID)`.
4. `reaper.go` ~line 233: beside `SweepDispatch`, call `d.st.SweepSays()` and log when n > 0.
5. Register `GET /v1/tasks/{id}/says` -> `d.handleTaskSays`. Look at how `s.Say` is wired in `internal/api/api.go`
   (function fields on `Server`, filled in by the daemon). Also add `RepliesOwed int json:"replies_owed,omitempty"`
   to the task view in `api.go` beside `AsksOpen` (~line 706), filled from `st.RepliesOwed()` the way `AsksOpen` is.
6. MCP, `internal/cli/control_peers.go`: `SayInput.Reply` (`reply,omitempty`, jsonschema: "true when you need an
   answer, not just a delivery"), send it in the `/v1/say` body, read `say` and `via` from the answer into
   `SayOutput` (`Say string json:"say,omitempty"`, `Via`). `TaskInput.Says bool`, `TaskOutput.Says []TaskSay`
   (id, direction sent|received, other end, via, state, channel, timestamps, preview, reply_wanted, replied_at,
   reset_kind), filled from `/v1/tasks/{id}/says`. Note `ask()` only surfaces `error`, and the new miss sentence
   carries the candidates, which is the point. The old-room fallback `sayByCard` needs no change.
7. Daemon tests (`go test ./internal/daemon -run 'Say' -count=1`, clear `ATRIUM_LOCATION`, `ATRIUM_DEBUG_INPUTLAG`):
   miss lists candidates and queues nothing; `@alias` says record `via: alias`; queued then hook-delivered row
   becomes `delivered/hook`; `reply:true` counts in `RepliesOwed` and a say back settles it; compact/clear stamps
   `reset_kind`. Look at existing `relay_test.go` / `peers` tests for the harness.
8. Fix the design doc deviations listed above.
9. `docs/changes/32.md` (format in `docs/changes/README.md`: `## Changelog` entry then `## Test plan` with
   `@LETTER@`). Short status line under item 32 in `docs/backlog-2.md`. Do NOT touch `CHANGELOG.md` or
   `docs/test-plan.md`.
10. Merge `claude/main` in (rule 77c), re-run targeted tests, commit, `atrium_report done` with the sha.

## For the final report to @runtime

- Migration name: `0069_say`.
- Cross-room, what `internal/link` / `internal/hub*` would need (not changed): a say id on the relay request and a
  delivery receipt coming back, so the sender's row moves from `handed` to `delivered` and the two rooms' rows are
  one row. Today this room records held/handed/unconfirmed and the hub's one word at handoff.
- Open Question for clint (already in the design): tell a session when it clears with a reply owed.
- No board chip: `replies_owed` is in the task JSON only.
- Finding: `GetByWireName` matches ended cards, so an exact handle of a dead card is refused as ended even if a live
  card holds it as an alias. Resolution is exact only (handle, live alias, card id). `atrium_say` used to show only
  `no session called X` on a miss, with no candidates.
- `.mercurius/` directory may be untracked in the worktree: do not commit it.
