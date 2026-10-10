package store

import (
	"bufio"
	"regexp"
	"strings"
	"unicode/utf8"
)

// The caps on what OpenQuestions returns. The store bounds a list again on the way in.
const (
	turnQuestionMax = 10
	turnQuestionLen = 500
)

// The heading, read leniently: `Open Questions:`, bold, or a markdown heading.
var (
	oqHeading = regexp.MustCompile(`(?i)^\s*(#{1,6}\s*)?(\*\*|__)?\s*open questions\s*:?\s*(\*\*|__)?\s*:?\s*$`)
	oqItem    = regexp.MustCompile(`^(\s{0,3})(\d{1,2})[.)]\s+(.*)$`)
	oqRule    = regexp.MustCompile(`^\s*(-{3,}|\*{3,}|_{3,})\s*$`)
)

// OpenQuestions extracts the numbered items of the LAST Open Questions block.
//
// The one reading of a message's questions: the Stop hook uses it for the seen view, and the
// daemon uses it to tell a worker's question from its report.
//
// The last, because a message that quotes an earlier block and then asks its
// own means the second. An indented line continues the item above it. The
// first non-blank line after an item that is neither an item nor indented ends
// the block. Only top-level numbers count: a numbered line indented deeper than
// the first item is a sub-item, and reads as part of the item above it.
func OpenQuestions(text string) (list []string, block bool) {
	lines := []string{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		lines = append(lines, strings.TrimRight(sc.Text(), "\r"))
	}
	start := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if oqHeading.MatchString(lines[i]) {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, false
	}
	var cur *strings.Builder
	base := -1
	flush := func() {
		if cur == nil {
			return
		}
		q := strings.TrimSpace(cur.String())
		if q != "" && len(list) < turnQuestionMax {
			if len(q) > turnQuestionLen {
				q = q[:turnQuestionLen]
				// Not in the middle of a character.
				for !utf8.ValidString(q) {
					q = q[:len(q)-1]
				}
			}
			list = append(list, q)
		}
		cur = nil
	}
	for _, l := range lines[start+1:] {
		switch {
		case strings.TrimSpace(l) == "", oqRule.MatchString(l):
			continue
		case oqItem.MatchString(l) && !(cur != nil && len(oqItem.FindStringSubmatch(l)[1]) > base):
			flush()
			m := oqItem.FindStringSubmatch(l)
			if base < 0 {
				base = len(m[1])
			}
			cur = &strings.Builder{}
			cur.WriteString(strings.TrimSpace(m[3]))
		case cur != nil && (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")):
			cur.WriteString(" ")
			cur.WriteString(strings.TrimSpace(l))
		default:
			// Prose after the list, or prose where a list should be.
			flush()
			return list, true
		}
	}
	flush()
	return list, true
}
