A worker launched in a git worktree of its launcher's own checkout is now told at launch that its commits are already
visible to the launcher and that it should not push, and `atrium_git_push` from such a worker, when the shared hub
remote points at another room's forwarder on the same machine, answers "nothing to push: <launcher> sees this commit
already" as a success instead of a refusal. The launch text never suggests `git push origin`. (commit-is-visible)
