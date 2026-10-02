## Test plan

## @LETTER@. Launching on another room from a room (f-launch-room)

Needs the room built from this change and restarted (the tool and the daemon's route) on the launching machine, and the
hub rebuilt and restarted. Either order works. Use a launching room that is not the hub's own, for example m1mini, and
a target room that has a throwaway directory, for example claude-sg4.

### @LETTER@1. A worker lands on the other room, with the launcher as its launcher

1. From a card on m1mini, call `atrium_launch` with `room: claude-sg4`, `cwd` a directory that exists on sg4 and not on
   m1mini, a `brief`, and `prompt: "read the brief and report done"`.
2. Read the answer, then `atrium_task` the returned `card`, then `atrium_say` the returned `handle`.

**Expected:** `card` is `claude-sg4~<id>`, `handle` is `<name>@claude-sg4` and `watch` is the hub board's
`/#term=<id>`. `BRIEF.md` is in the directory on sg4 and nothing was written on m1mini. The new card on sg4 shows
`spawned_by` `<you>@m1mini`. Both tools accept the card and the handle.

### @LETTER@2. The report comes back

1. Let the worker from @LETTER@1 finish and call `atrium_report`.

**Expected:** the launching card on m1mini gets the notice, as a launch from the hub's own tool would give it.

### @LETTER@3. An unknown room, and a room that is down

1. `atrium_launch` with `room: atlantis`.
2. Stop the sg4 room, wait for the hub to show it detached, and `atrium_launch` with `room: claude-sg4`.

**Expected:** 1. an error that names `atlantis` and lists the rooms the hub knows. 2. an error that says the room is not
answering and that nothing was started or held. Start sg4 again and nothing launches by itself.

### @LETTER@4. The cap is the target room's

1. Fill claude-sg4 to its launch cap with `atrium:subagent` workers, then launch on it from m1mini.
2. Launch from claude-sg4 onto m1mini, which has room to spare.

**Expected:** 1. refused with `at the launch cap of N running workers on room claude-sg4`. 2. goes.

### @LETTER@5. An ATRIUM_ name in env is refused

1. `atrium_launch` with `room: claude-sg4` and `env: {"ATRIUM_TASK_ID": "x"}`.

**Expected:** refused by the target room, as on the hub's own tool, and no session starts.

### @LETTER@6. An old hub

1. With m1mini on this build and the hub on the previous one, `atrium_launch` with `room`.

**Expected:** an error that says the hub is older than launching on another room and to update the hub. Nothing starts.
`atrium_say`, `atrium_task` and `atrium_exit` on another room are unaffected.

### @LETTER@7. The audit line

1. After @LETTER@1, read the audit log for room claude-sg4.

**Expected:** `kind=ctl-launch` with `by <you>@m1mini (claimed): launch claude as claude-sg4~<id>, ok`. No prompt and
no brief text in it.

### @LETTER@8. Headless

1. Run `env -u ATRIUM_LOCATION go test ./internal/cli ./internal/daemon ./internal/link` with
   `-run 'RoomLaunch|StdioLaunch|ARelayedLaunch'`.

**Expected:** pass.

## Decisions

- decided: Which `watch` URL does the room-local tool return? / The hub board form the hub's own handler returns,
  `<hub board>/#term=<bare id>`, with the hub's loopback base. / A room does not know a browser address for its hub
  (its link dials a service, not a URL), so the one source is the hub's own handler, kept identical so the two cannot
  disagree. It opens for an operator on the hub's machine. A hub with a public board address would want it swapped in
  one place, `launchOnRoom`.
- decided: Is an empty launcher (no `ATRIUM_AGENT_NAME`) allowed? / Yes, with no lineage, as the hub's own tool
  allows. / A human running the stdio server by hand has no card to report to, and refusing would be stricter than the
  hub path.
- decided: Which failures are `unconfirmed`? / A 502, 503 or 504 from the target after the post, all of them. / A launch
  is not idempotent, a gateway error can come after the room started the session, and a second launch starts a second
  session. A room the hub knows and cannot reach is found out BEFORE the post, so that one is `unreachable`.
- decided: Is a launch ever held in the outbox like a say? / No. / Sent late it starts a session nobody waits for, and
  the launcher is told at once and can launch again.
- decided: Is the stdio call bounded like the others at 8 seconds? / No, 55 seconds for this call only (`askFor`). /
  The daemon bounds the hub at 45 seconds, and the tool must outlast it so the daemon's sentence reaches the caller
  and not a client timeout that reads as "could not reach the board" after the session may have started.
- decided: Does the tool check `env` for ATRIUM_ names itself? / No. / The target room's `/v1/launch` refuses them
  (`launchOptionEnv`) on every path, and a copy here would be a fork that could drift. A target room too old to refuse
  them also drops `env` whole, which the options-dropped warning names.
- decided: Where does the shared piece live? / `launchOnRoom` in internal/link/control_mcp.go, called by `launchHandler`
  and by `launchAcross` (new file control_relay_launch.go). / The brief asked to extract and not fork. The exit code and
  the relay switch are untouched beyond one new `case RelayLaunch`, per fabric's note about r-exit-guard.
- decided: Does the daemon rate-limit launches like says? / No. / The launch cap per target room already bounds them.

## Checked

- Security. Nothing here reaches a room the hub could not already reach: the op ends in the hub's own launch (the same
  loopback call with `X-Atrium-Room`), after `reachable()`, and a room that is attached to the hub could always ask the
  hub's tool for the same. The launching room is the certificate's name (`take`/`serveRelay` pass it), never the
  request's, and the launcher's card is resolved on that room. The hub only serves the relay to a room that is attached
  (`h.Has(name)`). The cap and the gate are inside `launchOnRoom`. `ATRIUM_` env refusal is the target room's (see
  decisions).
- Mutation checks, each made red and put back. internal/link: lineage room taken from the target, card id tagged
  with the target, launch placed on the asking room (cap, lineage and audit tests all red), `reachable` removed,
  gateway failure made `unreachable`, brief dropped from the spec, `SubagentTag` dropped, audit call removed. internal/daemon: own-room
  check removed, unconfirmed made a 503, old-hub op case removed, launcher and brief dropped, the target's refusal code
  replaced. internal/cli: the across call removed, the route changed to `/v1/launch`, the own-room check removed, a
  brief written locally, a cwd stat added, the older-room case removed.
