package gitsync

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// A CLOSE'S STASH GOES AFTER A WHILE. `stash/<card short id>/<branch>` is what a close pushes with the stash answer
// (internal/daemon/closestash.go), and nothing else on the hub removes one. Each pass of the hub's collect timer
// deletes the ones older than the `git.stash_retention_days` setting, 30 by default, 0 for never.
//
// AGE is the time of the stash's push when the push log has one, and the commit's committer date when it has not,
// whichever is LATER. A stash of a clean worktree is an old commit, and the commit date alone would expire it the
// moment it landed.
//
// Only refs/heads/stash/ is ever looked at. Every expiry is logged and audited with the ref, the card and the sha, so
// the stash can be got back with `git branch <name> <sha>` until git's own gc drops the commit.

// DefaultStashRetentionDays is the retention when the setting was never written.
const DefaultStashRetentionDays = 30

// stashPrefix is where every stash is on the hub.
const stashPrefix = headsPrefix + "stash/"

// StashRetention is the retention in days: StashRetentionDays when set, else the default. 0 is never.
func (h *Hub) StashRetention() int {
	if h.StashRetentionDays == nil {
		return DefaultStashRetentionDays
	}
	if n := h.StashRetentionDays(); n >= 0 {
		return n
	}
	return DefaultStashRetentionDays
}

// ExpiredStash is one stash a pass removed.
type ExpiredStash struct {
	Repo, Ref, Card, SHA string
}

// ExpireStashes is one pass over every repository in the store, answering what it removed.
func (h *Hub) ExpireStashes(ctx context.Context) ([]ExpiredStash, error) {
	days := h.StashRetention()
	if days == 0 {
		return nil, nil
	}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	entries, err := h.Store().List()
	if err != nil {
		return nil, err
	}
	var out []ExpiredStash
	var errs []string
	for _, e := range entries {
		got, err := h.expireStashesIn(ctx, e, cut)
		out = append(out, got...)
		if err != nil {
			errs = append(errs, e.Ref.Name()+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return out, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

func (h *Hub) expireStashesIn(ctx context.Context, e Entry, cut time.Time) ([]ExpiredStash, error) {
	name := e.Ref.Name()
	l := h.lock("store:" + strings.ToLower(name))
	l.Lock()
	defer l.Unlock()
	run := h.runner()
	text, err := run.Git(ctx, e.Dir, "for-each-ref", "--format=%(refname) %(objectname) %(committerdate:unix)", stashPrefix)
	if err != nil {
		return nil, err
	}
	pushed := map[string]time.Time{}
	if h.PushLog != nil {
		if recs, err := h.PushLog.Branches(ctx, name); err == nil {
			for _, r := range recs {
				pushed[r.Ref] = r.At
			}
		}
	}
	var out []ExpiredStash
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || !strings.HasPrefix(f[0], stashPrefix) {
			continue
		}
		ref, sha := f[0], f[1]
		secs, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			continue
		}
		at := time.Unix(secs, 0)
		if p := pushed[ref]; p.After(at) {
			at = p
		}
		if at.After(cut) {
			continue
		}
		// The old sha is given, so a stash pushed again between the listing and here is not taken.
		if _, err := run.Git(ctx, e.Dir, "update-ref", "-d", ref, sha); err != nil {
			return out, fmt.Errorf("%s: %w", ref, err)
		}
		card, _, _ := strings.Cut(strings.TrimPrefix(ref, stashPrefix), "/")
		if h.PushLog != nil {
			_ = h.PushLog.Release(ctx, name, ref, "stash-expiry")
		}
		msg := fmt.Sprintf("%s %s card=%s sha=%s", name, ref, card, sha)
		log.Printf("[hub] stash expired: %s", msg)
		h.audit("", "git-stash-expired", msg)
		out = append(out, ExpiredStash{Repo: name, Ref: ref, Card: card, SHA: sha})
	}
	return out, nil
}
