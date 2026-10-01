# Security read: room-to-room access spike (7bf10158, m1mini, 2026-10-01): HOLD

`docs/rnd/room-to-room-access-spike.md`. A spike, so the verdict is on its recommendation and its default answers
for clint, not on code. Nothing gets built.

The order is right. Hub ops before raw reach, off by default, grants per ordered pair held as names on the hub, and
a dry-run proof at attach: that is the least reach that covers 2026-10-01. It holds on how the hub op is shaped and
on what the hook rule is said to guarantee. Both change clint's defaults (Q2 and Q4), so they are fixed before it
goes to him.

## High 1: `atrium_room_op <room> <op> [args]` is command execution on the hub machine

The hub machine (sg4) holds the operator's ssh to every room, and is the most trusted box. The ops run its scripts.
The scripts take parameters no caller should set. Example: `scripts/room-git.ps1` takes `-Ssh` (the ssh
executable) and `-SshOption` (any `-o`, so `ProxyCommand=<anything>`), plus `-Target`, `-Path`, `-Repo` and `-Force`
(room-git.ps1:70-91). An op that passes `[args]` through lets an agent on any room run a command of its choosing on
sg4, or point ssh at any host, as the operator. A prompt-injected worker is enough.

Fix, in the spike's text:
- Each op is a fixed, typed signature on the hub: the op name, the room (resolved from the hub's inventory, never a
  host), and named fields validated by pattern (a worktree name as `[a-z0-9][a-z0-9._-]{0,63}`). The hub builds the
  argv. Nothing is passed through, and `-Ssh`, `-SshOption`, `-Target`, `-Path`, `-Repo` and `-Force` are never
  reachable.
- Argv, never a string: no op may concatenate a caller field into a remote shell command. Check each script's ssh
  invocation for that before it is exposed.
- **Not "freely" (Q2).** Read-only ops (`room-check` without `-Fix`, a toolchain report) may run freely. Ops that
  change a machine (`-Fix`, `provision-room`, `room-toolchain` install, worktree create or remove) go through the
  calling card's permission gate as `room_op sg3 provision`, and are never auto-approved by a standing rule or
  `global_auto`.
- The hub authorizes the caller by its room certificate and its card, and logs every op on the hub and the card.

## High 2: the hook rule is a guardrail, and the spike states it as a boundary

- **The deny rule the spike counts on does not ship.** `Bash(git push*)` appears only in
  `internal/claudeconf/claudeconf_test.go:75,99`, and no shipped config carries it. On m1mini `~/.claude/settings.json`
  has no `permissions`, and the room board's rules are `{"rules":null}` (GET /v1/rules on 7781, read-only). What
  stops a push from m1mini today is that its clone's remote is `https://github.com/...` and the machine holds no
  ssh key or `gh` login, plus the permission gate when it is on. It is not a rule. Correct section 1 and Q4.
- **A prefix deny rule is not a boundary.** `Bash(ssh*)` and `Bash(git push*)` miss `git -C x push`,
  `bash -c 'ssh …'`, `/usr/bin/ssh`, `env ssh`, `scp`, `rsync -e ssh`, `sftp`, `git -c core.sshCommand=…` and any
  interpreter. The dotfiles regex at least takes flags. Q4 should read: yes, atrium owns the remote-git and ssh
  refusal, as a matcher in atrium's own PreToolUse hook (`atrium hook --event permission` already sees every Bash
  call), tested against that list, and stated as a guard against mistakes, not containment.
- **The real boundary is where credentials sit.** Today a room machine holds no key to another, and that is the
  only thing that keeps an agent's Bash on m1mini off sg3. A direct-ssh grant puts a usable key on the source machine,
  and from then on every agent on that room can reach the target with plain Bash, whatever `atrium reach` and its
  gate say. The same goes for a ziti identity file or a zrok environment the agent's user can read. Say so plainly in
  section 2 and Q3. Raw reach is acceptable only between machines of the same trust: the same operator account, where
  agents already run as that operator. A key per source room (Q3) then means "every agent on that room". Prefer
  hub ops, which keep the key on the hub, for anything they can cover.

## Lower

- The attach dry run (`ssh -o BatchMode=yes <alias> true`) runs from the room at every attach. Use the alias from
  the hub's grant, not from the hello, so a room cannot make the dry run dial a host the grant does not name.
- `atrium reach` output comes back into an agent's context, so it is untrusted input like a tool result. Say that
  the gate shows the full command, not a summary.
- Revoking (section 2): dropping a grant leaves the key working. The board should show "grant removed, key still
  authorized on <target>" until the operator removes it.

## Verdict

HOLD on the recommendation: High 1 (typed, non-passthrough hub ops behind the gate for anything that changes a
machine) and High 2 (correct the deny-rule fact, a hook matcher not prefix rules, and credentials as the boundary).
Then it goes to clint. A re-read is sections 0, 1, 2 and 3 and Q2 to Q4. No trailers until OK, then `doc-ok`.

Quality: clear and well sourced. Section 1 is a good inventory, and "zrok and ziti replace the route, not the login"
is the right catch. The one factual slip, a deny rule that does not ship, is the one the security case rested on.
