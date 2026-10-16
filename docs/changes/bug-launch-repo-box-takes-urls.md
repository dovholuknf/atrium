## Test plan

## @LETTER@. A repository link in the new agent dialog

### @LETTER@1. Pasted into the working directory box

1. Open the new agent dialog and paste `https://github.com/dovholuknf/dotfiles/tree/main/claude/skills/slop-review`
   into the working directory box.
2. The box empties, the repo panel opens with the link in its filter, the repo box shows
   `github.com/dovholuknf/dotfiles (will be fetched)` and `from` says `main`.
3. Press start new work with no name. The hub fetches the repo, the room clones it, and a session starts in a new
   `claude/work-<yymmdd>-<hhmm>` worktree off `main`.

### @LETTER@2. Typed in the repo filter

1. Paste `git@bitbucket.org:org/repo.git`, `ssh://git@host/org/repo`, `https://host/org/repo/pull/3` and
   `https://gitlab.com/g/s/r/-/tree/dev/docs` in turn.
2. Each reduces to host/org/repo. A repo this room or the hub holds is selected in the repo box and never shows as a
   fetch. The box never keeps an unrelated earlier repo.
3. Change the repo box by hand: start new work is disabled until it matches what was typed again.
