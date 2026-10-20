//go:build ignore

// Tests of the time guard in ci-report.go. ci-report.go is `go run` and not a package, so name both files:
//
//	go test scripts/ci-report.go scripts/ci-report_test.go
package main

import (
	"strings"
	"testing"
)

func TestParseAllowReadsPackagesTestsAndWildcards(t *testing.T) {
	got, err := parseAllow(strings.NewReader("# c\n\ninternal/a 90 slow pkg\ninternal/a.TestX 12.5 a real timer\ninternal/b.* 30 all of b\n"), "f")
	if err != nil {
		t.Fatal(err)
	}
	want := []allowEntry{
		{"internal/a", "", 90, "slow pkg"},
		{"internal/a", "TestX", 12.5, "a real timer"},
		{"internal/b", "*", 30, "all of b"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestParseAllowRefusesALineWithNoReasonOrABadCeiling(t *testing.T) {
	for _, bad := range []string{"internal/a 90\n", "internal/a\n", "internal/a ninety why\n", "internal/a 0 why\n", "internal/a -3 why\n"} {
		if _, err := parseAllow(strings.NewReader(bad), "f"); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestOverLimit(t *testing.T) {
	allow, _ := parseAllow(strings.NewReader("internal/slow 120 why\ninternal/x.TestSlow 10 why\ninternal/w.* 20 why\n"), "f")
	tests := []timing{
		{"m/internal/x", "TestFast", 4.9},
		{"m/internal/x", "TestOver", 5.1},
		{"m/internal/x", "TestSlow", 9},
		{"m/internal/x", "TestSlow/sub", 9.5},
		{"m/internal/x", "TestSlow/sub2", 10.5},
		{"m/internal/w", "TestAnything", 19},
		{"m/internal/w", "TestAnythingElse", 21},
		{"m/internal/notx", "TestSlow", 9},
	}
	pkgs := []timing{
		{"m/internal/x", "", 60},
		{"m/internal/slow", "", 100},
		{"m/internal/slow", "", 121},
		{"m/internal/other", "", 60.1},
		{"m/internal/xslow", "", 61},
	}
	got := strings.Join(overLimit(tests, pkgs, allow, 60, 5), "\n")
	for _, want := range []string{
		"WARN TEST m/internal/x TestOver took 5.10s, the limit is 5s:",
		"WARN TEST m/internal/x TestSlow/sub2 took 10.50s, the limit is 10s (its allowlist ceiling)",
		"WARN TEST m/internal/w TestAnythingElse took 21.00s, the limit is 20s",
		"WARN TEST m/internal/notx TestSlow took 9.00s, the limit is 5s:",
		"WARN PACKAGE m/internal/slow took 121.0s, the limit is 120s",
		"WARN PACKAGE m/internal/other took 60.1s, the limit is 60s:",
		"WARN PACKAGE m/internal/xslow took 61.0s, the limit is 60s:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "\n") + 1; n != 7 {
		t.Errorf("want 7 lines, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "add a line to scripts/test-time-allow.txt") {
		t.Errorf("the message does not say what to do:\n%s", got)
	}
}

func TestTheCheckedInAllowlistParses(t *testing.T) {
	if _, err := loadAllow("test-time-allow.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerOfNamesTheBucketAnExactNameBeatsAGlobAndRestTakesTheRest(t *testing.T) {
	defs := map[string]bucketDef{
		"hub/items": {Package: "./internal/link", Tests: []string{"internal/link/items_integration_test.go"}},
		"mcp/items": {Package: "./internal/link", Tests: []string{"internal/link/items_mcp_*test.go", "internal/link/*_test.go"}},
		"hub/rest":  {Package: "./internal/link", Rest: true},
		"store":     {Package: "./internal/store", Tests: []string{"internal/store/*_test.go"}},
	}
	for file, want := range map[string]string{
		"internal/link/items_integration_test.go":     "hub/items",
		"internal/link/items_mcp_integration_test.go": "mcp/items",
		"internal/link/zzz_test.go":                   "mcp/items",
		"internal/store/a_test.go":                    "store",
		"internal/other/a_test.go":                    "",
	} {
		if got := ownerOf(defs, file); got != want {
			t.Errorf("%s: got %q want %q", file, got, want)
		}
	}
	delete(defs, "mcp/items")
	defs["mcp/items"] = bucketDef{Package: "./internal/link", Tests: []string{"internal/link/items_mcp_*test.go"}}
	if got := ownerOf(defs, "internal/link/zzz_test.go"); got != "hub/rest" {
		t.Errorf("a file no bucket owns: got %q want hub/rest", got)
	}
}

func TestBucketOfATestAnswersForASubtestAsItsParentAndForAnImportPath(t *testing.T) {
	ix := &bucketIndex{byTest: map[string]string{"internal/link TestBacklog": "hub/backlog"}}
	for _, test := range []string{"TestBacklog", "TestBacklog/sub"} {
		if got := ix.of("github.com/x/atrium/internal/link", test); got != "hub/backlog" {
			t.Errorf("%s: got %q", test, got)
		}
	}
	if got := ix.of("github.com/x/atrium/internal/api", "TestBacklog"); got != "" {
		t.Errorf("another package: got %q", got)
	}
	stats := map[string]*bucketStat{}
	ix.count(stats, "m/internal/link", "TestBacklog", 1.5, false)
	ix.count(stats, "m/internal/link", "TestBacklog/sub", 1.5, true)
	ix.count(stats, "m/internal/link", "TestBacklog", 0.5, true)
	if s := stats["hub/backlog"]; s == nil || s.tests != 2 || s.failed != 1 || s.secs != 2 {
		t.Errorf("stats %+v", s)
	}
}
