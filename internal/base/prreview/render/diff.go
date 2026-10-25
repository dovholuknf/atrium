package render

import (
	"regexp"
	"strconv"
	"strings"
)

// diffInfo is what pr.diff says about the head: which lines each file adds, and the order files appear in.
type diffInfo struct {
	order []string
	added map[string]map[int]bool
}

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func parseDiff(text string) diffInfo {
	d := diffInfo{added: map[string]map[int]bool{}}
	var cur string
	next := 0
	inHunk := false
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			cur, inHunk = "", false
		case !inHunk && strings.HasPrefix(l, "+++ "):
			p := strings.TrimPrefix(l, "+++ ")
			if i := strings.Index(p, "\t"); i >= 0 {
				p = p[:i] // git ends an unquoted name holding a space with a tab
			}
			if strings.HasPrefix(p, `"`) {
				if u, err := strconv.Unquote(p); err == nil {
					p = u // git quotes a name holding a space or a non-ASCII byte
				}
			}
			if p == "/dev/null" {
				cur = ""
				continue
			}
			cur = strings.TrimPrefix(p, "b/")
			if d.added[cur] == nil {
				d.added[cur] = map[int]bool{}
				d.order = append(d.order, cur)
			}
		case cur != "" && hunkRe.MatchString(l):
			n, _ := strconv.Atoi(hunkRe.FindStringSubmatch(l)[1])
			next, inHunk = n, true
		case inHunk && strings.HasPrefix(l, "+"):
			d.added[cur][next] = true
			next++
		case inHunk && strings.HasPrefix(l, " "):
			next++
		}
	}
	return d
}

// file finds the diff's name for a finding's path: exact, else the ones that end at a path boundary. n is how many
// matched. Only n == 1 names a file, and the renderer then uses the diff's name, never the finding's.
func (d diffInfo) file(p string) (name string, n int) {
	p = strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "./")
	if _, ok := d.added[p]; ok {
		return p, 1
	}
	for _, f := range d.order {
		if strings.HasSuffix(f, "/"+p) || strings.HasSuffix(p, "/"+f) {
			name = f
			n++
		}
	}
	return name, n
}

// adds says whether the PR adds or changes this line at the head, and whether the path named more than one file.
func (d diffInfo) adds(p string, line int) (ok, ambiguous bool) {
	f, n := d.file(p)
	return n == 1 && d.added[f][line], n > 1
}

// position is where a finding's file sits in the diff, for ties. Files the diff does not name sort last.
func (d diffInfo) position(p string) int {
	f, n := d.file(p)
	if n != 1 {
		return len(d.order)
	}
	for i, o := range d.order {
		if o == f {
			return i
		}
	}
	return len(d.order)
}
