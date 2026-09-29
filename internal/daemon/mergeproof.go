package daemon

import (
	"fmt"
	"path/filepath"
	"strings"
)

// The proof half of a merge whose worker is on ANOTHER room. See
// docs/rnd/merged-cull-design.md, "Across rooms".
//
// A remote worker's branch comes back to this machine through
// `room-git.ps1 fetch` as `<room>/claude/<id>`, and the area branch it merged
// into lives here and nowhere else. So the proof is made here: is the fetched
// tip an ancestor of the area branch. The answer is `{into, tip}`, which the cull
// sent to the owning room carries, and that room accepts it only while its
// worktree still sits on `tip` (CullProved).
//
// THIS RUNS NO FETCH. Fetching is `room-git.ps1`'s job and a person or a script
// runs it, so the answer describes the refs as they are now and says so.

// MergeProof is one answer. Merged false is an answer, not an error: the ref is
// there and the area branch does not contain it yet.
type MergeProof struct {
	Into   string `json:"into"`
	Ref    string `json:"ref"`
	Tip    string `json:"tip"`
	Merged bool   `json:"merged"`
}

// MergeProof checks `ref` against `into` in the repository of `dir`, which has to
// be the worktree of a card on this room. That is the whole containment rule:
// this is not a way to ask git about any directory on the machine.
func (d *Daemon) MergeProof(dir, ref, into string) (*MergeProof, error) {
	dir, ref, into = strings.TrimSpace(dir), strings.TrimSpace(ref), strings.TrimSpace(into)
	if dir == "" || ref == "" || into == "" {
		return nil, fmt.Errorf("a proof needs a directory, a ref and a branch to check it against")
	}
	if strings.HasPrefix(ref, "-") || strings.HasPrefix(into, "-") {
		return nil, fmt.Errorf("%q is not a ref", ref)
	}
	if !d.isCardDir(dir) {
		return nil, fmt.Errorf("%s is not a directory any card on this room works in", dir)
	}
	local := filepath.FromSlash(dir)
	tip, err := gitIn(local, "rev-parse", "--verify", "-q", ref+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("there is no %s to check. has it been fetched?", ref)
	}
	if _, err := gitIn(local, "rev-parse", "--verify", "-q", into+"^{commit}"); err != nil {
		return nil, fmt.Errorf("there is no branch %s here to check against", into)
	}
	// Exit 1 is "not an ancestor", which is an answer. Anything else is the same
	// error shape here, and is also reported as not merged: the safe direction.
	_, ancestorErr := gitIn(local, "merge-base", "--is-ancestor", tip, into)
	return &MergeProof{Into: into, Ref: ref, Tip: tip, Merged: ancestorErr == nil}, nil
}

// isCardDir says whether dir is some card's recorded worktree.
func (d *Daemon) isCardDir(dir string) bool {
	tasks, err := d.st.List()
	if err != nil {
		return false
	}
	want := filepath.Clean(filepath.FromSlash(dir))
	for _, t := range tasks {
		if strings.TrimSpace(t.Worktree) != "" && strings.EqualFold(filepath.Clean(filepath.FromSlash(t.Worktree)), want) {
			return true
		}
	}
	return false
}
