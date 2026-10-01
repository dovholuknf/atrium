## Design note: hub documents D2, the views

For @rnd, before anything is built. From `docs/rnd/hub-documents-design.md` and the D1 contract `docs/backlog/fabric/hub-documents-api.md`. Branch `claude/u-m-docs`, section `mDocs`. The test plan steps are added to this file when the views exist.

## One renderer, two homes

- New `m/js/docs.js` holds everything D2 draws: the list, the document view, the history, the diff and the upload. `/m` loads it, and the board's `index.html` loads it the same way it already loads `m/js/compose.js`. There is no second markdown path: text goes through `md.js`, raster images and downloads through the same blob rules as `viewer.js`.
- It talks only to `/_hub/docs`, with plain same-origin `fetch`. It sets no `Origin` and no header of ours, and it never uses `no-cors`.
- Every title, name, handle and error sentence is escaped. Nothing from a document is ever put in the page as HTML.

## Phone (`/m`)

- **Routes.** The shell reads its own path. `/d/<slug>` is the newest version and `/d/<slug>@<n>` is version n, split on the last `@`. `/m/docs` is the list. A document opens as a sheet over the shell the way a card does: it pushes a history entry, Back returns to where it came from, and a reload on `/d/...` opens it directly.
- **Entry.** Home gets one `documents` row, and a card's view gets the `published N documents` line (below). A `/d/<slug>` link in a reply, report or recap opens in the app instead of navigating away: `md.js` already renders the relative link, and a click handler on the thread turns a same-origin `/d/` anchor into the sheet. Anything else keeps today's rules.
- **List.** Rows are title, the latest version's kind, version count, age and the ORIGIN of the latest version (`you at the machine`, `uploaded over the share`, or the card's handle). A title filter sits at the top and is sent as `?q=` after a short wait, so the hub does the match. A `deleted` chip lists the tombstones, each with a restore button. The footer says `usage.bytes of usage.cap`. Compact layout, text size follows `--m-fs`, 40px targets.
- **Upload.** An `upload` button opens the picker (images and any file). It is multipart, one request, with an XHR progress line like the composer's. The title field is optional. Refusals show the hub's own sentence by status: 413 over the cap, 422 with the rule named, 503 publishing off, 507 store full. Only when `settings.operator` is true does a 422 offer `upload anyway`, which sends `override=1`. Otherwise there is no such button, because a non-operator override is a 403.
- **Document view.** Header with the title (tap to rename, `POST .../title`), a `v3 of 3` control, and one line per version: origin, time, size. Body by `kind`:
  - `markdown` and `text`: `md.js`, text kept in the `--m-fs` size. Plain text is shown as text, not parsed as markdown.
  - `image`: png, jpeg, gif and webp only, checked by mime AND by the first bytes, drawn from a blob URL. Anything else, SVG included, falls to download.
  - `diff`: text, with added and removed lines coloured.
  - `other`, and any version with `missing` or `purged`: a download button for `other`, and for the others a sentence (`the bytes are missing`, `purged by the operator`) with no body.
  - A tombstoned document shows `deleted on <date> by <who>` and a restore button. Restoring a document with a purged version says that the operator must do it.
- **History and diff.** The history is the list of versions, newest first. For text kinds a `compare` control picks two versions, fetches both raw, and diffs them on the client by lines. A diff is refused over 5000 lines or 400 KB with a sentence, so a big file cannot freeze the phone. Origin is always shown beside `by`, and only `local` is ever worded as the operator.
- **`published N documents`.** On a card's view, one small line under the header. It comes from `GET /_hub/docs?card=<room~id>`, asked once when the card opens and again when its `output_at` moves, never on a timer. Tapping it opens the list filtered to that card. Zero shows nothing.

## Board (`index.html`)

- **A `documents` tab** behind the header chevron with the other lists. It is the same list as the phone's: title filter, upload, the `deleted` chip, the usage footer. Rows open the document in a panel over the board, drawn by `docs.js`, so the view is the phone's view at a wider size. A direct `/d/...` link on a desktop still gets the `/m/` shell, as the contract says.
- **The card line.** The card's detail (its menu and drawer) gets the same `published N documents` line, fetched when it is opened. Not on every row of the stack: that would be one `?card=` request per row on every poll.
- **Not in D2.** Purge, the caps, the publishing switch and the largest-documents list belong to the gear and stay operator-only, so they are left for whoever owns the gear. D2 reads `settings` only for `operator` and for the usage line.

## Tests: section `mDocs`

