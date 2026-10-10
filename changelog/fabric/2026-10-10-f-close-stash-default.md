# Closing a card stashes by default, and the hub clears old stashes

The close dialog's answer for a worktree with work nowhere else is now stash, with the focus on it so Enter takes it. A room that cannot stash says so and defaults to keep. `atrium close --yes` with no answer stashes (keeps when it cannot), and delete is never a default. The hub removes `stash/<card>/<branch>` refs older than `git.stash_retention_days` (30, 0 for never), logging each, and the close names the stash and the day it goes. (f-close-stash-default)
