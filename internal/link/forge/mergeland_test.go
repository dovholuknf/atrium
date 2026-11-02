package forge

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

func TestMergeLandMessageIsOneLineCutShortWithACount(t *testing.T) {
	got := mergeLandMessage(3, []hubstore.MergeRequest{{N: 12, Branch: "claude/foo"}, {N: 13, Branch: "claude/bar"}})
	if got != "Land 3: #12 foo, #13 bar\n" {
		t.Fatalf("%q", got)
	}
	var many []hubstore.MergeRequest
	for i := int64(1); i <= 40; i++ {
		many = append(many, hubstore.MergeRequest{N: i, Branch: "claude/a-rather-long-branch-name"})
	}
	got = mergeLandMessage(4, many)
	if strings.Count(got, "\n") != 1 || len(got) > mergeLandMsgMax+1 || !strings.Contains(got, " more\n") {
		t.Fatalf("%d %q", len(got), got)
	}
}
