package itemgate

import (
	"strings"
	"testing"
)

func TestParseTargetTellsAnItemFromACondition(t *testing.T) {
	for in, want := range map[string]Target{
		"r-037":                     {KindItem, "r-037"},
		" 91 ":                      {KindItem, "91"},
		"r-new-item-dependencies":   {KindItem, "r-new-item-dependencies"},
		"live:u-033":                {KindCond, "live:u-033"},
		"room:sg3":                  {KindCond, "room:sg3"},
		"sha:4815d47":               {KindCond, "sha:4815d47"},
		"f-006 migration on m1mini": {KindCond, "f-006 migration on m1mini"},
	} {
		got, err := ParseTarget(in)
		if err != nil || got != want {
			t.Errorf("ParseTarget(%q) = %+v, %v, want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "  ", "live:", "room: ", "sha:", strings.Repeat("x ", MaxTarget)} {
		if _, err := ParseTarget(bad); err == nil {
			t.Errorf("ParseTarget(%q) should refuse", bad)
		}
	}
}

func TestItemOfTitleReadsTheIdBeforeTheColon(t *testing.T) {
	for in, want := range map[string]string{
		"r-038: build the thing": "r-038",
		" u-033 : board chip":    "u-033",
		"r-038":                  "r-038",
		"fix the build":          "",
		"":                       "",
		"a/b: x":                 "",
		// r-new-review-7f4e76c0 item 4: a numbered id followed by a space counts, a word
		// does not.
		"r-037 fix the reaper": "r-037",
		"r-037 - fix":          "r-037",
		"91 the old one":       "91",
		"atrium fix the build": "",
	} {
		if got := ItemOfTitle(in); got != want {
			t.Errorf("ItemOfTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLandedIDTakesTheWholeNameAfterTheDate(t *testing.T) {
	for in, want := range map[string]string{
		"changelog/runtime/2026-09-30-r-007.md":                "r-007",
		"changelog/runtime/2026-09-30-r-007-3.md":              "r-007-3",
		"changelog/rnd/2026-09-30-item-dependencies-design.md": "item-dependencies-design",
	} {
		if got, ok := LandedID(in); !ok || got != want {
			t.Errorf("LandedID(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"changelog/2026-09-30-r-007.md", "changelog/runtime/r-007.md",
		"docs/runtime/2026-09-30-r-007.md", "changelog/a/b/2026-09-30-r-007.md", "changelog/runtime/2026-09-30-.md"} {
		if id, ok := LandedID(bad); ok {
			t.Errorf("LandedID(%q) = %q, should not count", bad, id)
		}
	}
}

func TestFindPathAndTheLoopItNames(t *testing.T) {
	edges := map[string][]string{"r-041": {"r-042"}, "r-042": {"r-040"}}
	loop := FindPath(edges, "r-041", "r-040")
	if strings.Join(loop, ",") != "r-041,r-042,r-040" {
		t.Fatalf("path = %v", loop)
	}
	err := LoopError(append([]string{"r-040"}, loop...))
	if !strings.HasPrefix(err.Error(), "r-040 waits on r-041, which waits on r-042, which waits on r-040") {
		t.Fatalf("error = %v", err)
	}
	if FindPath(edges, "r-040", "r-041") != nil {
		t.Fatal("no edge leaves r-040")
	}
}

func TestBoundCutsOnARune(t *testing.T) {
	if got := Bound("ééé", 5); got != "éé" {
		t.Fatalf("got %q", got)
	}
}
