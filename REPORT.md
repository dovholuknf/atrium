# r-keepalive-stale-badge

- `tick` (internal/daemon/keepalive.go) no longer returns early for a suspended room. It runs `clearOnRealTurn` per card, then skips decide/refresh/stop while suspended.
- `keepaliveCardView.Suspended` carries the reason. keepalive.js adds a tooltip line (stopped and warm chips) naming the room suspension, the reason, and the clear button.
- The suspension does not self-clear: it follows two misses on two cards, and a refresh after it spends money. The badge says how to clear it. Reasoning is in a comment in `tick`.
- Go test `TestKeepaliveSuspendedRoomStillClearsAStoppedCard` passes (`go test ./internal/daemon -run Keepalive`). `go build -o build.claude/ ./...` is clean.
- Headless: added assertions to the `keepalive` section (ka-miss gets `suspended`). NOT RUN: playwright is not installed on this machine.
- NOT DONE: the atrium:codebase-steward review. The ATRIUM_ONLY_SUBAGENTS hook blocked plain subagents and I have no atrium_launch. I read the diff myself instead.
