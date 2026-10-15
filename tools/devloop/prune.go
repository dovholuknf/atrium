package main

import (
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// worktreeFor reads `git worktree list --porcelain` and returns the path checked out on branch.
func worktreeFor(porcelain, branch string) string {
	var path string
	for _, line := range strings.Split(strings.ReplaceAll(porcelain, "\r", ""), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return path
		}
	}
	return ""
}

// sshRun runs one command on a room's host. The room name is the ssh host unless --ssh says otherwise.
func sshRun(host, command string) (string, error) {
	b, err := exec.Command("ssh", host, command).CombinedOutput()
	s := strings.TrimSpace(string(b))
	if err != nil {
		return s, fmt.Errorf("ssh %s: %w: %s", host, err, s)
	}
	return s, nil
}

func cmdPrune(args []string, out io.Writer) error {
	pos, flags := splitArgs(args, "room", "card", "ssh", "repo", "hub", "remote-repo")
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	room := fs.String("room", "", "the room that holds the card and worktree")
	card := fs.String("card", "", "card to exit, room~id or a bare id")
	host := fs.String("ssh", "", "ssh host for the room, default the room name")
	repo := fs.String("repo", ".", "the local checkout")
	remote := fs.String("remote-repo", ".", "the repository on the room, as seen from its ssh login directory")
	hub := fs.String("hub", defaultHub, "hub base URL")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if len(pos) != 1 || *room == "" {
		return fmt.Errorf("usage: prune <branch> --room <room> [--card <room~id>]")
	}
	branch := pos[0]
	h := *host
	if h == "" {
		h = *room
	}
	if *card != "" {
		r, id := splitCard(*card, *room)
		if err := exitCard(*hub, r, id); err != nil {
			return err
		}
		step(out, "exited card %s~%s", r, id)
	}
	list, err := sshRun(h, "git -C "+*remote+" worktree list --porcelain")
	if err != nil {
		return err
	}
	wt := worktreeFor(list, branch)
	if wt != "" {
		if _, err := sshRun(h, fmt.Sprintf("git -C %s worktree remove --force \"%s\"", *remote, wt)); err != nil {
			return err
		}
		step(out, "removed worktree %s on %s", wt, *room)
	} else {
		step(out, "no worktree on %s for %s", *room, branch)
	}
	if _, err := sshRun(h, fmt.Sprintf("git -C %s branch -D %s", *remote, branch)); err != nil {
		return err
	}
	step(out, "deleted %s on %s", branch, *room)
	g := realGit{}
	if _, err := g.run(*repo, "branch", "-D", branch); err != nil {
		return err
	}
	step(out, "deleted local %s", branch)
	if _, err := g.run(*repo, "push", "hub", "--delete", branch); err != nil {
		return fmt.Errorf("hub refused the branch delete, not retried: %w", err)
	}
	step(out, "deleted hub %s", branch)
	return nil
}
