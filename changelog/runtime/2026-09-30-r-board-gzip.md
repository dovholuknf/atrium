- **The board is gzipped, and a reload is a 304.** The hub and the room serve every board file through one server
  (`internal/webasset`): text is gzipped once and kept, each file carries an ETag, and `no-store` became `no-cache`,
  so a reload asks and gets a 304 with no body instead of the file again. First load 2.7 MB to 0.86 MB, a reload 0
  bytes, same 81 requests. A board served from disk is re-read when a file changes. Hub and room, needs both deploys.
  (r-new-board-gzip)
