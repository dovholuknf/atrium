package gitsync

import (
	"strings"
	"testing"
)

func TestMergeCutKeepsTheHeadAndTheTail(t *testing.T) {
	long := "first line\n" + strings.Repeat("x\n", MergeOutputMax) + "--- FAIL: TestLast\n"
	got := MergeCut(long)
	if len(got) > MergeOutputMax || !strings.HasPrefix(got, "first line\n") || !strings.HasSuffix(got, "--- FAIL: TestLast\n") ||
		!strings.Contains(got, "bytes cut") {
		t.Fatalf("cut: %d bytes, head %q, tail %q", len(got), got[:20], got[len(got)-30:])
	}
	if MergeCut("short") != "short" {
		t.Fatal("a short output must be kept as it is")
	}
}

func TestMergeCleanLeavesOneLineOfText(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[31mTestA\x1b[0m":     "TestA",
		"TestB\r":                  "TestB",
		"a\x00b\x07c":              "abc",
		"\x1b]0;title\x07TestC":    "TestC",
		"two\nlines":               "two lines",
		"plain (internal/link) ok": "plain (internal/link) ok",
	} {
		if got := MergeClean(in); got != want {
			t.Errorf("MergeClean(%q) = %q, want %q", in, got, want)
		}
	}
}
