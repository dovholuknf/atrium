# r-new-review-c184ae8c. Fix for the 0b4e3f0d review: every resume holder checked

Status: open, ONE HIGH. Do not ship the room half of c184ae8c until finding 1 is fixed. Filed by @review 2026-09-30.
Read-only review of c184ae8c (merge of `claude/r-review-fixes`), the fix for `r-new-review-0b4e3f0d.md`. Owned by
@runtime.

The three findings it answers are closed. `ResumeHolders` returns every other holder and `resumeHeld` refuses on any
live one. `bootResumes` gives each resume id on the reopen list to the first card that has it, and the rest start
fresh. And the refusal is written on the card. That last change is what finding 1 is about.

## 1. High. A fixture's first start can halt the room

`resumeHeld` now calls `noteResume(taskID, ...)` (`internal/daemon/fixtures.go`), which appends a `notified` event
on `taskID`. `fixtureResume` passes `onto` as that id, and `onto` is empty on a fixture's first run when no card in its
directory can be adopted (`startFixture`, `onto := f.TaskID`). It is also a deleted card's id when the fixture's card
was removed. The `event` table has `task_id ... REFERENCES task(id)` with `PRAGMA foreign_keys = ON`, and
`Store.guard` halts the store on any error that is not busy or locked.

Proved at c184ae8c with a throwaway store test, deleted afterwards:

```
s.AppendEvent("", EventNotified, map[string]any{"by": ResumeRefused})
err=store is halted: constraint failed: FOREIGN KEY constraint failed (787) halted=true
```

The path is the shared checkout, which is the case this whole change is about: a new fixture on
`D:/git/github/dovholuknf/atrium`, resume on, the default `latest` mode. Its newest conversation belongs to a live
card, so `resumeHeld("", id)` refuses and writes an event on card `""`. The room halts, and its agent listener closes,
at boot or when the fixture is started from the fixtures page.

Fix: `noteResume` writes nothing for an empty id, and `resumeHeld` notes the refusal on the card only after `Launch`
has created it, by returning the reason and letting `startFixture` write it on `task.ID`. More generally,
`AppendEvent` on a card that does not exist should be refused before the insert, not reach the guard. A halt is for
storage that failed, not for a caller naming a card that is not there.

A daemon-level test: a fixture with no card and resume on, in a directory whose newest session id is held by a live
card. `startFixtures`, then assert `d.st.Halted()` is false.

## 2. Low. The refusal says `via: reopen` when a fixture made it

`resumeHeld` is shared by `reopenResume` and `fixtureResume` and always writes `"via": "reopen"`. A fixture's
refusal reads on the timeline as a reopen. Pass the caller's name in.

## 3. Low. `bootResumes` is keyed on the stored id, which is not always the id resumed

The pass stores `t.ResumeID` for each card on the reopen list. `reopenResume` resumes what `resumeIDFor` answers,
which can differ from the stored id. A card whose stored id and resumed id differ is not protected by the map. Key it
on what `reopenResume` would return.

## Tests

`go test ./internal/daemon/ -run 'Reopen|Resume|Held|Fixture'` and `./internal/store/` were not rerun for this
review beyond the probe above. The probe is the finding.
