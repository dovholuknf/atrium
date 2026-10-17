## Design

### Where the time goes

`atrium_launch` without `room` (or with this room's own name) from the stdio control server runs
`launchHandler` in `internal/cli/control_peers.go`. Its last step is `ask(ctx, POST, "/v1/launch")`, and `ask` is
`askFor` with `peerToolTimeout`, 8 s. The error in the report is that call, word for word: `could not reach the board at
http://127.0.0.1:7778: Post ".../v1/launch": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`.

The hub proxies `/v1/launch` to the room, and the room answers only after it has trusted the worktree, written the
launch and started the runner (`internal/daemon/launch.go` waits for the runner to prove it started, two seconds plus
the runner's load time, more under load). That is routinely past 8 s, so the caller gives up while the room carries
on and files the card a little later. `GET /_hub/rooms` is fast because it never touches a room's launch path.

f-launch-answer-lost (`2ebfb464`) fixed the OTHER road: the hub's own `launchOnRoom` in `internal/link/control_mcp.go`
got a 30 s bound and `findLaunched`, which looks for the card by worktree and creation time after a 502. The stdio
road in `control_peers.go` was never given either, and the cross-room stdio road (`launchAcrossRoom`, 55 s) has the
hub do the recovery. So every plain launch from a minion's session still has the 8 s bound and no recovery.

### Fix

In `launchHandler`, for the `/v1/launch` post only:

1. Bound it by `launchLocalWait`, 45 s, under the 55 s `launchAcrossWait` and over the room's normal start.
2. Take `GET /v1/tasks` just before the post and keep the set of card ids. A card is "launched" when its id is NOT in
   that snapshot and its worktree equals `cwd`. No clock is compared: the hub proxies to rooms on other machines.
   `findLaunched` in `internal/link/control_mcp.go` judges by time today, so it moves to the same snapshot rule and
   both roads share one helper.
3. When the post fails with a timeout or a bad gateway, look for a launched card before reporting a failure. Found:
   return it with a note that it is running and must not be launched again.
4. Not found: remember `cwd` and the id snapshot in the process. The next launch for the same `cwd` checks for a card
   outside the remembered snapshot BEFORE it posts, and returns it if there is one. A launch that returned an answer
   clears it. Nothing is stored on disk.

### Not done

- A launch key. Done in f-002 (`docs/changes/f-002.md`): the snapshot recovery here stays as the fallback for a caller
  that sends no key and for a room older than the field.
- Follow-up: the room answers `/v1/launch` after the card is filed with status `starting`, not after the runner
  proves it started.

### Test

`internal/cli/control_launch_timeout_test.go` with a fake board (httptest): `/v1/launch` that outlasts the bound,
`/v1/tasks` that lists the card. The bound is a package variable so the test uses milliseconds. Cases: slow
answer with the card present returns it and `Recovered`; slow answer with no card yet errors, then the retry returns
the card that has since appeared without posting again; a refusal does not leave a memory.

## Test plan

### @LETTER@. A slow launch returns the card

1. From a worker session, `atrium_launch` into a directory whose runner takes more than 8 s to start.

**Expected:** the answer names the card, as it would have after a fast start. It never says `could not reach the
board`.

### @LETTER@2. A retry does not duplicate

1. Launch, and interrupt the answer (stop the room's reply for 50 s), then launch again with the same directory.

**Expected:** the second call returns the first card with the do-not-launch-again note, and the board shows one card.
