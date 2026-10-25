package api

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/forge"
)

func TestPRBriefNamesTheLocalFactsAndAsksForNoRemoteCall(t *testing.T) {
	got := prBrief(&forge.PR{Title: "Fix it", BaseRef: "main", MergeBase: "abc123"}, true)
	for _, want := range []string{"Title: Fix it", "Base branch: main", "Merge base: abc123", "git diff abc123 HEAD",
		prBriefFile, "Do not fetch, pull or run gh"} {
		if !strings.Contains(got, want) {
			t.Errorf("the brief lacks %q: %s", want, got)
		}
	}
	// the only mention of a remote call is the refusal
	rest := strings.Replace(got, "Do not fetch, pull or run gh", "", 1)
	for _, bad := range []string{"fetch", "gh ", "pull"} {
		if strings.Contains(rest, bad) {
			t.Errorf("the brief asks for %q: %s", bad, got)
		}
	}
	if prBrief(nil, false) != "" {
		t.Error("no pull request, no brief")
	}
	if strings.Contains(prBrief(&forge.PR{Title: "t", BaseRef: "main"}, false), prBriefFile) {
		t.Error("the file is named though none was written")
	}
}
