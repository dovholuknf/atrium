# Turning ssh on for a room: the lifecycle, the board wizard and a VM that can be reset

Status: design, 2026-10-14. Nothing here is built. Written by @rnd for backlog `f-room-ssh-wizard`, folding in
`f-ssh-room-login` and `f-ssh-wslrelay-loopback`. It builds on `docs/rnd/ssh-across-rooms.md` and
`docs/rnd/ssh-via-hub.md` and keeps what they decided unless it says so. For clint's review before anything is built.

## The problem

clint, 2026-10-14 16:23, in the backlog item: "sg3's sshd lacked sg4's key - that should just be part of what the room
does. let's engineer the claudevm so that we can test and retest adding rooms. wsl should have no bearing, other than
making sure when you ssh you're connecting to the right sshd. we just need to check that during room install or when
the room wants to be connected. room1 starts out just connected, no ssh enabled. user decides it wants ssh, so we check
sshd exists, where it is, all that. there should be a whole sexy wizard for all this the user would follow. don't
forget, sexy."

`f-ssh-room-login`, from the first ssh via hub test (sg4 to sg3) that reached sg3's sshd and failed login as `claude`:
"Each room has a setting, controlled by that room, for the user other rooms log in as. [...] Each room sends its ssh
pubkey to the hub. The hub relays the set of room pubkeys to every room, and each room adds them to the authorized keys
of its login user (on Windows admins that is `administrators_authorized_keys`). Adding a room needs no hand key copying
(adding rooms must be easy). [...] Only keys from rooms the hub knows. A removed room's key is removed on the next
relay."

`f-ssh-wslrelay-loopback`: "The tunnel reached `OpenSSH_8.9p1 Ubuntu` and login failed, though sg3's Windows sshd has
sg4's key. On sg3, port 22 listeners: `127.0.0.1` and `::1` are wslrelay (WSL localhost forwarding), `0.0.0.0` and `::`
are the Windows sshd. The more specific loopback bind wins. [...] so the door never silently opens onto WSL."

The ask in one line: a room joins with ssh off, a person turns it on by following a step by step wizard whose every step
checks something live and either fixes it or names the one thing the human must do, and the whole thing can be tested
from zero, over and over, on a VM that resets.

## What exists today

- **The tunnel is built.** A room can let the hub carry a TCP stream to its own sshd (`docs/rnd/ssh-via-hub.md`, built
  on `claude/ssh-via-hub`, `docs/changes/ssh-via-hub.md`). The switch is `remote_access.json` in the room's key
  directory (`internal/remoteaccess/remoteaccess.go`: `on`, `port`, `from`, `set_at`, `set_by`), written only by
  `atrium room remote-access on|off|log` at that machine (`internal/cli/remoteaccess_cmd.go`). `on` refuses inside a
  supervised session, as a speed bump. `ATRIUM_REMOTE_ACCESS=off` is a kill switch. `ssh-via-hub.md` decision 12 says
  there is NO board toggle, not even on the room's own loopback board, because that handler is also served to the hub
  over the link.
- **The host is hard coded.** The room dials `net.JoinHostPort("127.0.0.1", port)` (`internal/link/stream.go:505`).
  `State` has no address (`remoteaccess.go:38`, "Host is not here: it is always 127.0.0.1"). That is
  `f-ssh-wslrelay-loopback`.
- **The caller's config helper is built.** `atrium ssh config add <room>|--all` writes Host blocks to
  `~/.ssh/atrium_config` and one `Include atrium_config` line at the top of `~/.ssh/config` after a backup
  (`internal/sshcfg/sshcfg.go`). A block has `ProxyCommand ... ssh-proxy %h`, `HostKeyAlias <room>.atrium`,
  `ServerAliveInterval 30`, `CheckHostIP no`, and no `User` and no `IdentityFile`. It is run by a person and by nothing
  else.
- **No key moves anywhere.** `ssh-across-rooms.md` lists "moving, minting or installing ssh keys, and editing
  `authorized_keys`" as out of scope, and `ssh-via-hub.md` says the login is sshd's. A search of `internal` for a public
  key or `authorized_keys` finds only the sentence in `remoteaccess_cmd.go:81`. This design reverses that, and says so
  in the open question below.
