package worktrees

import (
	"fmt"
	"strings"
	"testing"
)

func TestLineFormat(t *testing.T) {
	long := "Stack sort keeps the pin of a card across a reload and a restart"
	for _, c := range []struct {
		name string
		l    Line
		want string
	}{
		{"landed", Line{Path: "/wt/a", Branch: "claude/a", Landed: "patch", Removable: true},
			"/wt/a  claude/a  clean  landed:patch  removable"},
		{"dirty and unlanded", Line{Path: "/wt/b", Branch: "claude/b", Dirty: 2, Flags: []string{"BRIEF"}, Unlanded: 1,
			Newest: "9c1e2abdeadbeef", Subject: "Stack sort", Keep: []string{ReasonDirty, ReasonUnlanded}},
			`/wt/b  claude/b  dirty 2 +BRIEF  unlanded 1 9c1e2ab "Stack sort"  keep: dirty, unlanded`},
		{"subject cut and more", Line{Path: "p", Branch: "b", Unlanded: 3, Newest: "abcdef0123", Subject: long,
			Keep: []string{ReasonUnlanded}},
			`p  b  clean  unlanded 3 abcdef0 "` + long[:40] + `" +2  keep: unlanded`},
		{"missing", Line{Path: "p", Branch: "(missing)", Missing: true, Prune: true, Removable: true},
			"p  (missing)  -  -  removable: prune"},
		{"unknown", Line{Path: "p", Branch: "b", Unknown: "status: boom", Keep: []string{ReasonUnknown}},
			"p  b  unknown  unknown  keep: unknown"},
	} {
		if got := c.l.Format(len(c.l.Path)); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestFormatPadsOnlyThePathAndKeepsOneTrailer(t *testing.T) {
	res := &Result{Ref: refHub, RefSHA: "3a359af7d0", From: "hub", Total: 4, More: 2, Unknown: 1,
		FirstUnknown: "status: boom",
		Lines: []Line{{Path: "a", Branch: "b", Landed: "ancestry", Removable: true},
			{Path: "abc", Branch: "b", Landed: "ancestry", Removable: true}}}
	got := strings.Split(strings.TrimRight(Format(res), "\n"), "\n")
	if len(got) != 4 || got[0] != "claude/main 3a359af7 from hub, 4 worktrees" {
		t.Fatalf("answer:\n%s", strings.Join(got, "\n"))
	}
	if !strings.HasPrefix(got[1], "a    b  ") || !strings.HasPrefix(got[2], "abc  b  ") {
		t.Errorf("padding: %q %q", got[1], got[2])
	}
	if got[3] != "2 more, narrow with prefix or paths  1 unknown: status: boom" {
		t.Errorf("trailer %q", got[3])
	}
}

func TestBudgetOfAHundredAndFiftyWorktrees(t *testing.T) {
	var lines []Line
	for i := 0; i < 150; i++ {
		// a path of 50 characters
		path := "/worktrees/claude/atrium/" + strings.Repeat("x", 20) + fmt.Sprintf("-w-%02d", i%100)
		lines = append(lines, Line{Path: path, Branch: fmt.Sprintf("claude/w-%02d", i%100), Unlanded: 1,
			Newest: strings.Repeat("a", 40), Subject: strings.Repeat("s", 60), Keep: []string{ReasonUnlanded}})
	}
	kept, more := capLines(lines)
	res := &Result{Ref: refLocal, RefSHA: strings.Repeat("a", 40), From: "local", Total: 150, Lines: kept, More: more}
	got := strings.Split(strings.TrimRight(Format(res), "\n"), "\n")
	if len(kept) != 100 || more != 50 || len(got) != 102 {
		t.Fatalf("%d kept, %d more, %d lines", len(kept), more, len(got))
	}
	if got[101] != "50 more, narrow with prefix or paths" {
		t.Errorf("trailer %q", got[101])
	}
	for _, l := range got[1:101] {
		if n := len([]rune(l)); n > 160 {
			t.Fatalf("a line of %d characters: %q", n, l)
		}
	}
}
