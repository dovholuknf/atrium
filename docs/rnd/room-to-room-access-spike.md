# Room-to-room access: a room reaching another room's machine (SPIKE)

Status: research, spike, 2026-10-01. Its conclusion: make the ops the hub runs over ssh callable from any room first,
and raw room-to-room ssh second, off by default.

Origin: spike by @rnd, 2026-10-01. Nothing built. Backlog: `docs/backlog/rnd/rnd-new-room-to-room-access.md`. It puts
reach between machines, so @review reads it for security before anything is built. Revised for @review's HOLD
(8f790b14, `docs/backlog/rnd/rd-new-review-7bf10158.md`).

## 0. The recommendation

**Most of what a director needs from another machine is an op the hub already runs with the operator's own ssh, so
make those ops callable from any room first. That needs no new key at all.** Raw room-to-room ssh comes second,
off by default, granted per ordered pair of rooms, and always through a named command atrium holds by name only.

1. **Hub-run ops (first).** The things that sent work back to sg4 on 2026-10-01 are scripts the hub machine runs over
   the operator's ssh: `room-toolchain.ps1`, `room-check.ps1 -Fix`, `provision-room.ps1`, `room-git.ps1 worktree`.
   Expose each as a hub op that a director on any room can ask for. The hub runs the script, with its own ssh, and
   returns the output. A director on m1mini fixes sg3's toolchain without m1mini ever reaching sg3. No key moves,
   and the key stays on the hub. **But the hub machine holds ssh to every room, so an op is command execution on the
   most trusted box, and it is shaped as section 3.1 says:** fixed typed signatures, argv built by the hub, nothing
   passed through, and anything that changes a machine behind the gate.
2. **A reach grant (second, only for what no op covers, and only between machines of the same trust).** The hub
   holds grants `{from room, to room, route, name}`, default none. A room with a grant may run `atrium reach <to> -- <command>`. That goes through the human's
   permission gate like any tool call, and is logged on the card. The route is one of the three below, and atrium
   holds only the name of the thing the machine's own tooling set up.

**The boundary is where credentials sit, not any rule.** Today no room machine holds a key to another, and that is
the only thing that keeps an agent's Bash on m1mini off sg3. A grant's key, identity file or zrok environment on the
source machine is usable by every agent on that room with plain Bash, whatever `atrium reach` and its gate say. So a
grant is only for two machines run by the same operator account, where the agents already run as that operator. For
everything else, the hub op keeps the key on the hub.

## 1. What is there today

- **Atrium runs no ssh itself.** No Go code calls `ssh`. Every ssh use is a script run on the hub's side, with the
  operator's `ssh` and its config, and no key passed (`scripts/room-git.ps1:75-92`, `:147`, and the same pattern in
  `provision-room.ps1:271`, `room-gate.ps1` and `room-toolchain.ps1`). Key choice is the operator's `~/.ssh/config`.
- **Rooms dial the hub. The hub dials no room.** One join string, transport `direct`, `ziti` or `zrok`
  (`internal/link/certs.go:193-221`). sg3 and m1mini attach direct over the LAN (`docs/backlog/fabric/f-022.md:23`).
  The hub's ssh to rooms is the operator's, outside atrium.
- **The credential line.** "Atrium may hold the NAME of a command or host that has a credential, never the
  credential" (`docs/backlog/fabric/f-003.md:23`, quoting `CLAUDE.md`). `docs/fabric/overlays.md:18-23, :79`: atrium
  never decides who may connect, never issues an identity, never creates a service or writes a policy.
- **The pattern to copy.** `internal/daemon/overlay_reserve.go:42-71` reserves a zrok share name with the account
  token that `zrok enable` left in `~/.zrok2`, read per call and never stored or shown.
- **Per-room opt-ins already exist in the hello.** `Upgrades` and `Git` are bools a room declares, and "the hub never
  asks a room that did not say so" (`internal/link/protocol.go:26-55`). Per-room hub settings exist too:
  `launch_caps` holds `{"default", "rooms": {...}}` (`internal/link/launchcaps.go`).
