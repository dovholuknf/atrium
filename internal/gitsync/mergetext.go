package gitsync

import (
	"fmt"
	"regexp"
	"strings"
)

// The bounds of what a merge run says, one set for the room that cuts and the hub that stores.
const (
	MergeReasonMax  = 500
	MergeFailingMax = 50
	MergeNameMax    = 200
	MergeOutputMax  = 64 << 10
	mergeOutputHead = 8 << 10
)

var ansiRe = regexp.MustCompile(`\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

// MergeClean is one line of text for a reason or a failing name: escape sequences and control characters gone.
func MergeClean(s string) string {
	s = ansiRe.ReplaceAllString(strings.ToValidUTF8(s, "?"), "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// MergeCut keeps the head and the tail of an output that is over MergeOutputMax, at line ends where it can.
func MergeCut(out string) string {
	out = strings.ToValidUTF8(out, "?")
	if len(out) <= MergeOutputMax {
		return out
	}
	head := out[:mergeOutputHead]
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		head = head[:i+1]
	}
	tail := out[len(out)-(MergeOutputMax-mergeOutputHead-64):]
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i < len(tail)-1 {
		tail = tail[i+1:]
	}
	tail = strings.ToValidUTF8(tail, "")
	return head + fmt.Sprintf("\n[... %d bytes cut ...]\n\n", len(out)-len(head)-len(tail)) + tail
}

// mergeCutReason is a reason cleaned and within MergeReasonMax.
func mergeCutReason(s string) string {
	s = MergeClean(s)
	if len(s) > MergeReasonMax {
		s = strings.ToValidUTF8(s[:MergeReasonMax-4], "") + " ..."
	}
	return s
}

// KnownFailingFile lists the tests that fail for reasons outside a change, one "- `Name`" line each.
const KnownFailingFile = "docs/known-failing-tests.md"

var (
	redTestRe    = regexp.MustCompile(`--- FAIL: (\w+)`)
	budgetMissRe = regexp.MustCompile(`(?i)unit.budget|over the 20ms|exceeded the budget`)
)

// RedRerun is whether a red test-touched output is a 20ms unit budget miss, which earns one rerun.
func RedRerun(out string) bool { return budgetMissRe.MatchString(out) }

// RedKnown is whether a red test-touched output failed only tests named in known, the text of KnownFailingFile.
// One with no failing test named is not.
func RedKnown(out, known string) bool {
	names := map[string]bool{}
	for _, l := range strings.Split(known, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "- `") {
			names[strings.Trim(strings.SplitN(l[2:], "`", 3)[1], "` ")] = true
		}
	}
	failing := redTestRe.FindAllStringSubmatch(out, -1)
	for _, f := range failing {
		if !names[f[1]] {
			return false
		}
	}
	return len(failing) > 0
}
