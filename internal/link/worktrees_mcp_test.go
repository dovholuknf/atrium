package link

import (
	"context"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link/ctl"
)

func TestWorktreesToolIsFullSetOnlyAndShort(t *testing.T) {
	if workerTools["atrium_worktrees"] {
		t.Error("atrium_worktrees is in the worker set")
	}
	if inClass("atrium_worktrees", ctl.ClassWorker) || !inClass("atrium_worktrees", ctl.ClassFull) {
		t.Error("atrium_worktrees is served to the wrong class")
	}
	if n := len(strings.Fields(worktreesToolDesc)); n >= 120 {
		t.Errorf("the description is %d words, want under 120", n)
	}
}

func TestWorktreesToolRefusesBeforeAskingTheRoom(t *testing.T) {
	c := &controlMCP{}
	for _, tc := range []struct {
		in   worktreesInput
		want string
	}{
		{worktreesInput{Action: "remove", Paths: []string{"x"}}, "not built yet"},
		{worktreesInput{Action: "audit", Prefix: "a", Paths: []string{"b"}}, "together"},
		{worktreesInput{Action: "nope"}, "audit or remove"},
	} {
		_, _, err := c.worktreesHandler(context.Background(), nil, tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: err = %v, want %q", tc.in, err, tc.want)
		}
	}
}
