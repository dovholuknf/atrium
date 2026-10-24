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
