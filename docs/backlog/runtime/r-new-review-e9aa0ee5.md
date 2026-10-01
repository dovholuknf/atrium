# Review of r-fyi-kind b9babc02 + 37601f18 + fa5cf5d6 at e9aa0ee5 (@runtime: fyi and needs, held count, stuck wake)

Reviewed by @review, 2026-10-01, from `git diff b9babc02~1 fa5cf5d6` and the branch tip e9aa0ee5 (claude/main merged).
Room side and hub side. This is factory-shape (b) and (c). @runtime asked for the hardest read on four points, and
each is answered under "Findings".

## What holds

- **Needs is the default, and any unknown word is needs** (`parseKind`). A misspelt kind costs one turn, never a
  lost message.
- **An fyi is held only by a receiver that holds its notices** (`holdsNotices`). Anything else is delivered as
  before. A say with `reply` is never held, on both `/message` and `/tell`. A blocked or question report is never
  held. A held fyi still counts as the worker's report (`peerSaid`), so it does not trip "ended without a report".
  It does not unpark a parked launcher. Cross-room says ignore `kind`.
- **The held count is cheap.** `toView` asks only for cards that hold notices, the marker is one setting per card,
  and `TimeFormat` is fixed-width with milliseconds, so the text comparison is a time comparison.
- **The read is the card's own.** The hub tool posts `notices-read` only for `atrium_task notices:true` on the
  caller's own card. A failed post does not fail the answer.

## Tests

In a detached worktree at e9aa0ee5, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG` cleared:

- `go vet` on daemon, api, store, link and cli: clean
- `go test -count=1 -run 'Fyi|FYI|Held|Notice|Report|Wake|Stuck|Escalat|Finish|Tell|Say' ./internal/daemon/`: ok
  (35s)
- `go test -count=1 -run 'Held|Notice|Fyi|Report|Say'` on api, store, link and cli: ok

## Findings

### Low

1. **(Your 1) Two report shapes that want action can still be held.** The statuses are done, blocked, question and
   progress.
   - A `progress` report that carries an `ask` is held as fyi. Only blocked and question are forced to needs, and
     `ask` is optional on progress. Force needs whenever `ask` is non-empty.
   - A `done` report sent as fyi to an `atrium:orchestrator` card is held. Done work waits on its launcher's
     acceptance and merge, so a held done stalls the pipeline until the orchestrator next reads its notices.
     `atrium:hold-notices` launchers already held reports, so this changes things only for orchestrator-tagged
     ones. I would force `done` to needs as well and leave fyi to progress reports and says, which are the "LANDED"
     and "review OK" relays the design was about. If you keep it, the tool description should say that a done
     report sent as fyi will not be acted on until it is read.
2. **(Your 2) The read marker can skip a notice that arrives during the read.** `taskHandler` reads the held
   notices, then posts `notices-read`, and `MarkNoticesRead` stamps `now()`. A notice held between the two is
   counted as read without having been seen, and the badge then says 0. Its text is still in the held list on the
   next read, so nothing is lost, but the count is what tells the orchestrator to read at all. Send the `at` of the
   newest notice returned with the post, and set the marker to the later of that and the stored value.
3. **(Your 3) The stuck wake fires with every silent stop. It is not a no-op, and it is a change against today.**
   `escalationStep` reaches 2 at 2 minutes, and `SilentStopNotifyAfter` is 2 minutes, so the wake is typed at the
   same moment as the first held silent-stop notice. Today an orchestrator that holds notices is never woken by a
   silent stop. With this commit it is woken by every one that lasts 2 minutes, which is the churn (b) and (c) set
   out to remove. A long tool waits 22 minutes (20 plus step 2), which is about right. Make the silent-stop wake
   count from the notice: wake when `elapsed >= SilentStopNotifyAfter + EscalationBackoff[wakeStep-1]` (4
   minutes), or use step 4 (10 minutes) so a worker that answers its notice within minutes never wakes the
   orchestrator.

### Answered, no change

- **(Your 4) Gating of `POST /v1/tasks/{id}/notices-read`.** It is a board write on the room, behind the same
  cross-origin check as the others, and it is not on the guest allowlist. It is not operator configuration, so
  `LocalOperator` is not the right gate. Any caller can clear any card's count, which is the same trust as
  approving a permission. The tool limits itself to the caller's own card.

Quality: after the Sonnet switch. It is careful where the contract is (default needs, reply never held, report
accounting kept), and the worker raised (3) itself. The misses are the edges of the rule: progress with an ask,
done to an orchestrator, and a read marker that is not tied to what was read.

HUB DEPLOY OK and ROOM DEPLOY OK e9aa0ee5. Lows 1 and 3 should follow before the orchestrator relies on fyi.
