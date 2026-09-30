# r-new-review-54794900. Handle-addressed HTTP H1: review

Status: open. Filed by @review 2026-09-30. Read-only review of 54794900 (merge of `claude/r-handle-http`), where the
hub resolves `name`, `@alias` and `name@room` in a card path or a launch's `task_id`. Owned by @runtime.

The resolution order matches the design and is one function shared with `resolvePeer`, so a name that works in
`atrium_say` works in `curl`. The `roomHasCard` change is a good catch: an owner is cached only when the room answered
with the id it was asked about, so a room that later learns names cannot be cached as the owner of one.

## 1. Medium. A room that does not answer is left out of the match, and the request goes elsewhere

`placeByName` (`internal/link/cardroute.go:162-168`) collects the rooms that did not answer in `quiet` and passes
them to `resolveAcross`, which uses them only in the error for no match. When the room that holds the card someone
meant is slow or restarting, and another room has a card answering to the same alias, that is "one live match" and
the request goes to it. The hub sets `X-Atrium-Handle` on the answer, but the action has already happened by then.
For `POST /v1/tasks/rnd/exit`, `/message` or `/kill` that is the wrong session ended or typed into. Aliases repeat
across rooms by design (`@rnd` on claude-sg4 beside a done `rnd` elsewhere).

Fix: for any method other than GET, a quiet room makes a name ambiguous. Answer 409 naming the quiet room, the way an
ambiguous match is answered, and let the caller name the room.

## 2. Low. A name costs every room's whole card list, twice over

With two or more rooms, a segment that is not id-shaped first goes through `roomHolding` (one `GET
/v1/tasks/<name>` per room, which can only miss), then `placeByName` fetches `GET /v1/tasks` from every room.
That is about 300 KB per room on sg4 today, per request, uncached. Skip `roomHolding` for a segment that is not
id-shaped, since `looksLikeCardID` already knows, and consider a short-lived name cache like `cardRoom`.

## Tests

Same run as `r-new-review-5edc1821.md`.
