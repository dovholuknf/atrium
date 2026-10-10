`atrium_proc_start {argv, cwd, name}` (`POST /v1/tasks/{id}/proc`) starts a long-running process for a card and
records it as a `proc` on the card's inventory in one step. It runs in the background in a group of its own, its
output goes to a log named for the card's short id, and `cwd` must be inside the card's worktree or the scratch
folder. Closing the card stops it with every process it started. Item r-card-procs-ports, phase 9 first slice.
