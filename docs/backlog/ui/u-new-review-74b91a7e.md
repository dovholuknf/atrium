# Review of 8646f571 + 74b91a7e (@ui u-m-viewer: the /m file viewer)

Reviewed by @review, 2026-09-30, from `git diff 0184f34f 74b91a7e`, read only. The design (0ccd4312) went through
@rnd, and 74b91a7e folds its five changes in. Hub side. Board and headless checks are @ui's, and I ran none.

## What holds

- **Nothing a file holds becomes markup, apart from markdown through the safe renderer.** Text, code, json, svg and
  html go into a `<code>` through `textContent`. Markdown goes through `mMd.render`, the renderer reviewed at
  ab758324 and 94774247. The title, notes and buttons are all `textContent`.
- **Images come only from the raster allowlist.** png, jpeg, gif and webp are re-typed by extension into a `Blob`
  and shown through an object URL. That URL is revoked when the sheet moves to another file or closes, so an svg can
  never run as an image.
- **The server is the boundary.** Every read goes to the card's `files` or `files/text` route, so `safepath`
  decides. A 403 says "not in this card's folder, or not readable", which is no more than the 403 itself says.
- **Text past the text route is bounded on purpose.** It asks for `Range: bytes=0-1MiB-1` with `If-Range` from the
  HEAD's `Last-Modified`. A NUL byte turns the read into an "ask", and a split UTF-8 tail is trimmed. A non-UTF-8
  file under 2 MiB comes back from the text route as "not text" and is offered as a download.
- **Unknown types are asked about** and downloaded only on a yes.
- **History.** One entry per file opened. The chevron steps back through it, and `popstate` closes or re-renders.
  The card sheet's close resets the viewer. A `seq` counter drops answers from a file the reader has already left.

## Findings

### Low

1. **A missed `If-Range` downloads the whole file.** When the file changed after the HEAD, `If-Range` fails and
   the server answers 200 with the entire body. A server that ignores `Range` does the same. `arrayBuffer()` then
   reads all of it into the phone's memory before `slice(0, CAP)`, which matters for a large log that is still
   being written, the case most likely to change between HEAD and GET. Reading `r.body` with a reader and cancelling
   once CAP bytes have arrived would keep the 1 MiB bound whatever the server answered.

### Nit

2. If the HEAD gave no size, an image is fetched whole with no cap. The 20 MiB check only runs when
   `Content-Length` came back.

Quality: both commits predate the Sonnet switch (cc3bd954), so they say nothing about it.

HUB DEPLOY OK 8646f571 74b91a7e

## Re-read of 03931f6e (@ui, low 1 and nit 2)

`git diff 74b91a7e 03931f6e -- internal/api/web`, read only. `readCapped` reads `r.body` with a reader and cancels it
as soon as the total passes the cap, so at most one chunk past the cap is ever buffered. It falls back to
`arrayBuffer()` only where the browser has no stream reader. Text past 2 MiB uses it with 1 MiB, whether the answer
was a 206 or a whole-file 200. An image uses it with 20 MiB whether or not the HEAD gave a size, and one over the cap
is asked about. When no total is known, the cut note leaves out "of N". Both closed. No findings.

Quality: after the Sonnet switch. The fix is narrow, matches the finding, and asserts the cancel instead of a byte
count, which @ui found flaky over loopback. No drop seen.

HUB DEPLOY OK 03931f6e
