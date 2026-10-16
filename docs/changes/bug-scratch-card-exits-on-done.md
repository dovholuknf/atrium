## Design

Exit-on-done (`exitsOnReport`, internal/daemon/exitonreport.go) is for work whose product is in git, which the
launcher lands. A scratch launch has no branch to land, so its transcript and files are the product and the card stays
open.

Where scratch is known: `LaunchRequest.Scratch` in `Daemon.Launch` (internal/daemon/launch.go). It is the one place
both paths meet. The control MCP `atrium_launch` already posts `"scratch"` in the body of `/v1/launch`, and a
cross-room launch is that same body carried by the hub to the room, so the field arrives with no new plumbing.
`atrium launch` has no scratch flag and starts nothing an agent launched, so it is unchanged.

The change:

1. `Daemon.Launch`, beside where `ItemTag` is stamped on the request: a request with `Scratch` and no
   `atrium:keep-open` tag gets `atrium:keep-open` appended. The tag shows on the card and the operator can remove it,
   after which the card exits on its next done report like any other.
2. `exitsOnReport` already returns false for `KeepOpenTag` (via `staysOpen`), so it needs no new branch. Its header
   gains a line saying a scratch launch is kept open by that tag.
3. Test: a card launched with `Scratch` carries `atrium:keep-open` and `exitsOnReport` is false for its done report,
   and a launch without scratch (a claude/* worktree launch) still does not carry it and still exits.

`atrium:keep-open` also exempts the card from reclaim (reclaim.go), so a scratch card stays until someone closes it.

Files: launch.go, exitonreport.go, one unit test, this note, the changelog line.

## Test plan

## @LETTER@. A scratch launch stays open on done

### @LETTER@1. Scratch

1. Launch a worker with `atrium_launch` and `scratch: true`. Confirm the card wears `atrium:keep-open`.
2. Have it report done. Confirm the runner is still running 30 seconds later.

### @LETTER@2. Worktree

1. Launch a worker in a claude/* worktree with no scratch. Have it report done.
2. Confirm it is exited about 5 seconds later, as before.