Against a mock of the `/_hub/docs` routes in the headless server, at 390 and 412 on `/m`, and once on the board:

- list newest first, title filter sends `q`, `deleted` chip lists tombstones and restores one
- multipart upload sends `file` and `title` only, shows the progress and the answer's link, and each refusal status shows its sentence, with `upload anyway` only for an operator
- every kind: markdown, text, a diff, a png, an SVG (download only, never rendered), `other`, a `missing` and a `purged` version, a tombstoned document
- a script in a title, a name and a markdown body does not run, and a `javascript:` link is dropped
- `/d/slug@2` opens version 2, Back leaves, a reload on the address opens it, a `/d/` link in a reply opens the sheet
- history, `compare` of two versions with coloured lines, and the refusal past the line cap
- `published N documents` on a card, with its count and its filtered list
- the pinch size scales the views, no sideways scroll, targets at least 40px, the mFollow and mStickBottom rules are untouched

## Questions

1. Is a document sheet over the board, with the `/m/` shell only for a direct `/d/` link, the right reading of "one link works on the phone and the desktop"?
2. The `published N documents` line costs one request per opened card. Acceptable, or should the hub add a count to the task row?
3. Is a client diff capped at 5000 lines and 400 KB the right bound, or should the hub diff?

## What changed from the note, after @rnd's nine changes

- `/m/docs` is a real address the hub serves (no trailing slash), so the list is built as an address.
- The documents door, the tab and the card line are hidden on any failure of `GET /_hub/docs/settings`, with no toast, since a room's own board has no hub documents.
- The document view has delete (asked twice), restore and new version (`.../versions`, file only). Every non-2xx shows the hub's own `error` sentence, with `(rule)` added on a 422, and no fixed list of statuses.
- Text over 1 MiB is shown cut at 1 MiB as text with `download the rest`. A dropped cut character is handled as in the file viewer. Images are typed by their own first bytes, never octet-stream, and their blob URL is revoked when the sheet closes or the version changes.
- A `card` origin links to the card by its card URL, and is plain text when the lookup fails.
- A same-origin `/d/` link anywhere opens the panel, but only when the path matches `^/d/[a-z0-9-]{1,60}(@[1-9][0-9]*)?$` exactly (so `@01`, upper case and deeper paths keep today's rules). Markdown renders such a link, and a bare `/d/slug` in text, as one.
- The compare trims the common start and end of each side, then runs Myers' O((N+M)D), and refuses with one sentence past 2000 changed lines. Each side is read up to 5 MiB. A diff document colours `+` and `-` lines except the `+++` and `---` headers, and `@@` lines are hunk strips.
- The card line is asked once when a card opens and at most once every 30 seconds after that. The board shows it in the card's details popover, not on the stack rows.

## Test plan

## @LETTER@. Hub documents on /m and the board

Screenshots: `docs/backlog/ui/img/u-m-docs/`.

### @LETTER@1. The list
Open /m on a hub with documents. Tap `documents`.
**Expected:** the address is `/m/docs`. Documents are newest first, each with its kind, version count, age and who wrote the latest version. Typing in the filter narrows the list by title. The `deleted` chip lists tombstones, and `restore` brings one back. On a room's own board the button is not there at all.

### @LETTER@2. Upload
Tap `upload`, pick a file, optionally type a title.
**Expected:** `uploading N%`, then `uploaded` with `open it`. A refused upload shows the hub's own sentence, with the rule in brackets for a secret. Only on the operator's own machine is `upload anyway` offered.

### @LETTER@3. One document
Open `/d/<slug>` from the list, from the address bar, or from a link in a reply.
**Expected:** the newest version, with its origin (`you, at the machine`, `uploaded over the share`, or the card's name, linked when the card exists). The version control changes the address to `/d/<slug>@<n>`. History lists every version. Back returns to where it came from. Markdown, text and diffs are drawn, a png is shown, anything else, SVG included, is a download. A version whose bytes are missing or purged says so.

### @LETTER@4. Compare, delete, new version
Choose `compare`, pick two versions.
**Expected:** changed lines coloured with unchanged runs folded. A compare of two unrelated big files says it is too big instead of freezing. `delete` asks again, then shows `deleted on ...` with `restore`. `new version` adds a version and shows the hub's sentence if the document was deleted meanwhile.

### @LETTER@5. On a card
Open a card that published documents.
**Expected:** `published N documents` under its name. Tapping it lists just that card's documents. On the board, the same line is in the card's details popover, and the `documents` tab behind the header chevron opens the same panel.
