package gitsync

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheStorePathNeverLeavesTheRoot(t *testing.T) {
	root := t.TempDir()
	s := &Store{h: &Hub{StoreRoot: func() string { return root }}}
	for _, ref := range []Ref{{"..", "x", "y"}, {"github", "..", "y"}, {"github", "x", "../../y"}, {"", "", ""}} {
		d := s.dir(ref)
		if d != filepath.Join(root, "refused.git") {
			t.Errorf("%+v gave %q", ref, d)
		}
	}
	good := s.dir(Ref{"github", "o", "r"})
	if !strings.HasPrefix(good, root) || !strings.HasSuffix(filepath.ToSlash(good), "github/o/r.git") {
		t.Errorf("a plain ref gave %q", good)
	}
}

func TestAGitArgumentHasNoLineBreakOrNul(t *testing.T) {
	cmd := gitCommand(context.Background(), "", nil, []string{"log", "a\nb\x00c\r"})
	for _, a := range cmd.Args {
		if strings.ContainsAny(a, "\x00\r\n") {
			t.Errorf("argument %q kept a control character", a)
		}
	}
	if got := cmd.Args[len(cmd.Args)-1]; got != "abc" {
		t.Errorf("last argument %q", got)
	}
}
