package land

import (
	"strings"
	"testing"
)

var me = Identity{Name: "Op Erator", Email: "op@example.com"}

func TestMessageKeepsOneScrubbedLine(t *testing.T) {
	cases := map[string]string{
		"Fix the thing  \n\nbody line\n\nCo-Authored-By: Claude <noreply@anthropic.com>\n": "Fix the thing\n",
		"\n\nSecond line first\nthird\n":            "Second line first\n",
		" \t\n  \nAfter blank-looking lines\n":      "After blank-looking lines\n",
		"Merge claude/main into r-001":              "Merge main into r-001\n",
		"Update CLAUDE.md and build.claude paths\t": "Update AGENTS.md and the build directory paths\n",
		"Ask Claude Code about claudevm":            "Ask the agent CLI about workvm\n",
		"Signed-off by anthropic person":            "\n",
		"":                                          "\n",
		"r-009/claude/branch names go":              "r-009/branch names go\n",
	}
	for in, want := range cases {
		if got := Message(in); got != want {
			t.Errorf("Message(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseRevListUnindentsAndFindsSignature(t *testing.T) {
	rec := "aaa\ntree t1\nparent p1\nparent p2\nauthor A <a@x> 100 +0100\ncommitter C <c@x> 200 -0500\n" +
		"gpgsig -----BEGIN PGP SIGNATURE-----\n \n abc\n -----END PGP SIGNATURE-----\n\n    subject\n    \n    body\n\x00"
	cs, err := ParseRevList([]byte(rec))
	if err != nil || len(cs) != 1 {
		t.Fatalf("got %v, %v", cs, err)
	}
	c := cs[0]
	if c.SHA != "aaa" || c.Tree != "t1" || len(c.Parents) != 2 || !c.Signed ||
		c.Author != "A <a@x> 100 +0100" || c.Committer != "C <c@x> 200 -0500" {
		t.Errorf("parsed %+v", c)
	}
	if c.Msg != "subject\n\nbody\n" {
		t.Errorf("msg %q", c.Msg)
	}
}

func TestBodyMatchesCommitTreeLayout(t *testing.T) {
	got := string(Body("t", []string{"p"}, me, "A <a@x> 100 +0100", "C <c@x> 200 -0500", "hi\n", "SIG1\n\nSIG2\n"))
	want := "tree t\nparent p\nauthor Op Erator <op@example.com> 100 +0100\n" +
		"committer Op Erator <op@example.com> 200 -0500\ngpgsig SIG1\n \n SIG2\n\nhi\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// A chain with a merge: m merges a (in range) and out (outside the range), and a collapses with b once rewritten.
func TestRewriteMapsParentsAndKeepsOutsideParent(t *testing.T) {
	const out = "0000000000000000000000000000000000000001"
	cs := []Commit{
		{SHA: "a000000000000000000000000000000000000001", Tree: "T1", Parents: []string{out}, Author: "X <x@y> 1 +0000", Committer: "X <x@y> 1 +0000", Msg: "first\n\nbody"},
		{SHA: "b000000000000000000000000000000000000002", Tree: "T1", Parents: []string{out}, Author: "X <x@y> 1 +0000", Committer: "X <x@y> 1 +0000", Msg: "first\n\nother body"},
		{SHA: "c000000000000000000000000000000000000003", Tree: "T2", Parents: []string{"a000000000000000000000000000000000000001", out}, Author: "X <x@y> 2 +0000", Committer: "X <x@y> 2 +0000", Msg: "merge"},
		{SHA: "d000000000000000000000000000000000000004", Tree: "T2", Parents: []string{"a000000000000000000000000000000000000001", "b000000000000000000000000000000000000002"}, Author: "X <x@y> 2 +0000", Committer: "X <x@y> 2 +0000", Msg: "dup parents"},
	}
	stored := map[string][]byte{}
	m, err := Rewrite(cs, me, nil, func(sha string, b []byte) error { stored[sha] = b; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if m[cs[0].SHA] != m[cs[1].SHA] {
		t.Errorf("commits identical once rewritten should be one commit")
	}
	merge := string(stored[m[cs[2].SHA]])
	if !strings.Contains(merge, "parent "+m[cs[0].SHA]+"\nparent "+out+"\n") {
		t.Errorf("merge parents wrong:\n%s", merge)
	}
	if strings.Count(string(stored[m[cs[3].SHA]]), "parent ") != 1 {
		t.Errorf("duplicate parent kept:\n%s", stored[m[cs[3].SHA]])
	}
	if Hash(stored[m[cs[2].SHA]]) != m[cs[2].SHA] {
		t.Errorf("sha is not the hash of the body")
	}
}

func TestRewriteSignsTheBodyWithoutTheSignature(t *testing.T) {
	cs := []Commit{{SHA: "a000000000000000000000000000000000000001", Tree: "T", Author: "X <x@y> 1 +0000", Committer: "X <x@y> 1 +0000", Msg: "m"}}
	var signed []byte
	stored := map[string][]byte{}
	m, err := Rewrite(cs, me, func(b []byte) (string, error) { signed = b; return "SIG\n", nil },
		func(sha string, b []byte) error { stored[sha] = b; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(signed), "gpgsig") {
		t.Errorf("signed body holds a signature: %q", signed)
	}
	if want := strings.Replace(string(stored[m[cs[0].SHA]]), "gpgsig SIG\n", "", 1); want != string(signed) {
		t.Errorf("stored body minus signature %q != signed %q", want, signed)
	}
}

func TestVerifyCatchesEachBadCase(t *testing.T) {
	good := Commit{SHA: "a000000000000000000000000000000000000001", Author: "Op Erator <op@example.com> 1 +0000",
		Committer: "Op Erator <op@example.com> 1 +0000", Signed: true, Msg: "one line\n"}
	if bad := Verify([]Commit{good}, me, true); len(bad) != 0 {
		t.Fatalf("good commit failed: %v", bad)
	}
	mut := func(f func(*Commit)) Commit { c := good; f(&c); return c }
	cases := map[string]Commit{
		"more than one line":  mut(func(c *Commit) { c.Msg = "one\ntwo\n" }),
		"empty message":       mut(func(c *Commit) { c.Msg = "\n" }),
		"contains \"Claude\"": mut(func(c *Commit) { c.Msg = "ask Claude\n" }),
		"anthropic":           mut(func(c *Commit) { c.Msg = "by anthropic\n" }),
		"Co-Authored-By":      mut(func(c *Commit) { c.Msg = "x Co-Authored-By: y\n" }),
		"author is":           mut(func(c *Commit) { c.Author = "Other <o@x> 1 +0000" }),
		"committer is":        mut(func(c *Commit) { c.Committer = "Other <o@x> 1 +0000" }),
		"not signed":          mut(func(c *Commit) { c.Signed = false }),
	}
	for want, c := range cases {
		bad := Verify([]Commit{c}, me, true)
		if len(bad) == 0 || !strings.Contains(strings.Join(bad, "|"), want) {
			t.Errorf("want a problem containing %q, got %v", want, bad)
		}
	}
	if bad := Verify([]Commit{mut(func(c *Commit) { c.Signed = false })}, me, false); len(bad) != 0 {
		t.Errorf("unsigned is fine with no-sign: %v", bad)
	}
}
