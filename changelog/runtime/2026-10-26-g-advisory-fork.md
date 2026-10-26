Advisory fork links open: a branch or pull request on a security advisory's temporary fork adds the remote `ghsa-<id>`,
fetches the branch or the pull request's head, and makes the worktree on it. The card's close removes the remote once
no other card holds it. The advisory body is not written to the worktree yet. g-advisory-fork
