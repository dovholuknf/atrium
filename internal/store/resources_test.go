package store

import "testing"

// A card's inventory: rows count per card, a pending owner hands its rows to the card after the card's own, a freed
// row stays with when it went, and the disk is the measured live rows only.
func TestCardResourcesCountMoveFreeAndMeasure(t *testing.T) {
	st := open(t)
	if _, err := st.AddResource("c1", ResDir, "/tmp/c1-scratch", ""); err != nil {
		t.Fatal(err)
	}
	for _, r := range [][2]string{{ResWorktree, "/wt/o/r/x"}, {ResRef, "refs/atrium/pr/7"}, {ResReview, "pr_1"}} {
		if _, err := st.AddResource("pending:k", r[0], r[1], "/git/o/r"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MoveResources("pending:k", "c1"); err != nil {
		t.Fatal(err)
	}
	rows, err := st.Resources("c1")
	if err != nil || len(rows) != 4 {
		t.Fatalf("%v %d", err, len(rows))
	}
	for i, want := range []string{ResDir, ResWorktree, ResRef, ResReview} {
		if rows[i].Seq != i+1 || rows[i].Kind != want {
			t.Errorf("row %d = %d %s, want %d %s", i, rows[i].Seq, rows[i].Kind, i+1, want)
		}
	}
	if left, _ := st.Resources("pending:k"); len(left) != 0 {
		t.Errorf("the pending owner kept %d rows", len(left))
	}
	if _, err := st.AddResource("c1", ResDir, "/tmp/more", ""); err != nil {
		t.Fatal(err)
	}
	_ = st.SetResourceBytes("c1", 1, 100)
	_ = st.SetResourceBytes("c1", 2, 5000)
	if err := st.FreeResource("c1", 1, "it was not there"); err != nil {
		t.Fatal(err)
	}
	if err := st.FreeResource("c1", 99, ""); err == nil {
		t.Error("freeing a row that does not exist said nothing")
	}
	rows, _ = st.Resources("c1")
	if rows[0].Live() || rows[0].FreedErr != "it was not there" || rows[4].Seq != 5 {
		t.Errorf("rows after free: %+v %+v", rows[0], rows[4])
	}
	disk, err := st.CardDisk()
	if err != nil || disk["c1"] != 5000 {
		t.Errorf("disk %v %v, want only the live measured 5000", err, disk)
	}
}
