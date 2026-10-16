## Test plan

## @LETTER@. remote-access names its directory

### @LETTER@1. A wrong --dir

1. Run `atrium room remote-access on --dir <an empty folder>` on a machine with a running room.

**Expected:** an error naming the folder and the running room's directory, and nothing written in the folder.

### @LETTER@2. The right directory

1. Run `atrium room remote-access`, then `on`, then `off`, with no `--dir`.

**Expected:** each answer has a `room directory:` line marked as the running room's. With `--dir` set to a different
real room directory, a note says the running room uses another one.

### @LETTER@3. No room for this account

1. As a Windows user with no room, run `atrium room remote-access`.

**Expected:** an error saying no room certificate was found and where it looked.
