# f-ssh-room-login: every room trusts every other room's key

Before this, no room had a key the others trusted, so every pair of rooms stopped at `Permission denied`. The wizard's
steps 5 to 7 (make a key, publish it, install the hub's set) were checks only.

## What it does

- **The login user is the room's own setting.** `remote_access.json` gets a `user` field. It is set by
  `atrium room remote-access on --user <name>` or `atrium ssh setup --user <name>`, kept across on and off, and defaults
  to `claude`. Only the room's CLI writes it.
- **Each room publishes** its login user, public key (ed25519 only, one line, no options), port and switch to the hub,
  on the existing `roomspec` link kind: `POST /_room/ssh`. The room's name comes from its certificate, so a room can
  only publish as itself. The hub keeps them in `room_ssh` (migration `0019_room_ssh`), which goes with the room row,
  so removing a room removes its key from the set.
- **The hub relays the set**: `GET /_room/ssh` for a room, and `GET /_hub/sshkeys` read only for the board and for
  `atrium ssh config add` on a machine with no room. Public keys and login names only.
- **Each room installs the set.** `atrium ssh setup` does steps 5 to 7 itself, and the room runs the same pass for steps
  6 and 7 every minute (`atrium room ssh setup sync` is one pass by hand). The set goes into the login user's
  authorized_keys between `# BEGIN atrium` and `# END atrium`, each key tagged `atrium:<room>`, filtered by this room's
  `from` list. Lines outside the block are written back byte for byte. A block with no END is refused. On Windows an
  administrator account gets `administrators_authorized_keys` and the `icacls` that sshd needs, and a room that is not
  elevated prints the command instead of writing it. A room with its switch off only publishes.
- **A room's own switch does not decide whether its key is trusted.** A room that only logs in to others has ssh off and
  still needs the others to trust it.
- **`atrium ssh config add <room>` writes `User <login>`** into the Host block when the hub knows it, so `ssh sg3`
  needs no user.

## What is not done

Not proven on a real Windows room: the Windows paths run against a fake machine only.

## Test plan

## @LETTER@. Rooms log in to each other with no hand key copying

### @LETTER@1. Publish and install

On two throwaway rooms joined to a throwaway hub, with sshd and `atrium room remote-access on` on both:

1. On each room run `atrium ssh setup --user <an account that exists>`.
2. Read `atrium ssh setup --check` on each.

**Expected:** each room made `~/.ssh/atrium_ed25519`, steps 5, 6 and 7 are ok, and each authorized_keys has an
`# BEGIN atrium` block holding the other room's key tagged `atrium:<room>`. Lines you had put in that file outside the
block are unchanged.

### @LETTER@2. A removed room's key leaves

1. Remove one room from the hub, then wait a minute or run `atrium room ssh setup sync` on the other.

**Expected:** its key is gone from the other room's authorized_keys block and nothing else changed.

### @LETTER@3. The login user in the Host block

1. On the first room run `atrium ssh config add <other room>`.
2. Run `ssh <other room> echo ok`.

**Expected:** `~/.ssh/atrium_config` has `User <the other room's login>` in the block and the command prints `ok` with
no user typed.
