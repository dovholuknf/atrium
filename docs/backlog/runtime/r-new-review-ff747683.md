# r-new-review-ff747683. A restart brings running cards back: review

Status: open. Filed by @review 2026-09-30. Read-only review of ff747683 (merge of `claude/r-exit-asked`), the fix
for the 12:58 restart that brought nothing back. Owned by @runtime.

The fix is right in shape. `done` never meant "somebody decided", and an asked exit recorded by `StopRunner` does.
The launch lock covers the new `/resume` branch, `ontoRefusal` still refuses a shelved or live card, and
`RestartRunner` writes `launched` after its own `exit-asked`, so a restarted card is not left down. Three things
remain.

## 1. Medium. The exit-asked record can be routed out of the table it is read from

`ExitAsked` (`internal/store/tasks.go:853`) reads the newest `notified` or `launched` event back from the `event`
table. Neither kind is in `dbPinnedKinds` (`internal/store/eventsink.go:93`), which holds only `created` and
`submitted`. So `event_cold_kinds=notified` sends every future `exit-asked` to the cold sink alone, and
`atrium room db compact --drop-kinds notified` deletes the ones already written. `notified` is the obvious
candidate for either, because it is the chattiest kind. After that every card somebody exited comes back on the
next restart, fixtures included, which is the r-new-reopen-resumes-exited-card incident again. Nothing logs it.

Fix: pin `notified` and `launched` in `dbPinnedKinds`, or better, keep the decision as a column on `task`
(`exit_asked_at`, cleared by a launch). One query, no `LIKE` over a payload, and nothing an event setting can reach.

## 2. Medium. Only `StopRunner` records an exit, and the other ways a card ends now come back

`Kill` (`internal/daemon/launch.go:1605`) writes no `exit-asked`. Neither does a person typing `/exit` in the card's
own terminal, nor a runner that dies. Before 0b4e3f0d a fixture whose card was `done` for any of those reasons stayed
down. Since this merge `fixtureEnded` (`internal/daemon/fixtures.go:124`) keys on `exit-asked` alone, so a fixture
somebody terminated, or exited from inside its terminal, starts again at boot. For a fixture in the shared checkout
with the default `latest` resume that is the exact path of the original incident: the newest conversation in
`D:/git/github/dovholuknf/atrium` belongs to a card on another room, and `resumeHeld` cannot see other rooms.

Fix: `Kill` records `exit-asked` too, since terminate is a decision. Decide separately whether a `SessionEnd` with
reason `prompt_input_exit` on a card that was NOT winding down counts as asked. The wind-down knows it is winding
down, so the hook can tell the two apart.

## 3. Low. `/resume` reports a refusal as a server error

The new branch of `handleResume` (`internal/daemon/park.go:254`) answers every `Launch` error with 500. The likely
errors are `ontoRefusal`'s: shelved, or a live runner the supervisor does not own. Those are 409s, and the board shows
a 500 as atrium breaking. Map them the way `api.launch` does.

## Tests

Run in `D:\worktrees\claude\atrium\review` at claude/main, with `ATRIUM_LOCATION` and `ATRIUM_DEBUG_INPUTLAG`
cleared. `./internal/store/` passes. `./internal/daemon/` whole ran past Go's default 10 minute timeout on a loaded
sg4 and was killed mid-migration, which is the package's size and not a failure. The tests covering these two merges,
`-run 'Reopen|Restart|Exit|Fixture|Resume|Ended|Asked|Held|Park|Prompt' -timeout 25m`, pass (113s). None of the three
findings has a test yet.
