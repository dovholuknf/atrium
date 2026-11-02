// Package cardproc is what a card's inventory needs from the operating system for its processes and ports: when a
// process started, so a recycled pid is never taken for the one recorded, how to stop a process and its children,
// and which ports the system has reserved. Design: docs/rnd/card-lifecycle-design.md section 9. Item
// r-card-procs-ports.
//
// It runs nothing on its own. The process registry (docs/rnd/process-registry-design.md) is a separate item.
package cardproc

import (
	"bufio"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrGone is a pid with no process behind it.
var ErrGone = errors.New("no such process")

// WaitGone waits up to within for every process of tree, pid to start time as Tree reads it, to exit, and answers the
// pids still running.
func WaitGone(tree map[int]string, within time.Duration) []int {
	deadline := time.Now().Add(within)
	for {
		var left []int
		for pid, at := range tree {
			if now, err := StartTime(pid); err == nil && now == at {
				left = append(left, pid)
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			sort.Ints(left)
			return left
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// treeOf is root and every pid under it in a parent table, with start times. A child whose after check fails is a
// stranger on a recycled parent pid, not a child.
func treeOf(root int, kids map[int][]int, after func(child, parent string) bool) map[int]string {
	out := map[int]string{}
	at, err := StartTime(root)
	if err != nil {
		return out
	}
	out[root] = at
	for todo := []int{root}; len(todo) > 0; todo = todo[1:] {
		p := todo[0]
		for _, c := range kids[p] {
			if _, seen := out[c]; seen {
				continue
			}
			cat, err := StartTime(c)
			if err != nil || (after != nil && !after(cat, out[p])) {
				continue
			}
			out[c] = cat
			todo = append(todo, c)
		}
	}
	return out
}

// Proc is one process: its pid and its executable's base name, lowercased and without an extension.
type Proc struct {
	PID  int
	Name string
}

// ancestorsOf walks up from pid through look, which answers a pid's executable and parent. A cyclic or corrupt parent
// chain stops at hops.
func ancestorsOf(pid, hops int, look func(int) (string, int, bool)) []Proc {
	_, at, ok := look(pid)
	if !ok {
		return nil
	}
	var out []Proc
	for i := 0; i < hops && at > 0; i++ {
		name, parent, ok := look(at)
		if !ok {
			break
		}
		out = append(out, Proc{PID: at, Name: baseName(name)})
		at = parent
	}
	return out
}

func baseName(exe string) string {
	n := strings.ToLower(exe)
	if i := strings.LastIndexAny(n, `/\`); i >= 0 {
		n = n[i+1:]
	}
	if i := strings.LastIndex(n, "."); i > 0 {
		n = n[:i]
	}
	return n
}

// Range is a closed range of ports.
type Range struct{ Lo, Hi int }

// Has is whether p is in the range.
func (r Range) Has(p int) bool { return p >= r.Lo && p <= r.Hi }

func (r Range) String() string { return strconv.Itoa(r.Lo) + "-" + strconv.Itoa(r.Hi) }

// ParseRange reads "lo-hi".
func ParseRange(s string) (Range, error) {
	lo, hi, ok := strings.Cut(strings.TrimSpace(s), "-")
	a, err1 := strconv.Atoi(strings.TrimSpace(lo))
	b, err2 := strconv.Atoi(strings.TrimSpace(hi))
	if !ok || err1 != nil || err2 != nil || a < 1024 || b > 65535 || a > b {
		return Range{}, errors.New("a port range is lo-hi, between 1024 and 65535, like 41000-41199")
	}
	return Range{a, b}, nil
}

// parseExcluded reads the table `netsh int ipv4 show excludedportrange protocol=tcp` prints: every line that starts
// with two numbers is a range, and the rest is headings.
func parseExcluded(out string) []Range {
	var rs []Range
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		a, err1 := strconv.Atoi(f[0])
		b, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil || a > b {
			continue
		}
		rs = append(rs, Range{a, b})
	}
	return rs
}
