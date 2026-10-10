Closing a card no longer reports a worktree freed while an ignored folder such as node_modules stays on disk. The close
removes what git left, long Windows paths included, measures the size with ignored files, and says what is left on the
row when it cannot. A closed PR now reads closed on the pulls tab and leaves the open filter, and a closed card leaves
the terminals list (r-close-leaves-worktree).
