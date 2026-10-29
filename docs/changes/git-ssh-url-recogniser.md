Docs: none, scripts/recognisers/README.md now names the ssh form

# git ssh clone urls are recognised

The seeded rows `github-repo-ssh`, `gitlab-repo-ssh` and `bitbucket-repo-ssh` take `git@<host>:<org>/<repo>(.git)` and
fill the same fields as the https repository row. The board's paste gate (`QUICKPASTE_URL`) took only http(s) text,
so it now also takes `git@<host>:<path>`, and the board-wide paste handler uses the same gate.

No reseed: a hub adds a built-in row whose id it has never held the next time it reads its table
(`internal/link/recognisers.go`), so a hub running this build picks the rows up by itself. The board gate needs the
new build on the room that serves the board.
