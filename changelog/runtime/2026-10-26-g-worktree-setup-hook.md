A worktree made by open, by a pull request link or by the providers dialog now runs the repo's setup hook from the
hooks folder, as new work does. A hook that fails or a missing folder is said in the progress and never fails the make.
(g-worktree-setup-hook)
