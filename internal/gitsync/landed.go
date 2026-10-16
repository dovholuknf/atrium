package gitsync

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// THE LANDED RULE AS FUNCTIONS. Branch status (branchstatus.go) decides whether a commit is carried by main, and the
// rule is the same wherever a repository sits, so the facts and the decision are plain functions over a Runner and a
// directory and not methods of the hub's Store. See docs/runtime/worktrees-tool-design.md, phase 1. `env` is the extra
// environment each git command runs with, nil when there is none.

// carriedBy is the rule that carries a commit on main, or empty when none does. Rule 3 is per branch and not here.
func carriedBy(f commitFacts, mainPIDs map[string]bool, mainSubj map[string]map[string]bool) string {
	if f.pid != "" && mainPIDs[f.pid] {
		return HowPatchID
	}
	if keys, ok := mainSubj[f.subject]; ok && keys[f.key] {
		return HowSubject
	}
	return ""
}

// readFacts is the patch id, subject and files of each commit, in batches.
func readFacts(ctx context.Context, r *Runner, dir string, env []string, shas []string) (map[string]commitFacts, error) {
	out := make(map[string]commitFacts, len(shas))
	for i := 0; i < len(shas); i += hashBatchSize {
		batch := shas[i:min(i+hashBatchSize, len(shas))]
		if err := readMeta(ctx, r, dir, env, batch, out); err != nil {
			return nil, err
		}
		pids, err := hashBatch(ctx, r, dir, env, batch)
		if err != nil {
			return nil, err
		}
		for c, p := range pids {
			f := out[c]
			f.pid = p
			out[c] = f
		}
	}
	return out, nil
}

// readMeta fills in subject and files, which cost no diff.
func readMeta(ctx context.Context, r *Runner, dir string, env []string, shas []string, into map[string]commitFacts) error {
	out, err := r.gitWith(ctx, dir, env, []byte(strings.Join(shas, "\n")+"\n"), 0,
		"-c", "core.quotepath=off", "log", "--no-walk=unsorted", "--stdin", "--no-merges", "--no-renames", "--name-only",
		"--format=%x00%H%x00%s")
	if err != nil {
		return err
	}
	parts := strings.Split(out, "\x00")
	for i := 1; i+1 < len(parts); i += 2 {
		sha, rest := parts[i], parts[i+1]
		subject, list, _ := strings.Cut(rest, "\n")
		var files []string
		for _, l := range strings.Split(list, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				files = append(files, l)
			}
		}
		sort.Strings(files)
		into[sha] = commitFacts{subject: strings.TrimSpace(subject), files: files, key: strings.Join(files, "\x00")}
	}
	return nil
}

// hashBatch is the patch id of each commit that has one. A batch whose diff is past hashCap is halved, and a single
// commit past it has none.
func hashBatch(ctx context.Context, r *Runner, dir string, env []string, shas []string) (map[string]string, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	diff, err := r.gitWith(ctx, dir, env, []byte(strings.Join(shas, "\n")+"\n"), hashCap,
		"log", "--no-walk=unsorted", "--stdin", "-p", "--no-merges", "--no-renames", "--format=commit %H")
	if err != nil {
		if !errors.Is(err, ErrOutputCap) {
			return nil, err
		}
		if len(shas) == 1 {
			return nil, nil
		}
		a, err := hashBatch(ctx, r, dir, env, shas[:len(shas)/2])
		if err != nil {
			return nil, err
		}
		b, err := hashBatch(ctx, r, dir, env, shas[len(shas)/2:])
		if err != nil {
			return nil, err
		}
		for c, p := range b {
			a[c] = p
		}
		return a, nil
	}
	if strings.TrimSpace(diff) == "" {
		return nil, nil
	}
	ids, err := r.GitInput(ctx, dir, []byte(diff), "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	res := map[string]string{}
	for _, l := range strings.Split(ids, "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			res[f[1]] = f[0]
		}
	}
	return res, nil
}
