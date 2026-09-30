# r-new-review-3c354ed4. Fix for the 54794900 review: a write never guesses past a quiet room

Status: open, one low. Filed by @review 2026-09-30. Read-only review of 3c354ed4 (merge of `claude/r-review-fixes`),
the fix for finding 1 and the low in `r-new-review-54794900.md`. Owned by @runtime.

The medium is closed. A POST, PUT, PATCH or DELETE by bare name with any room quiet answers 409, naming the match it
found and each quiet room as `name@room (not answering)` (`internal/link/cardroute.go:172-187`). A miss with a room
quiet is a 409 too, not a 404, which is right, since the card may be on the quiet room. `name@room` still goes
straight through. The test in `cardname_test.go` covers both. The low is closed as well: a segment not shaped like an
id is looked up as a name first, and `roomHolding` runs for it only after no list carries it.

## 1. Low. A terminal attach is a GET, so it still guesses

`write` is every method except GET and HEAD (`cardroute.go:176`). A websocket upgrade is a GET, and an attach
carries keystrokes. `GET /v1/tasks/rnd/attach` with the room holding the `rnd` somebody meant quiet, and another room
holding a live `rnd`, opens the other card's terminal. What is typed next goes to the wrong session, which is the case
the fix exists for. The board attaches by id, so this is a script or a typed URL.

Fix: count `Upgrade: websocket` as a write in that line.

## 2. Info. The id rule is now "UUID-shaped or it is a name first"

`looksLikeCardID` accepts only the 36-character UUID a room mints today. An id of any other shape now pays for a name
lookup on every room before `roomHolding`. Every card on sg4 is UUID-shaped, so this costs nothing today. It is
listed because an id format change would turn every board request into a name lookup with no error to say so.

## Tests

`go test ./internal/link/ -count=1 -run 'Name|Card|Quiet|Handle|Place|Attach|Resolve'` passes (22s), including the
new quiet-write case and the repro updated to a UUID-shaped id.
