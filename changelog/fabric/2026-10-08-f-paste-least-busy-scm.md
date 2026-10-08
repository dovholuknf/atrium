# A paste is placed only on a room that can open it

- A link pasted with the room left on "least busy" no longer fails on a room that has no scm folder. That room is passed over and the next least busy one is tried, for an open, a paste and a PR worktree. When no room can, one message names each room and why. Item f-paste-least-busy-scm.
- A room with `git.scm_root` unset uses `~/git` when that folder exists, and logs it. Provisioning now sets `scm_root` on every new room, and `atrium room set scm_root` is there for a stopped room.
- The launch dialog no longer stacks a second copy of a refusal when launch is pressed again.
