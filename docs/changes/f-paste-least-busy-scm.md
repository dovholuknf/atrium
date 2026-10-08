# f-paste-least-busy-scm

A paste in the launch dialog with the room on "least busy" answered "this room has no scm folder" (gitsync.ErrNoSCMRoot)
twice. `git.scm_root` was empty on all four rooms, and the hub placed the paste on the least busy room without asking
whether that room could clone anything.

## What changed

- **Placement passes over a room with no scm folder.** A room that cannot clone or open now answers `422` with code
  `no_scm_root` (an open, a `/v1/prs` paste, a PR worktree, a scratch folder). The hub's `retryDeaf`
  (internal/link/recogniseroute.go), which already took a paste from a room on an old build to the next least busy
  room, takes this answer the same way. `placePRRoomExcept` leaves out the rooms that already refused. The claim a PR
  worktree made follows the paste to the room that takes it.
- **No room can: one answer.** The hub replaces the last room's sentence with one `422 no_scm_root` that names every
  room and what it said, or why it was not asked (old build, not taking new work). A room that holds the repo or the
  worktree already has its row or its folder, so it never answers this.
- **The default.** A room with `git.scm_root` unset uses `<home>/git` when that folder exists, and logs the choice
  once. It is DERIVED ON EVERY READ and not written as the setting. Written, it would have changed a setting nobody
  asked to change, hidden from the settings page that the operator never chose it, and gone stale if the folder is
  removed. Derived, a `~/git` made later is found with no restart. An explicit setting always wins. With neither, the
  room answers `no_scm_root` and is passed over.
- **Provisioning.** `scripts/provision-room.ps1` runs `atrium room set scm_root` after the join (the shared folder when
  there is one, else `~/git`). `atrium room set|get scm_root` is new, for a stopped room.
- **The error twice.** `busyWhile` (js/core.js) put the refusal before the dialog footer, outside the scope it cleared
  on the next press, so pressing launch again stacked a second copy. It now clears the dialog's own.

## Test plan

## @LETTER@. A paste lands on a room that can open it

### @LETTER@1. The least busy room has no scm folder

1. On a hub with two rooms, leave `git.scm_root` unset on the least busy one and make sure it has no `~/git`.
2. Paste a PR link in the launch dialog with ROOM on "least busy" and press launch.

**Expected:** the card starts on the other room. The first room is not asked to clone.

### @LETTER@2. No room can

1. Do the same with no scm folder on any room.
2. Press launch twice.

**Expected:** one line names each room and why. Pressing again replaces it and does not stack a second.

### @LETTER@3. The default

1. On a room with no `git.scm_root` and a `~/git`, paste a PR link on that room.

**Expected:** the clone is under `~/git`, the room's log says the folder was used, and the setting still reads empty.
