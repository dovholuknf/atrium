package gitsync

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// commonDirOf is the absolute git common directory of dir, "" when dir is not in a repository.
func commonDirOf(ctx context.Context, r *Runner, dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	out, err := r.Git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	return filepath.Clean(strings.TrimSpace(out))
}

// SharesGitDir says whether two directories are in the same repository: their git common directories are the same
// folder. A worktree of a checkout shares it with that checkout, so a commit made in one is at once visible in the
// other. Best effort: any error is false.
func SharesGitDir(ctx context.Context, r *Runner, a, b string) bool {
	ca, cb := commonDirOf(ctx, r, a), commonDirOf(ctx, r, b)
	if ca == "" || cb == "" {
		return false
	}
	if ca == cb {
		return true
	}
	sa, err := os.Stat(ca)
	if err != nil {
		return false
	}
	sb, err := os.Stat(cb)
	return err == nil && os.SameFile(sa, sb)
}

// PointsAtOtherForwarder says whether the hub remote of the clone at dir pushes to a room's forwarder on this machine
// that is NOT the one at forwarderBase: a loopback URL under the hub's path. That is what a worktree of another
// room's checkout carries, because the remote lives in the shared .git/config.
func PointsAtOtherForwarder(ctx context.Context, r *Runner, dir, forwarderBase string) bool {
	for _, n := range []string{HubRemote, HubRemoteAtrium} {
		out, err := r.Git(ctx, dir, "remote", "get-url", "--push", "--all", n)
		if err != nil {
			continue
		}
		found := false
		for _, l := range strings.Split(out, "\n") {
			if l = strings.TrimSpace(l); l == "" {
				continue
			}
			u, perr := url.Parse(l)
			if perr != nil || !isLoopback(u.Hostname()) || !strings.HasPrefix(u.Path, "/git/") ||
				strings.HasPrefix(l, forwarderBase) {
				return false
			}
			found = true
		}
		if found {
			return true
		}
	}
	return false
}

func isLoopback(h string) bool {
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}
