The "your commits are already visible" line and the "nothing to push" answer of `atrium_git_push` now also work when
the launcher is on another room of the same machine (an orchestrator on one room, its worker on another, both in one
repository). The hub sends the launcher's working directory with a cross-room launch as `spawned_by_dir`, the room
keeps it on the card (migration `0096_task_spawned_by_dir`) and asks git only whether the two directories share a git
common dir, so a missing directory, a path that is not on this machine or any git error says nothing extra. The launch
text still never offers a push to the forge. (commit-visible-cross-room)
