package cli

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

func TestMergeRepoOfHubURL(t *testing.T) {
	for raw, want := range map[string]string{
		"http://127.0.0.1:7787/git/hub/github/dovholuknf/atrium.git": "github/dovholuknf/atrium",
		"http://127.0.0.1:7787/git/hub/github.com/o/r":               "github/o/r",
		"http://127.0.0.1:7787/git/other/github/o/r.git":             "",
		"git@github.com:o/r.git":                                     "",
		"http://127.0.0.1:7787/git/hub/../etc.git":                   "",
	} {
		if got := mergeRepoOfHubURL(raw); got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
}

func TestMergeRowSaysWhatARequestCameTo(t *testing.T) {
	landed := mergeRow(hubstore.MergeRequest{N: 3, State: hubstore.MergeLanded, Branch: "claude/a", Landed: "0123456789", EndedAt: "t2"})
	red := mergeRow(hubstore.MergeRequest{N: 4, State: hubstore.MergeRed, Branch: "claude/b", Failing: []string{"TestA", "TestB"}})
	if !strings.Contains(landed, "0123456") || strings.Contains(landed, "0123456789") || !strings.Contains(landed, "t2") {
		t.Errorf("landed: %q", landed)
	}
	if !strings.HasSuffix(red, "TestA, TestB") {
		t.Errorf("red: %q", red)
	}
}
