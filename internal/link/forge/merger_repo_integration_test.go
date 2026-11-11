//go:build integration

package forge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

type launchCaller struct {
	ctl.Caller
	in chan ctl.LaunchInput
}

func (c launchCaller) Ask(_ context.Context, _, _, _ string, _, out any) error {
	b, _ := json.Marshal(map[string]any{"tasks": []map[string]any{{"id": "c1", "wire_name": "w1", "tags": []string{"atrium:item:f-1"}}}})
	return json.Unmarshal(b, out)
}

func (c launchCaller) LaunchOnRoom(_ context.Context, in ctl.LaunchInput, _, _, _, _, _, _ string) (ctl.LaunchOutput, error) {
	c.in <- in
	return ctl.LaunchOutput{Handle: "m1"}, nil
}

func TestMergerLaunchNamesARepoTheRoomMakesAWorktreeOf(t *testing.T) {
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := launchCaller{in: make(chan ctl.LaunchInput, 1)}
	p := New(Calls{Ctl: func() ctl.Caller { return c }, BacklogStore: func() *hubstore.Store { return st },
		WorkOfTags: func([]string) string { return "f-1" }})
	m := hubstore.MergeRequest{N: 23, Repo: "github/o/r", Branch: "claude/foo", Requester: "w1", RequesterRoom: "sg4", Room: "sg4"}
	if _, err := p.mergeLaunchMerger(context.Background(), m, gitsync.MergeResult{Conflicts: []string{"a.txt"}}); err != nil {
		t.Fatal(err)
	}
	in := <-c.in
	if in.Cwd != "" || !home.RepoIsRef(in.Repo) || !strings.HasSuffix(in.Repo, "/o/r") || in.Branch != MergerBranch(23) {
		t.Fatalf("the merger would start outside a worktree: cwd %q repo %q branch %q", in.Cwd, in.Repo, in.Branch)
	}
}
