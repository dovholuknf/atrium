package api

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Reading a review's run folder. The index never holds a finding, so every count
// the pulls routes return is read from here at the moment of the request.

// PRFindingCounts is the findings of a run by severity. A leak is also counted
// under its severity.
type PRFindingCounts struct {
	High int `json:"high"`
	Med  int `json:"med"`
	Low  int `json:"low"`
	Nit  int `json:"nit"`
	Leak int `json:"leak"`
}

// PRWalkCounts is the findings of a run by walk state.
type PRWalkCounts struct {
	Done     int `json:"done"`
	Skipped  int `json:"skipped"`
	Deferred int `json:"deferred"`
	Open     int `json:"open"`
}

// findingName splits NN-<sev>-<rest>.txt. `medium` is accepted for `med` because
// the files a person wrote by hand before the daemon rendered them use it.
var findingName = regexp.MustCompile(`^(\d+)-(high|medium|med|low|nit)-.+\.txt$`)

// findingSev returns the position and normalised severity a file name carries.
func findingSev(name string) (pos int, sev string, ok bool) {
	m := findingName.FindStringSubmatch(name)
	if m == nil {
		return 0, "", false
	}
	for _, c := range m[1] {
		pos = pos*10 + int(c-'0')
	}
	sev = m[2]
	if sev == "medium" {
		sev = "med"
	}
	return pos, sev, true
}

// hasLeakLine reports whether a finding file carries a `Leak:` line.
func hasLeakLine(file string) bool {
	f, err := os.Open(file)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimLeft(sc.Text(), " \t*-")
		if strings.HasPrefix(line, "Leak:") && strings.TrimSpace(line[len("Leak:"):]) != "" {
			return true
		}
	}
	return false
}

// readWalk reads walk.txt into file name -> state. A missing file is an empty
// map, which makes every finding open.
func readWalk(dir string) map[string]walkLine {
	out := map[string]walkLine{}
	f, err := os.Open(filepath.Join(dir, "walk.txt"))
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if l, ok := parseWalkLine(sc.Text()); ok {
			out[l.File] = l
		}
	}
	return out
}

// walkLine is one line of walk.txt: file, state, time and an optional url.
type walkLine struct {
	File  string
	State string
	At    string
	URL   string
}

func parseWalkLine(s string) (walkLine, bool) {
	f := strings.Fields(s)
	if len(f) < 2 {
		return walkLine{}, false
	}
	l := walkLine{File: f[0], State: f[1]}
	switch l.State {
	case "open", "done", "skipped", "deferred":
	default:
		return walkLine{}, false
	}
	if len(f) > 2 {
		l.At = f[2]
	}
	if len(f) > 3 {
		l.URL = f[3]
	}
	return l, true
}

// readPRCounts counts the findings and their walk states. Every count is zero
// when the folder, or findings/ in it, is not there.
func readPRCounts(dir string) (PRFindingCounts, PRWalkCounts) {
	var fc PRFindingCounts
	var wc PRWalkCounts
	if dir == "" {
		return fc, wc
	}
	dir = filepath.FromSlash(dir)
	ents, err := os.ReadDir(filepath.Join(dir, "findings"))
	if err != nil {
		return fc, wc
	}
	walk := readWalk(dir)
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		_, sev, ok := findingSev(e.Name())
		if !ok {
			continue
		}
		switch sev {
		case "high":
			fc.High++
		case "med":
			fc.Med++
		case "low":
			fc.Low++
		case "nit":
			fc.Nit++
		}
		if hasLeakLine(filepath.Join(dir, "findings", e.Name())) {
			fc.Leak++
		}
		switch walk[e.Name()].State {
		case "done":
			wc.Done++
		case "skipped":
			wc.Skipped++
		case "deferred":
			wc.Deferred++
		default:
			wc.Open++
		}
	}
	return fc, wc
}

// tailLog returns the last max bytes of run.log, cut at a line start.
func tailLog(dir string, max int64) string {
	if dir == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(filepath.FromSlash(dir), "run.log"))
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	start := int64(0)
	if fi.Size() > max {
		start = fi.Size() - max
	}
	buf := make([]byte, fi.Size()-start)
	n, _ := f.ReadAt(buf, start)
	s := string(buf[:n])
	if start > 0 {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		} else {
			s = ""
		}
	}
	return s
}
