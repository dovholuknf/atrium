# Review: f-growl-reminders e4b7653c..fef7530e (m1mini, 2026-10-02): ROOM DEPLOY OK

One commit on claude/f-growl-reminders, the hub half of "no growler reminders, but a remind me" (clint 10:10, a
pause exception). The board half is u-remind-me, already OK. Unsigned.

## The tick loop

- **The ladder is read once per tick** (`g.ladder()`): the defaults merged with the `growl_ladder` setting, where an
  unreadable value means the default. That is one `Setting` read every 30 s, a negligible cost.
- **A reason off the ladder rings once.** The raise is untouched (the board sees it and the bell logs it), and the
  tick `continue`s before the backoff, so it never reminds or phones again on its own.
- **A due snooze** (`st.Wake()`) is marked `woke`. It phones once, ladder or not, through the same `phone` helper,
  which still takes only permission and question. Wake resets `raised_at` and `reminders`, so a woken permission
  restarts its ladder from step 0, with no due step at that tick, which means no double phone. A woken question off
  the ladder phones once and then stops.
- **Nothing can stop a permission reminding by accident.** The default keeps `permission`, `halt` and
  `deploy-hold` on. Only an explicit PUT turns one off, and a PUT is the operator's at the hub machine.

## The derive change

`growlEnded` skips a card whose status is `done` or `dead`, so `sync` ends its growlers as resolved. **The comment is
wrong, but the rule is safe.** `done` is not "session over": `atrium_report` sets `done` on a live session
(daemon/finish.go:180), and idle parking takes `done` cards (idletick.go:135). The rule holds anyway:
- a question or a block puts the card in `needs-input` (finish.go:183-187);
- a pending permission puts it in `needs-permission`;
- so a `done` card's last word is a done report, and an older question growler on it is stale.

Reword the comment to say "a card whose last report was done, or that is dead". It does not hold the verdict.

## The PUT

`edge.LocalOperator` only, like `/_hub/hosts`, with a 4 KiB body, a 400 on an unknown reason (checked before
anything is saved), and an audit as `growl-ladder-set`. The merge starts from the current ladder and sets only the
reasons named, so one reason does not reset the others. GET is open to whoever reaches the hub board, which is
harmless.

## Tests, mutation-checked

`go vet ./internal/link` is clean, and the `TestGrowl` tests pass. Each mutation was run against the 8 new tests:
- **Ladder skip turned off:** 4 fail, including OffNeverReminds, DefaultKeepsHaltAndHold and
  SettingChangeTakesEffect.
- **Ended-card skip removed:** `TestGrowlEndedCardEndsItsGrowlers` fails.
- **Woken phone removed:** both woken tests fail.
- **LocalOperator guard removed:** `TestGrowlLadderEndpoint` fails.
- **PUT merge replaced by an empty map: survives.** See the low.

## Lows

- **No test for "a PUT with one reason must not reset the others".** With the merge removed, a stored partial map
  falls back to the defaults for the unnamed reasons, so a first PUT `{"permission":false}` and then a second
  `{"question":true}` puts permission back on. Add that two-PUT case.
- **The PUT is a read-modify-write with no lock**, so two concurrent PUTs can lose one. It is local only and rare.
  Take `g.mu` around it, or accept it.
- **The stored map holds every reason after the first PUT,** so a later change to `growlLadderDefault` no longer
  reaches that hub. Store only what was named, or say in the comment that a PUT pins all of them.

Verdict: ROOM DEPLOY OK e4b7653c..fef7530e, hub-ok and room-ok (internal/link).

Quality: careful. The woken path reuses the phone helper rather than copying it, the ladder is read where it is used,
and the tests catch what matters. The misses are a wrong comment and one untested merge.
