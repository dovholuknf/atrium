//go:build integration && git

package gitsync

import "testing"

// A PAGE IS CHANGED when claude/main has a commit after the landed sha that touched it, and the answer names the newest one.
// A page nothing touched, a path that leaves the repository and a mirror the hub does not hold are no hint.
func TestPageChangedSinceNamesTheNewestCommitThatTouchedThePage(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	work := x.branch("fix/x", "x.go")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	git(t, x.work, "switch", "-q", "-C", "cm", "fix/x")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	st := x.h.Store()

	if _, ok, err := st.PageChangedSince(bg, hubRepo, work, "README.md"); err != nil || ok {
		t.Fatalf("nothing came after the landing: %v %v", ok, err)
	}
	first := commit(t, x.work, "README.md", "one")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	got, ok, err := st.PageChangedSince(bg, hubRepo, work, "README.md")
	if err != nil || !ok || got.SHA != first || got.Date == "" {
		t.Fatalf("a touched page: %+v %v %v", got, ok, err)
	}
	// The head moved, so the answer is read again and is the newer commit.
	second := commit(t, x.work, "README.md", "two")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	if got, ok, err = st.PageChangedSince(bg, hubRepo, work, "README.md"); err != nil || !ok || got.SHA != second {
		t.Fatalf("the newest commit: %+v %v %v", got, ok, err)
	}
	if _, ok, err := st.PageChangedSince(bg, hubRepo, work, "docs/untouched.md"); err != nil || ok {
		t.Fatalf("an untouched page: %v %v", ok, err)
	}
	// The landing commit itself is not a change.
	if _, ok, _ := st.PageChangedSince(bg, hubRepo, work, "x.go"); ok {
		t.Fatal("the feature's own file was counted")
	}
	for _, p := range []string{"", "../x", "/etc/passwd", "a\nb"} {
		if _, ok, err := st.PageChangedSince(bg, hubRepo, work, p); ok || err == nil {
			t.Fatalf("%q: %v %v", p, ok, err)
		}
	}
	if _, ok, err := st.PageChangedSince(bg, "nowhere", work, "README.md"); ok || err == nil {
		t.Fatalf("a mirror the hub does not hold: %v %v", ok, err)
	}
}
