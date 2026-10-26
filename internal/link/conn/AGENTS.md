# AGENTS.md (internal/link/conn)

The connection the room link keeps: the hub's half that accepts and lends connections, and the room's half that dials
and serves. Read `../AGENTS.md` first. Its rules hold here, and the files table there names what lives in this package.

- **conn imports `wire` and `transport`, never `link`.** The edge is pinned in `scripts/check-imports.go`. The hub's
  items, backlog, merges and board stay in `link`, which reaches conn through the aliases in `link.go`.
- **`seams.go` names the unexported pieces `link` reaches for.** Keep it to what `link` needs, and say why in its
  comment.
