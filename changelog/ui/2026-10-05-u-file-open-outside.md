# A file link in the terminal asks where to open, and a markdown file renders in a tab

Clicking a file path in a terminal now offers a small menu instead of going straight to the editor: **open in atrium's editor**,
**open in a tab**, and, only when that card's room has an `editor_command` set, **open on <room>**, which runs that command on the
machine the file is on. There is still no default program: an empty `editor_command` means no third entry. A directory still opens
the file browser at once.

**Open in a tab** streams the file from the room that holds the card, through the hub, with no download prompt. A markdown file opens
in a rendered page (vendored `marked`, then DOMPurify; raw HTML is shown as text, script and `javascript:` links are removed, remote
images are not fetched, and relative links and images resolve to the same card). Everything else opens at the new
`GET /v1/tasks/{id}/files/view` as `text/plain`, or its own type for a png, jpeg, gif, webp, bmp, ico or avif, with `nosniff` and
`Content-Security-Policy: sandbox`. HTML and SVG are never served as themselves from a card.

Screens are in `docs/screens/u-file-open-outside/`.
