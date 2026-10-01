# Review of 12b2ed5a (@rnd: docs/rnd/hub-documents-design.md)

Reviewed by @review, 2026-10-01. Design only, read for what D1 and D2 will be reviewed against.

## What holds

- **The security line.** A document is as readable as the board and no more: no public link, nothing sent to a
  third party, and the overlay rule unchanged. Purging, the caps, switching publishing off, and restoring a purged
  document are operator-only. Delete over the share is a tombstone and can be undone.
- **Raw bytes are always an attachment**, octet-stream with nosniff, as the files endpoint is. HTML and SVG are
  stored as text and never rendered, with a sandboxed iframe left to stage 3. Rendering goes through the reviewed
  `md.js` and `viewer.js`, with no second markdown path.
- **Stage 2 delivers context as a named file**, not as prompt text, under `.atrium/docs` with `.git/info/exclude`.
  That is the peer bus's framing applied to documents, and it is the right call.
- The secret checks are honestly called a speed bump.

## What D1 should spell out

1. **The name refusal runs on the resolved target, not on the path asked for.** `safepath` follows symlinks for
   containment, but a link `notes.md -> .env` inside the card passes containment and passes a name check on
   `notes.md`. Check the name list against the final, resolved path, case-insensitively, since rooms run on Windows.
2. **The content refusal applies to `content` as well as `path`.** An agent can read `.env` and pass its text as
   `content`, which skips checks 1 and 2 by construction. Check 3 should scan both inputs, and uploads from the
   board too.
3. **Record where each version came from, and say it in stage 2.** An upload over the share and an upload at the
   machine both look like "clint" today. Stage 2's launch prompt then tells a model "written by clint", which is
   the most trusted framing, for bytes anyone past the share's password could have added as a new version. Each
   version should store `via: local | share | card <id>` (from `edge.LocalOperator` at upload), and stage 2 should
   name it: "uploaded from the board over the share", not "clint". Pin an attached document to the version that was
   attached, not to the newest, so a later version cannot swap what a card was given.
4. **The slug and the download name are restricted.** A slug is the URL path and is followed by `@<n>`, so allow
   only `[a-z0-9-]`: no `@`, no `/`, no `.`-only names. Make collisions get a suffix. The attachment's
   `Content-Disposition` filename comes from a title someone typed, so encode it per RFC 6266 (`filename*=UTF-8''...`)
   and strip CR and LF.
5. **Uploads and versions over the share use the board's cross-origin protection.** Say that `POST /_hub/docs`
   and its version and tombstone routes sit behind the same `Sec-Fetch-Site` check as every other board write, so a
   page in clint's browser cannot publish into the store. It is probably true by construction, but D1's acceptance
   should test it.

A note, not a change. A share user can fill the 2 GiB cap and refuse every publish, the same lockout shape as the
web-push slot cap. The design already answers it: the refusal names the cap, and the gear lists the largest
documents.

OK to land with 1 to 5 added. None of them changes the shape.
