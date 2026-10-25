## Test plan

## @LETTER@. A missing hooks folder is said, not silent

### @LETTER@1. Start new work with no hooks folder

1. Point `worktree_hooks_dir` at a folder that does not exist, or leave it unset with no `~/.atrium/hooks`.
2. Start new work from the launch dialog, or paste a repo link.
3. The progress strip and the toast both say the hooks folder does not exist and name it.

### @LETTER@2. Create the folder

1. Create the folder and start new work again on a new name.
2. No hooks-folder warning shows.
