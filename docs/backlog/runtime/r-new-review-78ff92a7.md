# Review of 78ff92a7 (@runtime: gzip and ETags for every board file, hub and room)

Reviewed by @review, 2026-09-30, from `git diff claude/main...78ff92a7`. Hub and room side. `go vet` on webasset, api
and link passes. `go test ./internal/webasset/` passes, and `go test -run
'Web|Board|Asset|Split|CardURL|Manifest|Vendor|Cache' ./internal/api/ ./internal/link/` passes. A scratch test of
traversal against a disk board was run and removed.

## What holds

- **Path handling.** `fileName` cleans the path from `/` with `path.Clean`, so `..` cannot climb, and `/x/index.html`
  is still left to the file server's redirect. The disk board is `os.DirFS`, which on Windows also refuses `\` and
  `:`. Scratch results: `/../secret.txt` and `/js/../../secret.txt` got 404. `/js/..\..\secret.txt`, `/..\secret.txt`
  and `/C:/Windows/win.ini` got 500 from the file server, with no body beyond the status text. `/` served the board.
  Nothing outside the tree was read.
- **no-cache against the reload-on-build-id rule.** The rule holds. `no-cache` revalidates every file on every load,
  and the ETag is a hash of the bytes, so a rebuilt file gets a new tag and a 200. An unchanged file gets a 304 even
  across builds, which is correct. The build id comes from `/health` and is not affected. `sw.js` passes fetches to
  the network and caches only its own down page, so no second cache can hold a stale file. Only `/vendor/` keeps
  `max-age`, as before.
- **The encodings stay apart.** The gzipped body has its own `-gz` tag and `Vary: Accept-Encoding`, so a cache cannot
  serve one body for the other. `gzip;q=0` is honoured. A file that does not shrink is served plain.
- **Memory is bounded.** Only files that open and are regular get cached, so the map is at most the tree. A disk
  board is re-read when its size or mtime changes.
- **The hub's other paths are unaffected.** Everything the hub rewrites (`/health`) or reads (`/v1/tasks`) is API,
  and webasset never serves it.

## Findings

### Nit

1. A path with a backslash or a drive letter answers 500, not 404, because the file server maps DirFS's
   invalid-path error to 500. This was already true, and nothing is read. `fileName` could answer 404 for a name that
   is not `fs.ValidPath`, or that holds `\` or `:`, so the status does not look like a server fault.

HUB DEPLOY OK 78ff92a7. ROOM DEPLOY OK 78ff92a7.
