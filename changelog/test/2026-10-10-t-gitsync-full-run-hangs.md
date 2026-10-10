The `internal/gitsync` integration tests no longer take longer than the 10 minute go test limit. They never hung: 252 tests
ran one after another at about 580 seconds, so a full run hit the timeout while a healthy git child was still working.
Now they run in parallel (about 3 minutes, three runs in a row passed), the git CGI no longer dies on a git installed
under a folder the account cannot stat, and a few tests that committed with the user's signing config now turn it off.
Item t-gitsync-full-run-hangs.
