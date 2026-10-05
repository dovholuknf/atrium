# r-new-mingit-no-http-backend. A room on MinGit cannot serve its branches to the hub

Status: filed by the orchestrator 2026-10-04 23:30. Owned by @runtime. Not started.

## What is wrong

sgg runs MinGit 2.56 (`%USERPROFILE%\.local\git`), put there by hand because the machine has no winget. MinGit leaves
out `git http-backend`, so the room's git route (`internal/gitsync/backend.go`, `gitCGI`) has nothing to run and the
hub's collect from sgg answers 500 (`cgi: no headers` in the room log). Branches made on sgg can never reach claude/main.

## Done looks like

- Provision installs a git that has `http-backend` (PortableGit rather than MinGit), or the room serves upload-pack
  without it.
- room-check reports a git with no `http-backend` as a failure, with the fix, instead of the room failing silently.
- The hub's collect error names the cause rather than a bare 500.
