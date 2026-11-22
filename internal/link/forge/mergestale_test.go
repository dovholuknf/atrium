package forge

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

func TestMergeStaleResolvesRedAsksNobodyCanAct(t *testing.T) {
	red := func(n int64, branch, who, room string) hubstore.MergeRequest {
		return hubstore.MergeRequest{N: n, Repo: "github/o/r", Branch: branch, State: hubstore.MergeRed,
			Requester: who, RequesterRoom: room}
	}
	ms := []hubstore.MergeRequest{
		red(1, "claude/live", "w1", "room-e"),
		red(2, "claude/relanded", "w1", "room-e"),
		{N: 3, Repo: "github/o/r", Branch: "claude/relanded", State: hubstore.MergeLanded},
		red(4, "claude/deleted", "w1", "room-e"),
		red(5, "claude/merged", "w1", "room-e"),
		red(6, "claude/closed", "w2", "room-e"),
		red(7, "claude/culled", "w3", "room-e"),
		red(8, "claude/away", "w9", "gone-room"),
		{N: 9, Repo: "github/o/r", Branch: "claude/deleted", State: hubstore.MergeRefused},
	}
	branches := func(repo string) map[string]string {
		if repo != "github/o/r" {
			t.Fatalf("asked about %q", repo)
		}
		return map[string]string{"claude/live": gitsync.BranchOrphaned, "claude/relanded": gitsync.BranchOrphaned,
			"claude/merged": gitsync.BranchLanded, "claude/closed": gitsync.BranchOpen, "claude/culled": gitsync.BranchOpen,
			"claude/away": gitsync.BranchOpen}
	}
	cards := func(room string) []ctl.Card {
		if room != "room-e" {
			return nil
		}
		return []ctl.Card{{ID: "a", Wire: "w1", Status: "done"}, {ID: "b", Wire: "w2", Status: "done", Tags: []string{closedTag}}}
	}
	mergeStale(ms, branches, cards)
	want := map[int64]string{1: "", 2: "a later request landed it", 3: "", 4: "the branch is gone from the hub",
		5: "the branch landed", 6: "the card that asked is closed", 7: "the card that asked is gone", 8: "", 9: ""}
	for _, m := range ms {
		if m.Stale != want[m.N] {
			t.Errorf("request %d (%s): stale %q, want %q", m.N, m.Branch, m.Stale, want[m.N])
		}
	}

	unknown := []hubstore.MergeRequest{red(1, "claude/deleted", "w3", "room-e")}
	mergeStale(unknown, func(string) map[string]string { return nil }, func(string) []ctl.Card { return nil })
	if unknown[0].Stale != "" {
		t.Fatalf("a request was resolved on no answer: %q", unknown[0].Stale)
	}
}
