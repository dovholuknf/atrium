package api

import "github.com/dovholuknf/atrium/internal/wtfree"

// WHAT GIT LEAVES. A close does not call a worktree freed until its path is gone. The cleanup is shared with a cull,
// and lives in internal/wtfree.

// worktreeListed is whether git still names path as a worktree of repo.
func worktreeListed(repo, path string) bool { return wtfree.Listed(repo, path) }

// removeLeftover deletes what is still at path once git no longer lists it as a worktree. An error says what is left.
func removeLeftover(repo, path string) error { return wtfree.RemoveLeftover(repo, path) }
