# r-new-review-a1abed4f. Test isolation, and a constraint never halts

Status: one low. Filed by @review 2026-09-30. Read-only review of a1abed4f (merge of `claude/r-test-isolation`), the
fix for the lows in `r-new-review-f4466ea0.md` and the 002 leak. Owned by @runtime.

## Closed

- **A constraint never halts the store.** `guard` returns any `SQLITE_CONSTRAINT` family error to the caller, told
  apart by `sqlite.Error.Code()&0xff`, not by message text (`internal/store/store.go`). `constraint_test.go` pins it.
  Low 1 of f4466ea0 is closed.
- **A test's children cannot reach the live room.** `tellWhereIAm` puts `ATRIUM_LOCATION` in a runner's and a shell's
  environment when the daemon was given its own `LocationFile` (`internal/daemon/whereami.go`, `launch.go`,
  `shell.go`). `LocationFile` is set only by `atrium daemon --location-file` and by `atrium preview`
  (`internal/cli/cli.go:179`, `preview.go:186`). A live `atrium room` never sets it, so live runners are unchanged. The
  shell tests run with a temp home, a dead hub URL and `cmd /d`, and test daemons stop the runners they started. Low 3
  of f4466ea0 is closed as well.
- **Low 2 of f4466ea0 was mine to withdraw.** `reopenResume` resumes `t.ResumeID` (`internal/daemon/reopen.go`), which
  is exactly the key `bootResumes` is filled with. I misread `resumeIDFor` as a different source. It returns the same
  field.

## 1. Low. The event insert calls every constraint "no card"

`appendEventOn` now maps any constraint failure to `no card %q to record %s on: sql.ErrNoRows`. The foreign key is the
case it was written for. A `CHECK` failure is a constraint too, and the event table's `CHECK` lists the allowed kinds.
Somebody who adds an event kind and forgets to widen that `CHECK`, which `internal/store/CLAUDE.md` says is a table
rebuild, gets every event of the new kind reported as "no card" and treated as benign by any caller that ignores
`ErrNoRows`. Before, it halted, which was loud. Map only `SQLITE_CONSTRAINT_FOREIGNKEY` (787) to `ErrNoRows` there, and
let the rest come back as themselves.

## Tests

`go test ./internal/store/` and `./internal/daemon/ -run
'Reopen|Restart|Exit|Fixture|Resume|Asked|Held|Park|Idle|Shell|Source|Where|Location' -timeout 25m` both pass
(137s and 74s).

## Verdict

**ROOM DEPLOY OK** for a1abed4f. The low does not block it.
