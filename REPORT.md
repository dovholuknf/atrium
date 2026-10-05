# f-new-d1-nits report

## Done
- `isText` in `internal/hubstore/docs.go` backs up at most 3 steps. Test `TestIsTextCutInsideCharacter`.
- The `docOrigin` comment moved above `docOrigin`, `docBy` has its own (`internal/link/docs_api.go`).
- The phone's document list with a trailing slash serves the shell like `/m/docs` (`internal/link/proxy.go`). Test added
  in `TestDocURLsAnswerWithThePhoneShell`.
- QUEUE.md open nits marked BUILT. Changelog `changelog/fabric/2026-10-05-f-new-d1-nits.md`.

## Left
Nothing for these three. The other open nits in QUEUE.md are untouched.

## Verify
`go test ./internal/hubstore` passes. `go test ./internal/link -run Doc` passes. The full `./internal/link` run was not
compared against the known Windows failures.
