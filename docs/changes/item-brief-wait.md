# Item brief wait

`internal/link/item_brief.go` read the hub's `GET /_hub/work/<id>` for up to 25 seconds while building a brief on the
launch path. The wait is now 5 seconds. On a timeout the launch proceeds with a one line note in place of the steps
header. Any other failure still yields no header at all.

## Test plan

- `go test ./internal/link -run TestItemBrief`: a hub that never answers returns the one line note within the budget.
- Manual: launch an item card while the hub is stalled, and see the launch start in about 5 seconds with the note in
  BRIEF.md.
