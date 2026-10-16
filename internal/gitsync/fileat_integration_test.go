//go:build integration && git

package gitsync

import "testing"

// A FILE AS A COMMIT HELD IT: the text when the commit has it, false when it does not, and a refusal for a path that leaves the
// repository or a mirror the hub does not hold.
func TestFileAtReadsAFileAsACommitHeldIt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	work := x.branch("fix/x", "x.go")
	x.must(x.push(roomCard("sg4", "C1"), hubRepo, "fix/x:refs/heads/fix/x"))
	st := x.h.Store()

	if _, ok, err := st.FileAt(bg, hubRepo, work, "docs/changes/x.md"); err != nil || ok {
		t.Fatalf("a file the commit lacks: %v %v", ok, err)
	}
	if got, ok, err := st.FileAt(bg, hubRepo, work, "x.go"); err != nil || !ok || got == "" {
		t.Fatalf("a file the commit holds: %q %v %v", got, ok, err)
	}
	for _, p := range []string{"", "../x", "/etc/passwd", "-x", "a\nb"} {
		if _, ok, err := st.FileAt(bg, hubRepo, work, p); ok || err == nil {
			t.Fatalf("%q: %v %v", p, ok, err)
		}
	}
	if _, ok, err := st.FileAt(bg, "nowhere", work, "x.go"); ok || err == nil {
		t.Fatalf("a mirror not held: %v %v", ok, err)
	}
}
