# ssh wizard, steps 1 to 4: the address, which sshd, the read only checks

Design: `docs/rnd/room-ssh-wizard-design.md`. Backlog `f-room-ssh-wizard`, folding in `f-ssh-wslrelay-loopback`.

What this adds, and nothing here writes a key, an `authorized_keys` or an ssh config:

- `addr` in `remote_access.json`, default `127.0.0.1`. The room dials it for its own sshd, where it used to dial
  `127.0.0.1` always. `atrium room remote-access on --addr <ip or name>` sets it, and `status` prints the banner and host
  key fingerprint of what the room dials, and whether that is this machine's own sshd.
- `internal/sshwizard`: which sshd answers, decided by host key identity (`ssh-keyscan` against the machine's own
  `ssh_host_*_key.pub`), not by a process name. A banner that names another system is supporting text only.
- The eleven steps as read only checks, and `atrium room ssh setup --check` (also `atrium ssh setup --check`) with
  `--json`. `GET /v1/ssh/wizard` on the room serves the same result. Steps that read the hub's ssh record, which does
  not exist yet, say so and are skipped. The route leaves the self test off unless `self_test=1`, so a reload or
  prefetch never makes the room ssh into itself. The CLI still runs it by default.

## Test plan

## @LETTER@. ssh wizard checks

### @LETTER@1. Which sshd, on a machine where loopback is not sshd's

1. On a Windows machine with WSL, so `127.0.0.1:22` is wslrelay and the Windows sshd holds the wildcard bind, run
   `atrium room ssh setup --check`.

**Expected:** step 3 says `127.0.0.1:22 is WSL's sshd (wslrelay), not Windows'` and names the machine's own address as
the sshd to use. Nothing on the machine changes.

### @LETTER@2. The room dials the address

1. Run `atrium room remote-access on --addr <the address step 3 named> --from hub`.
2. Run `atrium room remote-access`.
3. From another machine, `ssh <room> echo ok` through the hub.

**Expected:** status prints `dials <addr>:22: <banner>, ssh-ed25519 SHA256:..., this machine's own sshd`, and the
stream reaches the Windows sshd and not WSL's.

### @LETTER@3. A step that needs a person

1. On a machine with no sshd, run `atrium room ssh setup --check`.

**Expected:** step 2 is `needs you` and prints the exact admin command. The exit code is zero, and `--json` carries the
same steps.
