A worker launched in a worktree of another room's checkout now ends with exactly one `remote.hub.url`, its own room's,
so `git fetch hub` and pushes pass the guard. The other room's url moves into its worktrees' own config. Worktrees that
already had two urls are repaired on the next launch. Item: r-shared-disk-hub-url-twice.
