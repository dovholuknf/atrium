package api

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/forge"
)

// A PULL REQUEST CARD READS EVERYTHING LOCALLY. A room never talks to a forge, and the permission hook refuses a
// fetch, a pull and gh, so a card told only to "read the diff, the description and the discussion" went and tried.
// The hub read the pull request and fetched its head and base, so the card is handed what that found: the base ref,
// the commit to diff against, the title, and a file holding the description and the discussion.

// prBriefFile is the file in the worktree that holds the description and the discussion.
const prBriefFile = ".atrium/PR.md"

// writePRBriefFile writes the description and discussion into the worktree, and keeps the folder out of git status.
// It reports whether the file is there to be named.
func writePRBriefFile(ctx context.Context, wt prWorktreeResult, url string) bool {
	if wt.PR == nil || wt.Path == "" {
		return false
	}
	dir := filepath.Join(filepath.FromSlash(wt.Path), ".atrium")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[atrium api] pr brief: %v", err)
		return false
	}
	body := strings.TrimSpace(wt.PR.Body)
	if body == "" {
		body = "(no description)"
	}
	disc := strings.TrimSpace(wt.PR.Discussion)
	if disc == "" {
		disc = "(no comments or reviews)"
	}
	text := fmt.Sprintf("# %s\n\n%s\n\nBase branch: %s\nMerge base: %s\n\n## Description\n\n%s\n\n## Discussion\n\n%s\n",
		wt.PR.Title, url, wt.PR.BaseRef, wt.PR.MergeBase, body, disc)
	if err := os.WriteFile(filepath.Join(dir, "PR.md"), []byte(text), 0o644); err != nil {
		log.Printf("[atrium api] pr brief: %v", err)
		return false
	}
	excludeFromStatus(ctx, wt.Path)
	return true
}

// excludeFromStatus adds .atrium/ to the repository's info/exclude, which no commit carries.
func excludeFromStatus(ctx context.Context, worktree string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-path", "info/exclude")
	cmd.Dir = filepath.FromSlash(worktree)
	nowindow.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.FromSlash(worktree), path)
	}
	if have, _ := os.ReadFile(path); strings.Contains(string(have), "\n.atrium/\n") || strings.HasPrefix(string(have), ".atrium/\n") {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("\n.atrium/\n")
}

// prBrief is what is added to the card's prompt: where to diff from, what the pull request is called, and the plain
// instruction that nothing is to be fetched. hasFile is whether writePRBriefFile made the file.
func prBrief(pr *forge.PR, hasFile bool) string {
	if pr == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nEverything you need is already local. Do not fetch, pull or run gh: this room has no forge access and the hook will refuse it.")
	fmt.Fprintf(&b, "\n\nTitle: %s\nBase branch: %s", pr.Title, pr.BaseRef)
	if pr.MergeBase != "" {
		fmt.Fprintf(&b, "\nMerge base: %s\nThe change is `git diff %s HEAD`, and `git log %s..HEAD` is its commits.",
			pr.MergeBase, pr.MergeBase, pr.MergeBase)
	}
	if hasFile {
		b.WriteString("\nThe description and the discussion are in " + prBriefFile + " in the worktree.")
	}
	return b.String()
}
