# Forge access alert wired, private PR fetch

A missing or logged out forge CLI now raises the board alert from the PR runner and from the pr-worktree verb, and
the alert clears when the forge next answers. The pr-worktree fetch of a private repo's PR head now reads through the
forge CLI's own git credential helper for that one fetch. No token is read, stored or logged.
