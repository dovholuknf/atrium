## Test plan

## @LETTER@. Room spec: `atrium room setup`

### @LETTER@1. Plan, then apply, then rerun

1. Write a `room.yaml` (version 1) for the machine: `os`, `account` (the login you are running as), an absolute
   `work_root` whose parent exists, and a `packs` entry for `claude`.
2. Run `atrium room setup --spec room.yaml --plan`, then `--apply --hub-addr <hub>:<port>`, then `--plan` again.

**Expected:** the plan lists `todo` rows and creates nothing, not even the lock. The apply makes the folders, points the
caches at the work root, sets the room's settings (or says `warn` that a running room holds them), installs the pack and
writes `~/.atrium/room.lock`. The last plan, and a second apply, say `ok` on every row and change nothing.

### @LETTER@2. A parent the account cannot examine

1. Take away the account's right to examine a parent of the work root (Windows: remove its `(RA,REA)` entry; Unix: remove
   its `setfacl` entry), then run `--plan`.

**Expected:** exit 13, a `work-root human` row, and the lines an administrator runs, which grant examining and nothing
more. Nothing else is touched until it is fixed.

### @LETTER@3. A spec that is not safe

1. Put `token: x` anywhere in the spec, or a `work_root` of `V:/`, or a pack `branch: -x`.

**Expected:** exit 1 and a message naming the field. Nothing is read from the machine.
