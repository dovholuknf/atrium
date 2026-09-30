# r-new-review-befa812b. Card URLs R4: a lent session's readable address

Status: clean. Filed by @review 2026-09-30. Read-only review of befa812b (merge of `claude/r-card-urls-r4`). Owned by
@runtime.

A lent card is now handed out as `<frontend>/room/<room>/<handle>` (`guestPath`, `internal/daemon/overlay_guest.go`),
and the guest listener serves that page, `/alias/<name>`, and the page's `GET /v1/tasks/<name>[@room]` lookup only
when the name is this card. This is the one surface built on an allowlist on purpose, so it was read as a security
change.

## What holds

- **Still an allowlist.** The new block runs after every named case and before the catch-all. It serves only GET and
  HEAD, and it rewrites the path to exactly `/` or exactly `/v1/tasks/<this id>` before handing it to the board. A
  longer path such as `/v1/tasks/<name>/attach` does not match `guestNamed` and falls through to the existing rules,
  so the `?kind=`, `carry` and `link` refusals are untouched.
- **Not an oracle.** A name that resolves to another card and a name that resolves to nothing both get the catch-all's
  exact text, "this link is one terminal. nothing else here is shared.", with the same 403.
- **Another room's name is not read.** `/room/<other>/<name>` and `<name>@<other>` do not match and fall to the 403.
- **The handed-out name does not move.** It is the wire name, never the alias. A renamed alias stops opening the
  card, and the address keeps working, which test-plan row R4.2 checks. A qualified wire name survives, because
  `url.PathEscape` encodes its `/` and `guestNamed` splits the escaped path before unescaping each part.
- **The listener's edge is unchanged:** `edge.Named` with the share's frontend names.

## Tests

`go test ./internal/daemon/ -run 'Guest|Lend|Share'` passes, including the new `guestnames_test.go`.

## Verdict

**ROOM DEPLOY OK** for befa812b.
