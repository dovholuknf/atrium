package gitsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A pull request's head in the hub's store, for rooms. See docs/rnd/scm-forge-design.md, Built.
//
// A ROOM NEVER FETCHES FROM A FORGE. The hub holds the repository in its store (Hold) and fetches a PR's head into
// `refs/atrium/pr/<N>` there (FetchPR). A room reads both through its link like any other ref of the store. Every
// transfer here is a fetch from the forge into the hub, and nothing is ever pushed to a forge.

// PRRefPrefix is where the store keeps pull request heads.
const PRRefPrefix = "refs/atrium/pr/"

// Hold makes sure the store holds the repository at a forge URL, as Init does, and answers its name. It is what a
// room's PR worktree or clone asks for, so a repository a room needs is in the store before the room reads it.
func (s *Store) Hold(ctx context.Context, rawURL string) (string, error) {
	res, err := s.Init(ctx, rawURL)
	if err != nil {
		return "", err
	}
	return res.Repo, nil
}

// FetchError is a fetch of a PR head that failed. Auth is true when git's words say the forge wanted a credential
// it did not get, which is the forge access alert's business.
type FetchError struct {
	Auth bool
	Msg  string
}

func (e *FetchError) Error() string { return e.Msg }

// FetchPR fetches refspec from remote into dst (under PRRefPrefix) in the store's copy of name, over https only and
// with git's object checks on. helper is the git credential helper line of the forge's own CLI, or "" for none: it is
// set for this one command, after every configured helper is reset, so the CLI's login answers and no token is read,
// stored or put in an argv. When the store's main is still empty and base is the PR's base branch, base is fetched
// into main too, which is how a private repository the seed could not read gets its main.
func (s *Store) FetchPR(ctx context.Context, name, remote, refspec, dst, base, helper string) (string, error) {
	ref, err := ParseName(name)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(dst, PRRefPrefix) {
		return "", refuse("a pull request's head goes under %s", PRRefPrefix)
	}
	if _, err := s.git(ctx, "", "check-ref-format", dst); err != nil {
		return "", refuse("%s is not a ref git will take", dst)
	}
	if strings.TrimSpace(refspec) == "" || strings.HasPrefix(refspec, "-") || strings.ContainsAny(refspec, ": \t\n") {
		return "", refuse("the forge named no head to fetch")
	}
	dir, ok := (&Receiver{h: s.h}).dir(ref)
	if !ok {
		return "", refuse("the store does not hold %s", name)
	}
	l := s.h.lock("store:" + strings.ToLower(ref.Name()))
	l.Lock()
	defer l.Unlock()

	bound := s.SeedTimeout
	if bound <= 0 {
		bound = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	specs := []string{"+" + refspec + ":" + dst}
	if base = strings.TrimSpace(base); base != "" && s.tip(ctx, dir) == "" {
		if _, err := s.git(ctx, "", "check-ref-format", "refs/heads/"+base); err == nil {
			specs = append(specs, "+refs/heads/"+base+":"+MainRef)
		}
	}
	if _, err := s.git(ctx, dir, prFetchArgs(remote, helper, specs)...); err != nil {
		return "", s.fetchFailed(ref, err)
	}
	out, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", dst+"^{commit}")
	if err != nil {
		return "", &FetchError{Msg: "the fetch finished and " + dst + " is not there"}
	}
	s.h.audit("", "git-store-pr", fmt.Sprintf("%s %s=%s", ref.Name(), dst, short(strings.TrimSpace(out))))
	return strings.TrimSpace(out), nil
}

// prFetchArgs is the git argv of one PR head fetch.
func prFetchArgs(remote, helper string, specs []string) []string {
	args := []string{"-c", "transfer.fsckObjects=true"}
	if helper != "" {
		args = append(args, "-c", "credential.helper=", "-c", "credential.helper="+helper)
	}
	args = append(args, "fetch", "-q", "--no-tags", "--no-write-fetch-head", "--", remote)
	return append(args, specs...)
}

// fetchFailed says what went wrong in a sentence that names the hub, and marks a missing credential.
func (s *Store) fetchFailed(ref Ref, err error) error {
	if errors.Is(err, ErrStopped) || errors.Is(err, context.Canceled) {
		return err
	}
	text := strings.ToLower(err.Error())
	var ge *Error
	if errors.As(err, &ge) {
		text = strings.ToLower(ge.Stderr + " " + ge.Error())
	}
	for _, w := range []string{"could not read username", "could not read password", "authentication failed",
		"terminal prompts disabled", "returned error: 401", "returned error: 403", "access denied",
		"repository not found", "invalid credentials"} {
		if strings.Contains(text, w) {
			return &FetchError{Auth: true, Msg: "the hub could not read the head of that pull request from the forge: " +
				ref.Name() + " is private or the hub's login cannot read it"}
		}
	}
	line := err.Error()
	if ge != nil && ge.First() != "" {
		line = ge.First()
	}
	return &FetchError{Msg: "the hub could not fetch the head of that pull request: " + s.scrub(line)}
}
