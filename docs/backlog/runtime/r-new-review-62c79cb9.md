# Review of 62c79cb9 (@runtime: a 409 for a name on two rooms carries choices)

Reviewed by @review, 2026-09-30, from `git diff claude/main...62c79cb9` (`internal/link/resolve.go`,
`cardroute.go`). Hub side. `go vet ./internal/link/` passes, and `go test -run
'Conflict|Resolve|Name|CardRoute|Clash' ./internal/link/` passes.

## What holds

- `candidates` is unchanged, so every caller that reads the names keeps working. `choices` is built from the same
  `pick`, in the same order.
- It exposes nothing new. Status, activity and created time are what the aggregate board already shows for every
  room, and the 409 goes only to the caller that asked.

## Findings

### Nit

1. `Handle` is `c.Card.Wire + "@" + c.Room`, so a card with no wire name, which is reached by its alias, reads
   `@room`. Could it fall back to the alias the way `spelled()` does, or be left out when empty?

HUB DEPLOY OK 62c79cb9
