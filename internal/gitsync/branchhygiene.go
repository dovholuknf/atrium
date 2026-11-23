package gitsync

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
)

// A LANDED BRANCH GOES, AND A BUNDLE OF IT STAYS. Once claude/main holds every change of a claude/* or stash/* branch
// (an ancestor, or a squash or a cherry-pick that merging changes no file of), the hub deletes it. Before each delete
// it writes a bundle of what claude/main does not have to `<hub dir>/git-bundles/<host>/<owner>/<repo>/`, kept
// `git.bundle_retention_days` (30 by default, 0 for never), so `git fetch <bundle> <ref>` gets it back. A branch whose
// tip is already under claude/main needs no bundle, since claude/main keeps every commit.
//
// A merge deletes its own branch and the stash copies of it through DeleteLanded. SweepLanded is the same by content
// for every other branch, once a day, and skips a branch pushed within sweepGrace, so a card that has only just cut
// one is not robbed. Only refs/heads/claude/ and refs/heads/stash/ are ever looked at, and never claude/main.

// DefaultBundleRetentionDays is the retention when the setting was never written.
const DefaultBundleRetentionDays = 30

const (
	sweepGrace = 2 * 24 * time.Hour
	// SweepEvery is how often the collect timer runs SweepLanded.
	SweepEvery = 24 * time.Hour
)

// BundleRetention is the retention in days: BundleRetentionDays when set, else the default. 0 is never.
func (h *Hub) BundleRetention() int {
	if h.BundleRetentionDays == nil {
		return DefaultBundleRetentionDays
	}
	if n := h.BundleRetentionDays(); n >= 0 {
		return n
	}
	return DefaultBundleRetentionDays
}

// Unlanded is how many branches the last sweep found with work claude/main does not have.
func (h *Hub) Unlanded() int { return int(h.unlanded.Load()) }

// BundleDir is where the bundles of a repository's deleted branches are.
func (h *Hub) BundleDir(name string) string {
	ref, err := ParseName(name)
	if err != nil {
		return filepath.Join(h.Dir, "git-bundles", "refused")
	}
	return filepath.Join(h.Dir, "git-bundles", ref.Host, ref.Owner, ref.Repo)
}

// dropRef bundles ref at tip, then deletes it only if it is still at tip. The caller holds the repository's lock.
func (s *Store) dropRef(ctx context.Context, dir, name, ref, tip string) error {
	if under, err := s.isAncestor(ctx, dir, tip, ClaudeMainRef); err != nil {
		return err
	} else if !under {
		bd := s.h.BundleDir(name)
		if err := os.MkdirAll(bd, 0o755); err != nil {
			return err
		}
		file := fmt.Sprintf("%d-%s-%s.bundle", time.Now().Unix(), strings.ReplaceAll(strings.TrimPrefix(ref, headsPrefix), "/", "_"), tip[:8])
		if _, err := s.git(ctx, dir, gitexec.BundleCreate{File: filepath.Join(bd, file), Ref: ref, Exclude: ClaudeMainRef}); err != nil {
			return err
		}
	}
	_, err := s.git(ctx, dir, gitexec.DeleteRef{Ref: ref, Old: tip})
	return err
}

// dropStashCopies drops every stash/<card>/<branch> whose work claude/main holds, and answers the refs it dropped.
func (s *Store) dropStashCopies(ctx context.Context, dir, name, branch string) []string {
	text, err := s.git(ctx, dir, gitexec.ForEachRef{Format: "%(refname) %(objectname)", Patterns: []string{stashPrefix}})
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if _, rest, _ := strings.Cut(strings.TrimPrefix(f[0], stashPrefix), "/"); rest != branch {
			continue
		}
		if !s.holdsAll(ctx, dir, f[1], ClaudeMainRef) || s.dropRef(ctx, dir, name, f[0], f[1]) != nil {
			continue
		}
		out = append(out, f[0])
	}
	return out
}

// SweepResult is what one sweep of a repository did.
type SweepResult struct {
	Deleted []string
	// Unlanded are the branches whose work claude/main does not have, for the human to read.
	Unlanded []string
}

// SweepLanded deletes every claude/* and stash/* branch of the repository whose work claude/main holds, with a bundle
// of each, and lists the rest.
func (s *Store) SweepLanded(ctx context.Context, name string) (SweepResult, error) {
	var res SweepResult
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return res, err
	}
	text, err := s.git(ctx, dir, gitexec.ForEachRef{Format: "%(refname) %(objectname) %(committerdate:unix)",
		Patterns: []string{headsPrefix + "claude/", stashPrefix}})
	if err != nil {
		return res, err
	}
	cut := time.Now().Add(-sweepGrace)
	pushed := map[string]time.Time{}
	if s.h.PushLog != nil {
		if recs, err := s.h.PushLog.Branches(ctx, name); err == nil {
			for _, r := range recs {
				pushed[r.Ref] = r.At
			}
		}
	}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] == ClaudeMainRef || !ValidSHA(f[1]) {
			continue
		}
		ref, tip := f[0], f[1]
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
		if !s.holdsAll(ctx, dir, tip, ClaudeMainRef) {
			res.Unlanded = append(res.Unlanded, strings.TrimPrefix(ref, headsPrefix))
			continue
		}
		if err := s.sweepDrop(ctx, dir, name, ref, tip); err != nil {
			log.Printf("[hub] sweep: could not delete %s %s: %v", name, ref, err)
			continue
		}
		res.Deleted = append(res.Deleted, ref)
	}
	return res, ctx.Err()
}

func (s *Store) sweepDrop(ctx context.Context, dir, name, ref, tip string) error {
	unlock, err := s.lockMerge(name, dir)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.dropRef(ctx, dir, name, ref, tip); err != nil {
		return err
	}
	if s.h.PushLog != nil {
		_ = s.h.PushLog.Release(ctx, name, ref, "landed sweep")
	}
	return nil
}

// SweepAll is SweepLanded over every repository of the store, then the removal of bundles past their retention.
func (h *Hub) SweepAll(ctx context.Context) error {
	entries, err := h.Store().List()
	if err != nil {
		return err
	}
	total := 0
	for _, e := range entries {
		name := e.Ref.Name()
		res, err := h.Store().SweepLanded(ctx, name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		total += len(res.Unlanded)
		for _, ref := range res.Deleted {
			h.audit("", "git-branch-swept", name+" "+ref)
		}
		if len(res.Deleted) > 0 {
			log.Printf("[hub] sweep: %s deleted %d landed branches, %d unlanded", name, len(res.Deleted), len(res.Unlanded))
		}
	}
	h.unlanded.Store(int64(total))
	return h.ExpireBundles()
}

// ExpireBundles removes the bundles older than the retention. 0 keeps them all.
func (h *Hub) ExpireBundles() error {
	days := h.BundleRetention()
	if days == 0 {
		return nil
	}
	cut := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	return filepath.WalkDir(filepath.Join(h.Dir, "git-bundles"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".bundle") {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(cut) {
			if os.Remove(p) == nil {
				h.audit("", "git-bundle-expired", filepath.ToSlash(p))
			}
		}
		return nil
	})
}
