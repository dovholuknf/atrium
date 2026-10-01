# u-m-viewer: design of the /m file viewer

Spec: `docs/backlog/ui/u-new-m-file-viewer.md`. Read only. Written before the build, for @rnd.

## The test plan section is added with the build

A `## Test plan` section is appended here once the viewer exists. This half page is the design.

## Back mechanism

A history entry, and the viewer is a full-height sheet drawn over the thread, not a replacement for it. Opening pushes
`{ mcard: <id>, mview: <path> }`. The thread's DOM and its scroll position are never touched, so Back, which only closes the
sheet, returns to exactly where the reader was. The card's own popstate handler already ignores a state for the card that is
open, and now also closes the viewer when the state it lands on has no `mview`. A reload on a viewer entry drops back to
the thread (the entry is replaced with the card's), since the viewer holds nothing worth restoring.

## What counts as text

By extension, because a path is all that is known before the read. Markdown: `.md`, `.markdown`. Text: `.txt`, `.log`,
`.csv`, `.tsv`, `.ini`, `.conf`, `.env`, `.diff`, `.patch`, and extensionless `Makefile`, `Dockerfile`, `LICENSE`, `README`.
Code: `.js .mjs .ts .go .py .rb .rs .java .c .h .cpp .sh .ps1 .sql .html .css .xml .yaml .yml .toml .proto .svg`. JSON:
`.json`, pretty printed when it parses and shown as it is when it does not. Images: `.png .jpg .jpeg .gif .webp`. SVG is
shown as text, never as an image, since an SVG can carry script. A file of a text type whose first 4 KB holds a NUL byte is
treated as unknown, so a binary named `.log` does not print as noise.

Markdown goes through the same safe renderer as the thread (`mMd.render`, with the card's context so links to other files
of the card open in the viewer too). Everything else is set with `textContent` in a monospace block that scrolls sideways.

## Size cap

The size comes from a `HEAD` on the card files endpoint (Go's mux answers `HEAD` for a `GET` route, with the length), before
anything is read. Text is read with `Range: bytes=0-1048575`. A server that ignores the range answers 200 and the
reader slices the first 1 MB. Past the cap the viewer says "showing the first 1 MB of 3.4 MB" with a "download the rest"
button, which downloads the whole file. Images are read whole up to 20 MB and asked about beyond it.

## Unknown types

Asked first, never downloaded on opening: "Can't preview X.zip (1.2 MB). Download?" with Download and Cancel. The size is
the `HEAD`'s. Download goes through the same fetch and save that the file links use today. Cancel is Back.

## Errors

`403` from `internal/safepath` is "not in this card's folder", never "missing". `404` is "no such file". Anything else
shows the daemon's message. The viewer never shows a broken image.

## Text size and width

The sheet sits inside the card, so it inherits `--m-fs`, and the same two-finger pinch that sizes the thread sizes it.
Reading width is the column of the page, with `overflow-wrap` for prose and sideways scroll for code.

## Where a link comes from

The detection is the thread's own (ab758324 with the drive-path fix of 94774247): an absolute path under the card's folder,
or a relative path in markdown link syntax. A tap on one opens the viewer where it used to start a download. Nothing else
about those links changes.
