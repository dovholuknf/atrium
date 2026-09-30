# Review of the card-audit landing (u-039 1d477c01, u-028 54368c03, u-027 d8e0cc20, u-001 53b3b570)

Reviewed by @review, 2026-09-30, from reading `git show` of each cherry-pick onto claude/main f7bb6268 (detached in
`D:/worktrees/claude/atrium/land-spam`). Board files, tests and docs only, a hub-only deploy. Reviewed during the pause
on the orchestrator's exception for the card audit's unmerged work. The headless suite is @ui's and was not run here.

## u-028 54368c03, the composer attaches several files and pasted images

- **Nothing a file brings is rendered as markup.** The chip's name, state and the remove button's label go through
  `el(tag, cls, text)`, which sets `textContent` (`internal/api/web/m/js/compose.js:83-88`). An image preview is an
  object URL of the local `File`, never a server URL. It is revoked when its chip goes, on send, and on unmount.
- **The upload still takes no path.** The board's composer posts to `/v1/tasks/{id}/files` through `api`, which names
  the room. The phone's default does the same with `X-Atrium-Room` read from `atrium.room`. The destination is
  computed server side, as before. The board offers no upload to a guest (`canUpload: () => !isGuest()`), and on the
  phone a guest's upload is refused by the server and shows on the chip.
- **The send waits for every upload**, so a message cannot leave without the path it refers to. A failed upload
  inserts no path.
- **A paste goes up only when the clipboard holds files and no text**, so copying an image from a web page (which
  brings its markup) stays a text paste.
- **Removing a chip removes exactly its span of text.** The span is tracked through later edits by a single-edit
  diff (`editOf`, `follow`), with a fallback to finding the path by text once an edit has reached into it.
- The paths go in bare, with no paste preamble. That is how the composer already inserted them before this change
  (`tcomposeInsert`).

## u-027 d8e0cc20, the usage tab

- The cumulative line is an SVG path built from numbers only. The two new tooltips go through `esc`. The pace is the
  last hour's whole buckets over the time they cover. The projection is drawn only on 24h and 7d, only when the last
  hour had tokens, and is worded "at this pace". A zero total cannot divide by zero (`proj || 1`, `Math.max(..., 1)`).
- The by-card and by-group bars change only a CSS class. The cache toggle's label and tooltip now say its state.

## u-039 1d477c01, one clock for the headless suite

Test harness only (`scripts/test-board-headless.js`), plus `UC.now` as the usage tab's test seam (`ucNow`, which is
`Date.now()` in production).

## u-001 53b3b570, the phone audit

Docs, JPEGs and a test off the default list. `report.json` (14,074 lines) was scanned for user paths, credentials and
tokens. The only matches are form placeholder labels ("who types the password").

## Findings

None.

## Verdict

**HUB DEPLOY OK** for all four: 1d477c01, 54368c03, d8e0cc20, 53b3b570.
