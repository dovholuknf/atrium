package gitsync

import (
	"context"
	"sort"
	"strings"
)

// WHICH BRANCH OF THE HUB HAS A COMMIT, whatever repository it is in. A worker ending with atrium_done names a commit
// it pushed, and asking by the repository its own directory belongs to missed every worker whose directory is not a
// clone the room can name (a plain directory, a worktree of an unmapped clone, a remote that is the hub's own
// forwarder). The hub knows its repositories, so it is asked for the commit and nothing else.
//
// A commit is on a branch when it is the branch's tip or an ancestor of it. Only the hub's own store is read, never a
// room's work in progress, and never the forge.

// commitMinLen is the shortest commit id the lookup takes, as git itself takes.
const commitMinLen = 7

// lookupCommit answers a lookup that names a commit and no repository: `found` with the repository and the one branch
// that has it, or `not found`.
func (h *Hub) lookupCommit(ctx context.Context, q URLQuery) URLAnswer {
	sha := strings.ToLower(strings.TrimSpace(q.Commit))
	if len(sha) < commitMinLen || len(sha) > 40 || !isHex(sha) {
		return URLAnswer{State: URLNotFound, Note: "that is not a commit id: 7 to 40 hex characters"}
	}
	known := h.known(ctx)
	names := make([]string, 0, len(known))
	for n, k := range known {
		if k.inStore {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		v, ok := h.Store().ViewOne(ctx, name)
		if !ok {
			continue
		}
		dir, err := h.Store().Path(name)
		if err != nil {
			continue
		}
		type tip struct{ branch, sha string }
		tips := make([]tip, 0, len(v.Branches)+1)
		if v.Main.SHA != "" {
			tips = append(tips, tip{"main", v.Main.SHA})
		}
		for _, b := range v.Branches {
			tips = append(tips, tip{b.Name, b.SHA})
		}
		for _, t := range tips {
			if !isHex40(t.sha) {
				continue
			}
			if strings.HasPrefix(strings.ToLower(t.sha), sha) || h.Store().commitUnder(ctx, dir, sha, t.sha) {
				return URLAnswer{State: URLFound, Repo: name, Branch: t.branch, Branches: []URLBranch{{
					Name: t.branch, Sources: []URLSource{{Source: "hub", SHA: t.sha}}}}}
			}
		}
	}
	return URLAnswer{State: URLNotFound, Note: "no branch on the hub has commit " + sha}
}

// commitUnder says whether sha (hex, 7 to 40) is reachable from tip (40 hex) in a bare repository.
func (s *Store) commitUnder(ctx context.Context, dir, sha, tip string) bool {
	if dir == "" || !isHex(sha) || !isHex40(tip) {
		return false
	}
	_, err := s.git(ctx, dir, "merge-base", "--is-ancestor", sha, tip)
	return err == nil
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