- **The hub already carries per room things a room pulls by certificate.** The `roomspec` kind
  (`internal/link/roomspec.go`) is the model: the room dials, the hub takes the name from the certificate and nothing
  the room sends, a room reads and writes only its own, and the answer is bounded. The hub's store keeps it
  (`internal/hubstore`, `roomspec.go`).
- **Removing a room is a hub operation.** `atrium rooms rm <name>` (`internal/cli/atrium_rooms.go:512`, `--force` for a
  room never coming back) marks it removed in the hub's store. The room's machine is not told and keeps whatever it had.
- **A room's own account and keys.** `docs/room-accounts.md` recommends a room run as a standard user (`localai`, or
  `claude` on sg3 and m1mini), which holds that account's `.ssh`. An agent in the room runs as that account.
- **Provisioning is already a stepped script.** `scripts/provision-room.ps1` prints one line per step,
  `provision <step> <status> <detail>` (its `Step` function), takes `-Check` for a read only run and `-Remove` to undo,
  and is idempotent. A room on claudevm was provisioned and removed with it more than once
  (`docs/research/reports/f-room-bringup-REPORT.md`, "claudevm").
- **claudevm is a Hyper-V VM** (`docs/release/packaging.md`, "Windows MSI: built and installed on the Hyper-V VM
  claudevm") reached over `ssh`, and the standing rule is that autostart, stop, restart and `-Remove` tests run there
  and never on a real room (`docs/backlog/fabric/f-005.md`, `docs/rnd/multi-room-design.md`).
- **The board's Rooms tab** already draws the `ssh` badge and a `remote access` row in the room's facts
  (`internal/api/web/js/rooms.js`, around lines 489 to 515 and 694), fed by a `remote_access` object the room tells the
  hub at attach and on every change (`internal/link/room.go`, `watchAccess`).

## The lifecycle

```
 joined ──► ssh off ──► wizard ──► ssh on ──► (keys and config kept current) ──► ssh off ──► removed
   room has       a person turns it on        every pull       a person turns it off      hub forgets it,
   no keys,       at the machine               rewrites          removes the room's         the next pull
   no sshd                                     what it owns      managed lines              drops its key
                                                                                            everywhere
```

1. **Joined, ssh off.** A room joins exactly as today. It holds no ssh key, publishes none and changes nothing about
   the machine's sshd or `~/.ssh`. The Rooms tab says `ssh: off` with a button, "Set up ssh".
2. **The wizard** (below) takes it to on. The decision is a person's, at that machine, the way `remote-access on` is
   today. The wizard is the better face of that same decision.
3. **On.** Three things are kept current, and each is the ROOM's act on its own machine:
   - the room's public key and login user are on the hub (so others can reach it),
   - the keys of the rooms its `from` list allows are in its login user's authorized keys, inside one managed block,
   - its `~/.ssh/atrium_config` has a block for every other room that has ssh on.
4. **Off.** `remote-access off` (or the wizard's off) also removes the room's managed block from `authorized_keys`,
   withdraws its published key (`enabled: false`) and leaves `atrium_config` alone, since that file only holds ways to
   reach others.
5. **Removed.** `atrium rooms rm` deletes the room's key and user from the hub's store with the room row. On their next
   pull every other room gets a set without it, rewrites its managed block, and the key is gone from every
   `authorized_keys` and every `atrium_config`. "Removing a room removes its keys on the next relay", with no message
   sent to any machine, since a message would be a hub reaching into a machine. The removed room's own machine keeps its
   own managed block for others, which does nothing, because the hub will not carry a stream from a room it does not
   know.

## Options

### A. The hub pushes keys to rooms (rejected)

The hub writes each room's authorized keys by telling the room to. Simple, and no polling. Rejected because it lets the
hub, or anyone who reaches the hub's board, grant a login on any machine. The rule in `AGENTS.md` and in
`ssh-via-hub.md` is that the room decides. A room that pulls and filters through its own `from` list keeps that rule.

### B. The room pulls the set from the hub and writes its own files (chosen)

The room asks the hub for the key set over the link, filters by its own `from` list, and writes a managed block. The hub
only stores and serves public material, tied to certificate names. A stale or hostile hub can withhold a key (a login
fails) and cannot add a key for a room the machine's own `from` does not allow, because the room filters.

### C. A new key per pair of rooms (rejected for now)

Each pair holds its own key, so a leaked key reaches one machine. It is the better blast radius and costs N(N-1) keys, a
negotiation between rooms, and a much larger wizard. Kept in the open questions. The `from` list already gives most of it:
a room only installs the keys of the rooms it names.

### D. The board executes the fixes (rejected as stated)

A "Fix it" button on the board that runs commands on the room. The room's board handler is the one the hub proxies to, so
a button there is a button the hub, and everything behind the hub's board, can press. `ssh-via-hub.md` decision 12 rules
it out for the switch, and the same reasoning covers writing `authorized_keys`. Replaced by the arm token below.

## Recommendation

**B, driven by one step engine with two faces.** The engine is Go, in a new `internal/sshwizard`. It is a list of steps.
Each step has a `Check` that only reads and a `Fix` that may write, and returns a result: `ok`, `fixed`, `needs-human`
with the exact command or click, or `failed` with the reason. The CLI and the board are two views of it:

- **`atrium room ssh setup`** is the doing. A terminal wizard at the machine (colour, one line per step, a spinner while a
  check runs, the exact command printed when a person must act, then it rechecks). It is the successor to
  `remote-access on` and calls the same `remoteaccess.Save`.
- **The board wizard** is a stepper in the room's drawer on the Rooms tab, a modal with the same steps. Every step shows
  its live check, read only, on any board, including through the hub, because a check only reads. A step that needs a
  fix shows the exact command with a copy button and a "recheck" that polls. The board EXECUTES a fix only when the room
  has been **armed**: `atrium room ssh arm` run at the machine writes a short lived random token (15 minutes, mode 0600,
  in the room's key directory) and prints a `http://127.0.0.1:7781/...#arm=<token>` link. Fix buttons send the token and
  the room compares it with the file. The hub cannot read a file on the room's machine, so a request that came through
  the hub never carries it. This keeps decision 12's rule (nothing the hub can reach writes the switch) and gives the
  "sexy" board flow. Same posture as `remote-access on`: a speed bump against an agent on the machine, since an agent can
  read the file too.

### The steps

Each step names what it found and what it did. Windows, macOS and Linux differ only inside a step.

1. **Where am I.** Room attached to a hub with a certificate it can read, this OS, the account the room runs as, and
   whether that account is elevated. Fails with the room's `RejoinSentence` when there is no proven name.
2. **Is there an sshd.** Windows: the `sshd` service (`Get-Service sshd`) and `C:\Windows\System32\OpenSSH\sshd.exe`.
   macOS: Remote Login (`launchctl print system/com.openssh.sshd`). Linux: `systemctl is-active ssh sshd`. Not installed
   or not running is `needs-human` with the exact admin command (Windows:
   `Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0` then `Start-Service sshd` and
   `Set-Service sshd -StartupType Automatic`. macOS: `sudo systemsetup -setremotelogin on`. Linux:
   `sudo apt install openssh-server` or the distro's). The room's account is usually a standard user, so installing is
   never something the wizard does itself.
3. **Which sshd answers, and is it this machine's.** This is `f-ssh-wslrelay-loopback`, answered by identity and not by
   guessing from a process name.
   - Read this machine's sshd HOST KEY files, `%ProgramData%\ssh\ssh_host_*_key.pub` on Windows and
     `/etc/ssh/ssh_host_*_key.pub` elsewhere, which are public and readable by any user.
   - Read each candidate address's banner and host keys in Go (`golang.org/x/crypto/ssh`, stopping before auth):
     `127.0.0.1`, `::1`, and each non-loopback address of the machine. Match what each address returns against the files.
   - An address whose key matches is this machine's sshd. A banner that names another system, such as
     `OpenSSH_8.9p1 Ubuntu`, or a key that matches no file, is the wrong sshd, and the step says so plainly: "127.0.0.1:22
     is WSL's sshd (wslrelay), not Windows'. 192.168.1.147:22 is this machine's." On Windows the step also names the
     listener's owner (`Get-NetTCPConnection -LocalPort 22 -State Listen`, owning process `wslrelay`) as supporting
     evidence, which a standard user may read.
   - The chosen address goes into `remote_access.json` as `addr` (new field, default `127.0.0.1`), and
     `internal/link/stream.go:505` dials it. This is the first half of `f-ssh-wslrelay-loopback`. The host key
     fingerprint is stored beside it, and the room re-scans at attach and when status is read, so a room whose sshd
     changed (a WSL update taking port 22, an address that moved) shows `ssh: wrong sshd` on the Rooms tab and refuses
     streams until the wizard is run again, instead of silently opening onto the wrong machine.
   - The `status` form of `atrium room remote-access` prints the banner and fingerprint of what it dials, so "which sshd
     is this" is one command (the other half of that item).
   - A non-loopback address can move with DHCP. Step 3 prefers `0.0.0.0` answers by name (the machine's hostname) over a
     literal IP when the name resolves to a matching key, and says if it had to pin an IP.
4. **The login user.** Default is the account the room runs as. Editable (sgg is `localai`, most rooms `claude`).
   Checked: the user exists and is the one that owns the authorized keys file the next step writes. Stored in
   `remote_access.json` as `user`, sent to the hub with the key, and written into every other room's `User` line.
5. **This room's key.** A dedicated key, `~/.ssh/atrium_ed25519`, made with the machine's own `ssh-keygen -t ed25519 -N ""`
   when missing, and never the user's default key, so atrium never reads or publishes a key that does something else. The
   private half stays in that file and never leaves the machine and never enters atrium's memory. Only the `.pub` text is
   read. No passphrase, because an agent cannot type one, and the wizard says that is a decision.
6. **Publish.** The room posts its public key, login user, port and sshd host key to the hub on a new kind, shaped like
   `roomspec`, as a room named by its certificate (`POST /_room/ssh`). Bounded in size, one record per room, replaced on
   each post. The hub stores it with the room row (`internal/hubstore`, new table, added at the END of the migration
   slice).
7. **Install the others' keys.** The room asks `GET /_room/ssh-keys` and gets every room the hub knows that has
   `enabled`, a name, user and key. It keeps the ones its own `from` list allows (`hub`, names, `*`) and writes them into
   the authorized keys file of the login user inside one block:

   ```
   # BEGIN atrium (managed, do not edit between the markers)
   ssh-ed25519 AAAA... atrium:sg4
   ssh-ed25519 AAAA... atrium:m1mini
   # END atrium
   ```

   Only the block is ever rewritten. A line outside it is never read or touched. The file is `~/.ssh/authorized_keys`, or
   for an administrator account on Windows `%ProgramData%\ssh\administrators_authorized_keys`, found by reading
   `sshd_config` for `Match Group administrators` and `AuthorizedKeysFile`. That Windows file needs an ACL of
   Administrators and SYSTEM only, or sshd ignores it, and writing it takes elevation. The step is `needs-human` with the
   exact `icacls` and copy lines when the room's account is not elevated, and the wizard does everything else. A key line
   gets no options, no `command=`. The set includes this room's own key, so the self test in step 8 can log in to
   itself. The summary says so, and `--no-self-test` leaves it out.
8. **Prove a round trip.** From this machine, through the hub, to this machine: `ssh -o BatchMode=yes -o
   ConnectTimeout=10 <this room> echo atrium-ok`, using the generated key and the `atrium_config` block, over
   `atrium ssh-proxy`. It exercises the room's switch, the hub carry, sshd, the user, the key and the authorized file in
   one pass, with no second machine. A failure says which layer by the text it got: `Permission denied (publickey)` means
   the key or file, `Connection closed` before a banner means the carry, a banner naming another system means step 3.
9. **Reach the others.** For each other room online with ssh on, `ssh -o BatchMode=yes <room> echo atrium-ok`, one line
   each, green or red with the reason. A red because that room does not have this key yet says "it picks it up within a
   minute, or run its wizard", and the check retries until it lands. This is the live check clint described for "sg3's
   sshd lacked sg4's key": the answer is no longer a human copying a key.
10. **Write ssh config here.** `~/.ssh/atrium_config` gets a block per other room that has ssh on, now with
    `User <their login user>`, `IdentityFile ~/.ssh/atrium_ed25519`, `IdentitiesOnly yes`, and `UserKnownHostsFile
    ~/.ssh/atrium_known_hosts` holding the host key each room published, so a first connection prompts nobody and a tunnel
    that lands on the wrong sshd is a loud host key mismatch (the question left in `f-ssh-room-login`'s note, whether the
    tunnel lands on sg3's own sshd and not another). The `Include atrium_config` line is added once, with the consent
    `atrium ssh config add` already takes (the step asks, and does nothing without a yes).
11. **Turn it on and say so.** `remoteaccess.Save` with `on`, `port`, `addr`, `user`, `from`. The Rooms tab shows `ssh`,
    and the summary names what was changed on the machine: the key file, the managed block, the `Include` line.

After the wizard, a small loop in the room keeps 6, 7 and 10 current: at attach, when the hub tells the room its set
changed (a `note` on the control connection, a nudge and not data), and every ten minutes. It writes only what it owns:
the managed block, `atrium_config` and `atrium_known_hosts`. It never runs when ssh is off.

### What a person sees

The board's stepper is a vertical list with the step name, a status dot (grey, pulsing, green, amber, red), one line of
what was found ("Windows sshd 9.5, running, automatic start"), and for a step that needs a human the command in a code
block with a copy button. The summary line at the end is the plain sentence: "sg3 can be reached by sg4 and m1mini as
claude. 2 keys installed, 2 rooms configured." The CLI draws the same list. There is a `--check` form that prints the
list and changes nothing, which is also what the harness runs.

## The claudevm harness

The point is to add a room from zero and turn ssh on as a script that runs and reruns, with a pass or fail per step.

- **A snapshot that is the starting line.** On the Hyper-V host, a checkpoint of claudevm named `ssh-zero`: Windows, the
  OpenSSH CLIENT, the standard user `localai` and the work disk `provision-room.ps1` needs
  (`f-room-bringup-REPORT.md` records those), no sshd, no atrium. A second checkpoint `ssh-sshd` adds the OpenSSH server
  installed and running, for the path that is not about installing it. `Restore-VMCheckpoint` resets in seconds. The
  script waits for the guest to answer ssh again. Whether `Restore-VMCheckpoint` needs admin on sg4 is a question for
  clint (open question 3).
- **A runner script**, `scripts/vm/claudevm-ssh-e2e.ps1` (new directory `scripts/vm/`, a shared `vm-common.ps1`). One
  scenario per run, each starting from a restored checkpoint:
  1. `restore` the checkpoint and wait for ssh.
  2. `provision-room.ps1 localai@claudevm -Name claudevm-t1`, asserting its `provision <step> ok` lines. It is the same
     script a real room is made with.
  3. `atrium room ssh setup --check` over ssh, asserting step 2 reads `needs-human`, then the admin line is run as the
     guest's administrator over PowerShell Direct, then `--check` again, asserting `ok`.
  4. `atrium room ssh setup --yes` with the arm token, asserting every step `ok` or `fixed`.
  5. From the hub's machine, `ssh claudevm-t1 echo atrium-ok` through `atrium ssh-proxy`, asserting the text.
  6. `atrium rooms rm claudevm-t1`, the next pull on a second room, asserting the key is gone from its managed block.
  7. Print one table, step by step, pass or fail, with the first failing step's output. Exit non-zero on any fail.
- **Scenarios worth a checkpoint each.**
  - `zero`: no sshd (step 2 `needs-human`, then fixed, then green).
  - `wslrelay`: a tiny listener on `127.0.0.1:22` that writes `SSH-2.0-OpenSSH_8.9p1 Ubuntu`, with the real sshd on
    `0.0.0.0`. A loopback bind beats the wildcard bind, which is exactly what wslrelay does on sg3, so it reproduces
    `f-ssh-wslrelay-loopback` with no WSL in the VM. Step 3 must refuse 127.0.0.1 and choose the machine's address.
  - `admin`: the login user is an administrator, so the keys go to `administrators_authorized_keys` and the ACL step
    is `needs-human`.
  - `rekey`: a second room joined first. The new room's key must appear in the first room's managed block within a pull
    interval with no human step.
  - `removal`: step 6 above.
- **Linux, if cheap.** A throwaway WSL distro on the host, `wsl --import` from a saved tarball for every run and
  `wsl --unregister` after, is the cheapest reset there is and needs no Hyper-V. It is a Linux room with `systemd`, which
  covers the apt sshd path and `/etc/ssh/ssh_host_*_key.pub`. A Linux room in WSL also puts a second sshd on a machine
  that has a Windows one, which is the clean place to test step 3 with the real wslrelay. Run on sg4 only after clint's
  yes (open question 4). macOS has no cheap reset and stays a manual scenario on m1mini in `docs/test-plan.md`.
- **What it must never touch.** Never a real room. The scripts refuse a target that is not `claudevm`, the standing rule
  (`f-005.md`), and the hub it points at is a throwaway (`atrium run --no-room` on spare ports, as the f-005 proof did),
  never the live hub.

## Phase plan

Five phases, each one worker, one branch and one merge, each landing alone. Phase 1 is what unblocks the sg4 to sg3
failure clint hit and touches no hub code, so it can be built and merged before anything else is decided.

1. **Which sshd, the dial address, and the read only wizard.** One worker. Four pieces in this order:
   - `addr` in the switch: `remoteaccess.State` gains `Addr` (default `127.0.0.1`), `Load` validates an IP or a name,
     `internal/link/stream.go:505` dials it, `atrium room remote-access on --addr` and `status` print it plus the banner
     and host key fingerprint of what it reaches. Test beside `internal/link/stream_integration_test.go`: a listener on a
     second loopback address is dialled when `addr` names it.
   - `internal/sshwizard/whichsshd.go`: `Probe(addr)` returns banner and host keys from a Go ssh handshake that stops
     before auth, through an `Env` seam like `internal/guard/env.go` so a test can answer it. `HostKeys()` reads the `.pub` files,
     `Match` compares. Tests with a fake keyscan: matching key, an Ubuntu banner, no sshd.
   - `internal/sshwizard/steps.go`: the `Step` type and the eleven steps, each with a `Check` that reads only and a `Fix`
     that returns `needs-human` with its text. No `Fix` writes in this phase. Table tests, one per step per verdict, with
     the machine behind an interface.
   - `atrium room ssh setup --check` and the room's read only `GET /v1/ssh/wizard`, bounded at ten seconds.

   Done when: `go test ./internal/sshwizard/... ./internal/remoteaccess/... ./internal/link/...` passes, `--check` on a
   machine with sshd prints all eleven steps, a stray listener on `127.0.0.1:22` is reported as the wrong sshd and
   refused, and `docs/changes/room-ssh-wizard-phase1.md` carries the test plan.
2. **The hub's key store and kinds.** `room_ssh` table at the END of the migration slice (room, user, port, public key,
   host key, enabled, updated), deleted with the room. `POST /_room/ssh` and `GET /_room/ssh-keys` on the model of
   `roomspec.go`: name from the certificate only, bounded, no certificate refused. Done when: a room cannot read or post
   another's, `rooms rm` deletes the key, the set omits `enabled: false`.
3. **The fixes and the terminal wizard.** `internal/sshwizard/fix.go` (key generation, the managed block writer that
   leaves outside lines byte for byte, creates 0600, writes by temp file and rename, the Windows administrators file and
   its `needs-human` lines), `internal/sshcfg` gains `User`, `IdentityFile`, `IdentitiesOnly`, `UserKnownHostsFile`.
   `atrium room ssh setup` (interactive and `--yes`), `arm`, and `remote-access off` removing the block and withdrawing
   the key. Done when: writer tests in a temp home pass and the round trip step proves login on the claudevm.
4. **The pull loop.** At attach, on the hub's nudge, every ten minutes, writing only owned files. Done when: a test with
   a hub and two rooms over loopback adds a room and sees its key in the first room's block, removes it and sees it
   leave.
5. **The board wizard and the harness.** `ssh-wizard.js` stepper, copy buttons, the arm link, `ssh: wrong sshd` and the
   Set up button, write routes only on the room's loopback board and only with the arm token, headless unit and shots.
   Then `scripts/vm/` with the checkpoint notes, the runner, the five scenarios and the Linux leg, then
   `docs/fabric/hub-and-rooms.md`, the change note and a pointer from `ssh-across-rooms.md`. Done when: the runner
   prints a green table from `ssh-zero` and `wslrelay` on claudevm.

The harness can start earlier than phase 5 for the read only scenarios (`zero` and `wslrelay` need only phase 1), and
should, since it is how phase 1 is proven.

## Risks

- **A shared key gives every room's agents a login on every ssh enabled room**, as that room's login user. An agent on
  any room already holds that room's own keys and can ssh anywhere those keys work, so this is the same exposure, now
  uniform and automatic (`ssh-across-rooms.md`, "Security notes", says the same of per card limits). The `from` list is
  the control: a room installs only the keys it names, and `*` should not be the wizard's default for a room that is
  not a throwaway. The default offered is the rooms the hub already has, shown by name, to be ticked.
- **Writing `authorized_keys` is a new power.** It is confined to one managed block, to the login user's own file, and
  to a room whose person armed it. A bug that wrote outside the block would be a lockout or a grant, so the writer has
  the most tests of anything here.
- **The Windows administrators file is picky.** The wrong ACL means sshd ignores it and the failure says only
  `Permission denied (publickey)`. Step 8's text mapping and the `icacls` line exist for this, and the `admin` scenario
  in the harness is the proof.
- **A moved address.** A DHCP address that changes breaks a pinned IP. Step 3 prefers a name and re-scans, and the room
  refuses streams on a mismatch instead of guessing.
- **The hub holds public keys and login names.** Nothing secret, and the board is on loopback with no login, so anyone
  who can reach the board can read them. `roomspec` carries the same note and the same answer: nothing here may hold a
  credential, and a public key is not one.
- **Hub down.** The pull fails and the last installed block stays. A removed room's key then stays until the hub is back.
  It is the same bound as every hub dependent thing.
- **Hyper-V checkpoints and time.** A restored VM has a stale clock and a stale TLS and room certificate state. The
  runner sets the clock and re-joins the room, so a checkpoint never holds a room's credentials (it is taken before
  `provision`).

## Decided

These were open and are closed. Recorded so nobody reopens them while building.

- Keys go on the hub and a managed block goes in each room's `authorized_keys`, with the room filtering by its own
  `from` list. Clint asked for exactly that. This reverses the out of scope line in `ssh-across-rooms.md`, which phase 5
  amends.
- One key per room, not one per pair. Revisit only if a `*` `from` turns out common.
- The board runs fixes after a local `arm`. Without arm the board wizard is read only plus copy buttons. A request that
  came through the hub never carries the token, so decision 12 of `ssh-via-hub.md` still holds.
- The wizard never installs sshd itself: the room's account is a standard user, so that step is `needs-human`.
- The generated key has no passphrase and is dedicated (`~/.ssh/atrium_ed25519`), never the user's own.
- The self test includes this room's own key, with `--no-self-test` to leave it out.
- The `from` default offered is the rooms the hub already has, ticked by name. Never `*` for a room that is not a
  throwaway.

## Open for clint

Every question carries a recommended answer, and the recommendation is what gets built if nobody answers.

1. May the harness restore claudevm checkpoints, and who runs it? `Restore-VMCheckpoint` needs rights on sg4. Recommended:
   yes, run from an elevated shell by clint or by a scheduled task under an account in Hyper-V Administrators that is
   used for nothing else, with the script refusing any VM that is not `claudevm`. The read only `--check` scenarios need
   no rights at all.
2. May the Linux leg import and unregister a throwaway WSL distro on sg4, where wslrelay already owns `127.0.0.1:22`?
   Recommended: yes, a distro named `atrium-ssh-t1` only, deleted at the end of each run. It is also the one place the
   real wslrelay can be tested instead of the fake listener. Until clint says yes, phase 5 ships the `wslrelay` fake
   scenario only.
3. Should phase 1 ship before the hub pieces? Recommended: yes. Phase 1 changes nothing on the hub, fixes the sg4 to sg3
   dial by letting `addr` be set by hand, and gives the wizard's checks on their own.
4. What does the room do when the hub's key set changes while ssh is off? Recommended: nothing. The loop never runs
   when ssh is off, and turning ssh on pulls once at once.
5. Does the wizard add the `Include atrium_config` line without asking on a later run? Recommended: no. It asks once and
   records the consent in `remote_access.json`, after which the loop may keep the file current but never adds a second
   `Include`.
6. Where does the arm link open? Recommended: the room's loopback board only. Remote use goes through the terminal
   wizard, because a link that opens elsewhere is a link the hub could be asked to hand out.
