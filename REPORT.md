# f-say-resolve-across-rooms

The hub's cross-room index (`internal/link/everywhere.go`) now also keeps every live card, tagged or not, in `named`. It is used only to resolve a bare name.

- `lookupEverywhere` tries the tagged cards first, as before. If none match, it tries all live cards on other rooms. One match routes as `name@room`. Several give a 409 listing `name@room`. None gives a 404, which now lists cards from every room.
- `RelayFind` answers with `sendName()`, so a card with no wire name no longer produces `@room`.
- `sayEverywhere` in `internal/daemon/relay.go` adds a note saying which room a bare name went to. A parked match is still woken only when `wake` was passed.
- Tests, all in `internal/link/everywhere_relay_test.go`: a unique untagged remote match, an ambiguous match, and a local match that still wins. `go test ./internal/link ./internal/cli` pass.
- `internal/daemon` has failures that do not touch this change. They are unix socket paths that are too long, a claude path guard, and a keepalive args test.
- `atrium_exit` still does not fall through to another room. The existing `TestHubSideExitDoesNotFallThrough` makes that deliberate, because an exit cannot be taken back. `atrium_task` already fell through on the hub side, and now does so for untagged cards too.
