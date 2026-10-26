Docs: none

# A worker launched without go or make is the room's misconfiguration

At launch the room looks on the PATH the runner gets, for `go` when the card's folder holds `go.mod` and for `make`
when it holds a `Makefile`. A miss is logged and carried on `/v1/health` and the `health` event as `toolchain_missing`
(`tools`, `reason`, `card`, `since`). The launch still goes ahead. The next launch in such a folder that finds both
clears it. `internal/daemon/toolchain.go`.

Not yet a growler: the hub's `growl.reason` CHECK does not allow a new reason without a migration.

## Test plan

## @LETTER@. A room with no make on PATH says so

### @LETTER@1. Launch a worker on this repo

1. On a room whose PATH has no `make`, launch a worker in an atrium checkout.
2. `curl` the room's `/v1/health`.

**Expected:** the room logs `room misconfigured: <card> launched without make on its PATH`, and `/v1/health` carries
`toolchain_missing` with `tools: ["make"]`.

### @LETTER@2. Put make back

1. Put `make` on the room's PATH, restart the room and launch another worker there.

**Expected:** `/v1/health` no longer carries `toolchain_missing`.