- **The remote-git refusal is dotfiles' hook, and rooms do not have it.** It matches
  `\bgit\s+(?:-\S+\s+)*(push|pull|fetch)\b` in `claude/hooks/pre-tool-use-hook.ps1`
  (`docs/fabric/git-sync-design.md:15-21`). `scripts/room-gate.ps1:13` copies nothing else from dotfiles to a room,
  "least of all the footgun guard".
- **No deny rule ships either.** `Bash(git push*)` appears only in `internal/claudeconf/claudeconf_test.go:75,99`.
  On m1mini `~/.claude/settings.json` has no `permissions`, and the room's rules are `{"rules":null}`. What stops a
  push from m1mini today is that its clone's remote is `https://github.com/...` and the machine holds no ssh key and
  no `gh` login, plus the permission gate when it is on. What stops an agent's `ssh` to another room is that there is
  no key to use. The credentials are the boundary.

## 2. The three routes, each held by name

| Route | What the operator sets up, outside atrium | What atrium holds | Auth | Revoke |
| --- | --- | --- | --- | --- |
| Direct ssh | a key on the source machine, its public half in the target's `authorized_keys`, a `Host` alias in the source's `~/.ssh/config` | the alias name | the ssh key | remove the line from `authorized_keys`. Dropping the grant stops atrium offering it, not the key, and the board shows "grant removed, key still authorized on <target>" until the operator removes it |
| zrok private | the target runs `zrok share private --backend-mode tcpTunnel localhost:22` on a reserved name. The source runs `zrok access private <name>` | the share name. The token stays in `~/.zrok2`, used by zrok itself | still an ssh key, because zrok gives reach, not a login | release the share, or remove the key |
| OpenZiti | a service `ssh-<room>`, a bind on the target's identity, a dial policy for the source's identity, made by whoever runs the controller | the service name and the identity file's path | the ziti identity for reach, and an ssh key or zssh for the login | remove the dial policy |

**Yes, atrium can drive every route holding only names**, the way `overlay_reserve.go` does. Note that **zrok and
OpenZiti replace the network route, not the login.** Each still needs an ssh credential on the target, unless
OpenZiti's zssh is used. "No key exchange outside ssh's own" is true, but ssh's own key is still a key the operator
places.

**Every route leaves a usable credential on the source machine**: the ssh key, the ziti identity file, or the zrok
environment. Any agent on that room whose user can read it can use it directly, without `atrium reach`. Section 0's
boundary applies to all three.

## 3. Answers

### 3.1 How a hub op is shaped

- **A fixed, typed signature per op**, defined on the hub: the op name, the room (resolved from the hub's inventory,
  never a host or address), and named fields validated by pattern, such as a worktree name as
  `[a-z0-9][a-z0-9._-]{0,63}`. The hub builds the argv. Nothing a caller sends is passed through, and a script's
  `-Ssh`, `-SshOption`, `-Target`, `-Path`, `-Repo` and `-Force` (`room-git.ps1:70-91`) are never reachable.
- **Argv, never a string.** No op concatenates a caller field into a remote shell command. Each script's ssh
  invocation is checked for that before its op is exposed.
- **Read-only ops run freely**: `room-check` without `-Fix`, a toolchain report, a worktree list. **Ops that change a
  machine go through the calling card's permission gate**, shown as `room_op sg3 provision` with its full argv: `-Fix`,
  `provision-room`, a `room-toolchain` install, a worktree create or remove. They are never auto-approved, by a
  standing rule or by `global_auto`. A machine-changing op from a card whose gate is off (`ATRIUM_PERM_GATE=off`, as PR runner
  forks run) is refused, because there is no live human gate to ask.
- **The hub authorizes the caller** by its room certificate and its card, and logs every op on the hub and on the
  card.

### 3.2 Reach grants and the rules

- **Default: off.** No grant, and no hello field, means no reach. Turning it on is two things the operator does: set
  up the route on both machines (section 2), then add the grant on the hub (`atrium hub reach add m1mini sg3 ssh
  sg3-claude`). Provision gains `-Reach <room>=<route>:<name>`, which prints what to set up on each side and never
  carries a key, as its ziti and zrok blocks already do (`provision-room.ps1:106-115`). The board shows a room's
  grants on its room card.
