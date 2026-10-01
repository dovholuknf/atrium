# Review: u-m-docs feadd779 (hub documents D2 views, @ui)

Range `m1mini/claude/u-m-docs` e026f353~1..feadd779: e026f353 (design note), 2664f40e (the views), c88a76e7
(screenshots), with hub-main eff2fb8e merged. Read against `docs/changes/u-m-docs.md`, @rnd's 9 changes and the
contract `docs/backlog/fabric/hub-documents-api.md`. Asked to read hard on untrusted text.

## What holds

- **Hub text is text.** Titles, slugs, names, `by`, origin words, the hub's `error` and `rule`, sizes and dates all
  go in through `textContent` (`el()`). The only `innerHTML` writes are a fixed SVG for the back button and
  `mMd.render` for a markdown version.
- **Markdown goes through md.js unchanged in posture.** The whole run is escaped before markup. The new `/d/` branch
  writes the escaped label and `esc(u)` for an href that matched the exact pattern. Remote images stay links, and
  only http and https survive as links.
- **The raw bytes cannot run on the hub's origin.** `docsRaw` (internal/link/docs_api.go:363-389) always sends
  octet-stream, `nosniff` and `attachment`, and docs_api_test.go:189 locks it. So an http link in a document that
  points back at `/_hub/docs/<slug>/raw` downloads rather than renders. The client never sets a raw URL as a `src`
  or a navigation.
- **Images** are drawn only from a blob typed by its own first bytes (png, jpeg, gif, webp), under 20 MiB, and every
  object URL is revoked when the sheet moves on (`dropUrls` on hide and on each render). SVG and HTML are never
  sniffed in, so they are downloads.
- **The 1 MiB render cap** is enforced while reading (`readCapped` cancels the reader past the cap), and a NUL in the
  read text makes it a download. A cut inside a character is trimmed.
- **The `/d/` link handler** matches `DOC_RE` exactly on the resolved pathname, and only when the resolved origin is
  this page's own and the hub has documents. Modified clicks keep the browser's behaviour.
- **Compare.** Myers with a prefix and suffix trim and `MAX_D` 2000. I extracted `myers` and `diffOps` and ran them
  in a scratch script (D:/worktrees/claude/reviews/github-dovholuknf-atrium/proof-feadd779/run.js). 20,000 random
  pairs: both sides rebuild from the ops, and the edit count equals the LCS minimum every time. On sides of 1M and 5M
  lines with more than 2000 changes, the refusal comes in 27 to 71 ms. The trace is at most 2001 arrays of 4003
  ints, about 32 MB, then dropped.
- **Downloads** use a name with slashes and C0 controls replaced, and cut to 120 characters. The `override` field is
  offered only when the hub's settings say operator.
- **Requests** are plain same-origin fetches with no header of ours. CSRF and the operator gate stay the hub's.
- **The card line** asks at most once in 30 seconds per card, and on a board without hub documents it never shows.

## Findings

### Low

1. **A one-segment Git Bash path in a reply becomes a document link.** The bare-path branch in md.js now tests
   `DOC` before `inside()`, and `/d/tmp`, `/d/work` or `/d/git` match `^\/d\/[a-z0-9-]{1,60}$`. Agents on Windows
   print these paths often, and nothing marks them as code. With hub documents the tap opens a document view that
   answers 404. On a room board without hub documents, the link is a plain `<a href="/d/tmp">` and navigates to a
   404 page, where it used to be text. Fix: make only a markdown link `[..](/d/..)` a document link, and leave a bare
   path as text. Or render a bare `/d/` path as a link only when `mDocs.available()` is true.
2. **A relative link in a hub document renders as a dead card-file control.** `docs.js` renders with
   `{ id: "", worktree: "" }`, and `inside()` returns true for any relative path with no `..`. So `[notes](notes.txt)`
   becomes an `md-file` button with `data-card=""`. A tap asks the viewer or the card download for card "" and fails.
   `![x](a.png)` becomes an `md-img` that is never hydrated, because only `m-replies` and `m-recap` are observed. No
   bytes leave and nothing is fetched cross-card, but the control is a lie. Fix: a context flag (`ctx.files = false`)
   that turns `inside()` off, so these stay text, the way an outside path does.
3. **A diff document of 1 MiB can be a million DOM nodes.** `diffBlock` makes one `div` per line, and a 1 MiB file
   of short lines is roughly that many. Not measured, but a node count that size is seconds of layout on a phone. It is a self-inflicted tap
   on content a card or a share user uploaded. Fix: cap the drawn lines (for example 20,000) and offer the rest as
   the existing "download the rest" strip.

### Nit

4. `decode` strips trailing U+FFFD from every read, not only from a cut one. Strip it only when `got.over`.

Quality: after the Sonnet switch. The untrusted-text paths are complete and each of @rnd's 9 changes is present.
The Myers code is correct on the first pass, which is the hard part. The misses are second-order: what the shared
renderer does with a context it was not written for, and a path pattern that collides with this machine's own path
style. No drop.

HUB DEPLOY OK and ROOM DEPLOY OK e026f353~1..feadd779. The lows can follow.
