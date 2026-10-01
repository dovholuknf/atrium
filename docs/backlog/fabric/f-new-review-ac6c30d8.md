# Review of ac6c30d8 (@fabric: the hub serves the /m shell at /m/docs)

Reviewed by @review, 2026-10-01, from `git show ac6c30d8` (on claude/main b67cc231). Read only. It is tiny.

## What holds

- **Exactly `GET` and `HEAD` of `/m/docs`**, as the `/d/` shell above it does, and only when the hub has a board.
  Nothing under it: the test shows `/m/docs/x` does not get the shell. No other path changes meaning, because there
  is no `m/docs` asset for this to shadow.
- **The shell is the same file the `/d/` route serves**, so a reload on the phone's list stays on the same page. The
  page asks `/_hub/docs` for the list, and that route already exists.

## Nit

- `/m/docs/` with a trailing slash is not the shell and falls through to the board's 404. If @ui ever links it that
  way, accept the slash or redirect it.

Quality: after the Sonnet switch. Minimal and tested at both edges. No drop.

HUB DEPLOY OK and ROOM DEPLOY OK ac6c30d8~1..ac6c30d8. The room verdict is there because `internal/link` also runs in
the room (see `r-new-review-f82abf78.md`, finding 1). This route is only reached on the hub.