- **A room says what it can reach** in its hello: `Reach: [{to, route, name}]`. At attach the hub asks the room for a
  dry run of each grant it holds, with the grant's own name, never the hello's, so a room cannot make the dry run dial
  a host the grant does not name (`ssh -o BatchMode=yes <grant alias> true`, a zrok access, a ziti dial). Only a grant
  that is configured on the hub AND proven by the room counts.
- **It is a room capability the hub knows**, so it feeds the room-handoff check (`docs/rnd/room-handoff-design.md`
  section 2): a card that needs reach to X refuses a room with no proven grant to X. A hub op (section 0, item 1)
  needs no grant, because the hub runs it.
- **The hook rule.** ssh to another room is the same class as remote git: it leaves the room. `atrium reach` checks
  the grant, runs the named route, and goes through the permission gate, which shows the full command, not a
  summary. What comes back is untrusted input, like any tool result. Raw ssh and remote git are refused by a matcher
  in atrium's own PreToolUse hook (`atrium hook --event permission` already sees every Bash call), not by prefix
  deny rules, which miss `git -C x push`, `bash -c 'ssh ...'`, `/usr/bin/ssh`, `env ssh`, `scp`, `rsync -e ssh`,
  `sftp`, `git -c core.sshCommand=...` and any interpreter. The matcher is tested against that list. **It is a guard
  against mistakes, not containment.** Containment is the credential boundary of section 0.
- **What an agent may do there** is clint's call (question 2). The default here: read-only hub ops freely,
  machine-changing hub ops through the gate and never auto-approved, `atrium reach` through the gate, raw ssh
  refused by the matcher.

## 4. Cost, and which first for clint's rooms

| Piece | Owner | Size | Needs |
| --- | --- | --- | --- |
| Hub ops for the existing scripts, typed signatures and the gate (3.1) | @fabric | 2 to 3 days | a review of each script's remote-command building first |
| Grants on the hub, the hello field, the dry run | @fabric | 1 day | |
| `atrium reach` with the gate, and the remote-git and ssh matcher in atrium's hook | @runtime | 1 to 2 days | the test list of 3.2 |
| Direct ssh route | the operator | minutes per pair, n×(n-1) pairs | a key per source room |
| zrok private route | @fabric | 2 days | the share reservation exists. The access lifecycle (start, port, stop) is new |
| OpenZiti route | @fabric | 3 days or more | a controller admin outside atrium makes the service and policies |

**For clint's rooms: hub ops first.** They cover every case from 2026-10-01 and need no key off the hub. Then direct
ssh between the LAN rooms (sg3, m1mini, sg4), which all run as clint's operator account, if a case comes up that no op
covers. zrok private for a room off the LAN, since clint
already runs zrok. OpenZiti only for a room already on a ziti network (sgg).

## 5. Questions for clint

1. **Hub ops first, raw reach later?** **Default: yes.** Hub ops cover toolchain, provision, room-check and worktrees
   with no new key.
2. **What may an agent do on another room?** Hub ops only. Or `atrium reach` through the gate as well. Or nothing.
   **Default: read-only hub ops freely, machine-changing hub ops through the gate and never auto-approved, reach
   through the gate, raw ssh refused.**
3. **A key per source room, or per pair?** A key on a room is usable by every agent on that room with plain Bash,
   whatever the gate says. **Default: one key per source room,** authorized on each target it may reach, and only
   between machines run by the same operator account. Revoking a pair is removing one line on the target.
4. **Rooms have no remote-git or ssh guard at all.** No deny rule ships, and dotfiles' hook is not copied to rooms
   (`room-gate.ps1:13`). What stops a push today is that no credential is on the box. Should atrium's own PreToolUse
   hook carry a remote-git and ssh matcher, stated as a guard against mistakes, not containment? **Default: yes,**
   and @review owns its test list.
