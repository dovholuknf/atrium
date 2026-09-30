package daemon

import "testing"

// The proof is made where the area branch lives: a fetched tip that the area
// branch contains is merged, one it does not is an answer and not an error.
func TestMergeProofSaysWhetherTheAreaBranchHoldsTheTip(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, false)
	cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	got, err := d.MergeProof(r.wt, r.branch, DefaultCullInto)
	if err != nil {
		t.Fatal(err)
	}
	if got.Merged || got.Tip != cullGit(t, r.wt, "rev-parse", "HEAD") {
		t.Fatalf("proof = %+v, want the tip and not merged", got)
	}
	cullGit(t, r.main, "branch", "-f", DefaultCullInto, r.branch)
	if got, err = d.MergeProof(r.wt, r.branch, DefaultCullInto); err != nil || !got.Merged {
		t.Fatalf("proof = %+v err=%v, want merged", got, err)
	}
}

// It is not a way to ask git about any directory on the machine.
func TestMergeProofRefusesADirectoryNoCardWorksIn(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	if _, err := d.MergeProof(r.wt, r.branch, DefaultCullInto); err == nil {
		t.Fatal("a proof was made for a directory no card works in")
	}
}
