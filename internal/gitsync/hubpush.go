package gitsync

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The remote names are HubRemote and HubRemoteAtrium (hubremote.go).
const extraHeaderKeyStart = "http."

// ExtraHeaderKey is the git config key that scopes a header to the room's forwarder: `http.<url>.extraHeader`
// with the url `http://127.0.0.1:<agent port>/git/`. NEVER a bare http.extraHeader, which goes to every remote.
func ExtraHeaderKey(forwarderBase string) string {
	return extraHeaderKeyStart + forwarderBase + ".extraHeader"
}

// PushEnv is the git environment that carries a card's token to the forwarder and to nothing else. Three entries
// starting at `first`, the index after any GIT_CONFIG entries the environment already has.
func PushEnv(forwarderBase, token string, first int) []string {
	i := fmt.Sprint(first)
	return []string{
		"GIT_CONFIG_COUNT=" + fmt.Sprint(first+1),
		"GIT_CONFIG_KEY_" + i + "=" + ExtraHeaderKey(forwarderBase),
		"GIT_CONFIG_VALUE_" + i + "=" + HeaderCardToken + ": " + token,
	}
}

var plainBranch = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// CheckPushBranch says why a branch name is not a plain branch push, or "". Nothing but a branch name is taken:
// no `+`, no `:`, no refs/ path, no HEAD, no tag, no `--` option.
func CheckPushBranch(b string) string {
	switch {
	case b == "":
		return "say which branch to push"
	case strings.HasPrefix(b, "+") || strings.Contains(b, ":"):
		return "a push here is one plain branch name. no force, no `+`, no `:` refspec, no delete"
	case strings.HasPrefix(b, "-"):
		return "a branch name cannot start with a dash"
	case strings.HasPrefix(b, "refs/") || b == "HEAD" || strings.EqualFold(b, "tags"), strings.HasPrefix(b, "tags/"):
		return "push a branch by its name, like fix/x. tags and refs paths are not pushed from here"
	case !plainBranch.MatchString(b) || strings.Contains(b, "..") || strings.HasSuffix(b, "/") ||
		strings.HasSuffix(b, ".lock") || strings.HasSuffix(b, ".") || strings.Contains(b, "//") ||
		strings.Contains(b, "@{"):
		return b + " is not a branch name git accepts here"
	}
	return ""
}

// HubPushRemote is the remote a push goes through: `hub` when its push URL, AFTER every url.*.insteadOf and
// pushInsteadOf rewrite (which is what `git remote get-url --push` prints, not the raw remote.<name>.url), is the
// forwarder's, else `atrium-hub` under the same test. A clone whose own `hub` points elsewhere is therefore pushed
// through `atrium-hub` if the room added it, and refused if it did not.
func HubPushRemote(ctx context.Context, r *Runner, dir, forwarderBase string) (string, error) {
	_, name, err := hubPushTarget(ctx, r, dir, forwarderBase)
	return name, err
}

// hubPushTarget is HubPushRemote and the one URL it pushes to. `get-url --push` alone prints only the FIRST push
// URL, and git pushes to every one, so every line `--all` prints has to be the forwarder's and there has to be one.
func hubPushTarget(ctx context.Context, r *Runner, dir, forwarderBase string) (url, name string, err error) {
	want := forwarderBase + strings.TrimPrefix(HubRemotePrefix, "/git/")
	var seen []string
	for _, n := range []string{HubRemote, HubRemoteAtrium} {
		out, gerr := r.Git(ctx, dir, "remote", "get-url", "--push", "--all", n)
		if gerr != nil {
			continue
		}
		var urls []string
		for _, l := range strings.Split(out, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				urls = append(urls, l)
			}
		}
		ok := len(urls) == 1 && strings.HasPrefix(urls[0], want)
		if ok {
			return urls[0], n, nil
		}
		for i, u := range urls {
			urls[i] = sanitizeURL(u)
		}
		seen = append(seen, n+" pushes to "+strings.Join(urls, " and "))
	}
	if len(seen) == 0 {
		return "", "", fmt.Errorf("this clone has no hub remote. the room adds one when it syncs the repository")
	}
	return "", "", fmt.Errorf("this clone's %s, not only this room's hub forwarder. it is refused so a card cannot push to another server", strings.Join(seen, " and "))
}

// sanitizeURL drops any credential from a URL before it goes in a sentence.
func sanitizeURL(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if j := strings.IndexAny(rest, "/"); j >= 0 {
			if at := strings.LastIndex(rest[:j], "@"); at >= 0 {
				return u[:i+3] + rest[at+1:]
			}
		}
	}
	return u
}

// PushToHub pushes one plain branch from the clone at dir to the room's hub forwarder, with the card's token, and
// answers git's own report. It is the push `git push hub <branch>` is, without a shell, under the same rules: the
// remote's push URL after every rewrite has to be the forwarder's, and only a plain branch is pushed.
//
// Nothing the clone's config says can widen it: the refspec is built here, every push URL has to be the forwarder's,
// its proxy settings are cleared, push.followTags is off, the remote's mirror setting is off, and the repository's own pre-push hook is not run (it is code from the working tree).
func PushToHub(ctx context.Context, r *Runner, dir, forwarderBase, token, branch string) (string, error) {
	if why := CheckPushBranch(branch); why != "" {
		return "", fmt.Errorf("%s", why)
	}
	target, remote, err := hubPushTarget(ctx, r, dir, forwarderBase)
	if err != nil {
		return "", err
	}
	spec := "refs/heads/" + branch + ":refs/heads/" + branch
	out, err := r.GitEnv(ctx, dir, PushEnv(forwarderBase, token, 0),
		"-c", "push.followTags=false", "-c", "remote."+remote+".mirror=false",
		// The clone's own config does not get to put a proxy between the token and the forwarder. A remote's proxy
		// and a url-scoped one beat http.proxy, so both of those are cleared, the url-scoped one at the exact URL,
		// which is the most specific key there is.
		"-c", "remote."+remote+".proxy=", "-c", "http."+target+".proxy=", "-c", "http."+forwarderBase+".proxy=",
		"push", "--porcelain", "--no-verify", "--no-recurse-submodules", remote, spec)
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && strings.TrimSpace(ge.Stderr) != "" {
			return strings.TrimSpace(out + "\n" + ge.Stderr), err
		}
		return out, err
	}
	return strings.TrimSpace(out), nil
}
