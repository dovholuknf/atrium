Atrium no longer sets `remote.origin.pushurl = atrium-refused://origin-push` on clones it makes. It lived in the
shared `.git/config`, so the operator's own `git push` failed in every worktree of the clone. A sync or
`atrium_git_clone` now removes that exact value from a clone that still has it, and leaves any other pushurl alone.
The hook that keeps agents to the hub remote is unchanged. f-drop-origin-pushurl-guard.
