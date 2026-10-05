# u-file-open-outside

Built: click menu on a terminal file link (editor / tab / open on <room> when the room has an editor_command, asked of that card's room);
`GET /v1/tasks/{id}/files/view` (internal/api/fileview.go); `read.html` + `read.js` markdown tab with vendored marked 12.0.2 and DOMPurify 3.1.6.

Tests: Go `fileview_test.go` (content types, nosniff/sandbox, resolved-extension typing, 403 outside/other card/symlink, 400/404);
headless `fileOpenOutside` (menu entries, files/open call, tab URLs, no third entry without editor_command, hostile markdown) run with
`linkTip` and `bootClean`. Not the whole suite. Pre-existing failures, unchanged by this branch: `TestTheWalkerLaunchSetAndClear` and two
check-terminal.js invariants (verified on the base commit).

## Review fixes
Security:
- Markdown was first sanitised into a live `div`, so a remote `<img>` started loading before it was rewritten (caught by the test). Now parsed in an inert DOMParser document.
- read.html carries its own CSP (no inline script, no remote sources) and its script moved out to read.js, so a sanitizer hole is still not script.
- Content type is chosen by the extension of what the path RESOLVED to, so a symlink `pic.png -> page.html` is text.
- SVG is text/plain, not an image; images limited to raster types. Raw HTML in markdown is escaped by the renderer before DOMPurify; `#` anchors and non-http(s)/mailto schemes lose their href; remote images are not loaded.
- `../` and absolute paths, another card's file and an escaping symlink all answer 403, as download does. Guest links are default-deny, so the route is not reachable from a lent session.
Codebase fit:
- Reuses safepath.Contained, realRelative, RealPathHeader, the shared `showMenu`, `openOnDaemon`, `openEditor`, and the editor's "new tab" button now uses the same `fileTabURL`. openFromTerminal still never calls files/open (check-terminal rule).
- `read.js` lives beside read.html, because check-board.sh requires every file in js/ to be loaded by index.html.

## Notes
- Plain click on a file is now a two-step (menu, then entry). Directories are unchanged.
- Anchor links (`#x`) inside a rendered doc are dropped, not scrolled to.
- Headless run needed NODE_PATH pointing at the main checkout's node_modules (playwright is not installed in the worktree).
- Before/after PNGs: docs/screens/u-file-open-outside/ (popup-before/after, markdown-tab-before/after).
