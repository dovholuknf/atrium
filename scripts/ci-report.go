//go:build ignore

// Reads `go test -json` on stdin and prints what a person reading a CI log
// needs, as it happens and again at the end. Run by scripts/ci.sh:
//
//	go test -json ./... | go run scripts/ci-report.go -dir build.claude/ci
//
// THE LOG HAS TO EXPLAIN A FAILURE BY ITSELF. Plain `go test` prints the whole
// output of a failing package, which for internal/daemon is thousands of log
// lines from fifteen hundred tests that passed, with the one that failed
// somewhere inside. This prints, per package, one ok or FAIL line with its
// time; per failed test, its own output and nothing else; for a package that
// died without a failing test (a timeout, a panic, a crash), the end of its
// output, which is where the panic and the running tests are; and at the end,
// every failure again, the slowest tests and the slowest packages.
//
// Into -dir it writes what is too big for the log: <package>.txt with the
// whole output of each failed package, and timings.tsv with every test's
// time. scripts/ci.sh also tees the raw stream there as go-test.json.
//
// Exit status 1 when anything failed, so the caller does not have to parse.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type event struct {
	Time       time.Time
	Action     string
	Package    string
	ImportPath string
	Test       string
	Output     string
	Elapsed    float64
}

// What is printed of one failed test or package before the rest is left to
// the file: the start, where the setup said what it did, and the end, where
// the failure is.
const headLines, tailLines = 60, 340

type pkgState struct {
	out      *os.File
	path     string
	loose    []string // output that belongs to no test: its first headLines and its latest lines
	dropped  int      // lines of loose let go from between the two
	failed   int
	started  time.Time
	finished bool
}

type timing struct {
	pkg, test string
	secs      float64
}

func main() {
	dir := flag.String("dir", "build.claude/ci", "where the files go")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "ci-report:", err)
	}

	pkgs := map[string]*pkgState{}
	tests := map[string][]string{} // pkg + " " + test: its output until it ends
	var times []timing
	var pkgTimes []timing
	var failures []string
	bad := false

	pkgOf := func(name string) *pkgState {
		p := pkgs[name]
		if p == nil {
			p = &pkgState{path: filepath.Join(*dir, strings.ReplaceAll(name, "/", "_")+".txt"), started: time.Now()}
			p.out, _ = os.Create(p.path)
			pkgs[name] = p
		}
		return p
	}

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		var e event
		if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &e) != nil {
			// Not an event: go itself talking, a build error, a vet failure.
			fmt.Println(string(line))
			if strings.HasPrefix(string(line), "FAIL") {
				bad = true
			}
			continue
		}
		switch e.Action {
		case "build-output":
			fmt.Print(e.Output)
			continue
		case "build-fail":
			fmt.Printf("FAIL\t%s [build failed]\n", e.ImportPath)
			failures = append(failures, e.ImportPath+" [build failed]")
			bad = true
			continue
		}
		if e.Package == "" {
			continue
		}
		p := pkgOf(e.Package)
		key := e.Package + " " + e.Test
		switch e.Action {
		case "output":
			if p.out != nil {
				_, _ = p.out.WriteString(e.Output)
			}
			if e.Test != "" {
				tests[key] = append(tests[key], e.Output)
			} else {
				p.loose = append(p.loose, e.Output)
				if len(p.loose) > 2*(headLines+tailLines) {
					p.dropped += len(p.loose) - headLines - tailLines
					p.loose = append(p.loose[:headLines], p.loose[len(p.loose)-tailLines:]...)
				}
			}
		case "pass", "skip":
			if e.Test != "" {
				if e.Action == "pass" {
					times = append(times, timing{e.Package, e.Test, e.Elapsed})
				}
				delete(tests, key)
				continue
			}
			p.finished = true
			pkgTimes = append(pkgTimes, timing{e.Package, "", e.Elapsed})
			if p.out != nil {
				_ = p.out.Close()
				_ = os.Remove(p.path)
			}
			if e.Action == "pass" {
				fmt.Printf("ok\t%s\t%.1fs\n", e.Package, e.Elapsed)
			}
		case "fail":
			bad = true
			if e.Test != "" {
				p.failed++
				times = append(times, timing{e.Package, e.Test, e.Elapsed})
				name := fmt.Sprintf("%s %s (%.2fs)", e.Package, e.Test, e.Elapsed)
				failures = append(failures, name)
				fmt.Printf("\n--- FAIL: %s\n", name)
				printCapped(tests[key], 0, p.path)
				delete(tests, key)
				continue
			}
			p.finished = true
			pkgTimes = append(pkgTimes, timing{e.Package, "", e.Elapsed})
			if p.out != nil {
				_ = p.out.Close()
			}
			fmt.Printf("FAIL\t%s\t%.1fs\n", e.Package, e.Elapsed)
			if p.failed == 0 {
				// No test failed and yet the package did: a timeout, a panic, a
				// crash, or a TestMain that exited non-zero. What says which is at
				// the end of what it printed, and the tests still running are
				// whatever has output and no end.
				failures = append(failures, e.Package+" [the package failed with no failing test: see its output above]")
				fmt.Printf("\n--- %s failed with no failing test. the end of its output:\n", e.Package)
				printCapped(p.loose, p.dropped, p.path)
				for k, out := range tests {
					if strings.HasPrefix(k, e.Package+" ") {
						fmt.Printf("\n--- still running when %s ended: %s. its output:\n", e.Package, strings.TrimPrefix(k, e.Package+" "))
						printCapped(out, 0, p.path)
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Println("ci-report: reading go test's output:", err)
		bad = true
	}
	for name, p := range pkgs {
		if !p.finished {
			bad = true
			failures = append(failures, name+" [never finished: go test was killed or crashed]")
			fmt.Printf("\n--- %s never finished. the end of its output:\n", name)
			printCapped(p.loose, p.dropped, p.path)
			if p.out != nil {
				_ = p.out.Close()
			}
		}
	}

	writeTimings(filepath.Join(*dir, "timings.tsv"), times)
	fmt.Println()
	fmt.Println("=== slowest tests")
	sort.Slice(times, func(i, j int) bool { return times[i].secs > times[j].secs })
	for i := 0; i < len(times) && i < 15; i++ {
		fmt.Printf("%8.2fs  %s %s\n", times[i].secs, times[i].pkg, times[i].test)
	}
	fmt.Println("=== slowest packages")
	sort.Slice(pkgTimes, func(i, j int) bool { return pkgTimes[i].secs > pkgTimes[j].secs })
	for i := 0; i < len(pkgTimes) && i < 8; i++ {
		fmt.Printf("%8.1fs  %s\n", pkgTimes[i].secs, pkgTimes[i].pkg)
	}
	if len(failures) > 0 {
		fmt.Println("=== failed")
		sort.Strings(failures)
		for _, f := range failures {
			fmt.Println("  " + f)
		}
		fmt.Printf("the whole output of each failed package is in %s, uploaded with the run\n", *dir)
	}
	if bad {
		os.Exit(1)
	}
}

// printCapped prints lines, or their start and end with a note of where the
// rest is when there are too many. dropped is how many the caller already let
// go from between the two.
func printCapped(lines []string, dropped int, file string) {
	if len(lines) > headLines+tailLines || dropped > 0 {
		for _, l := range lines[:min(headLines, len(lines))] {
			fmt.Print(l)
		}
		lines = lines[min(headLines, len(lines)):]
		cut := dropped + max(0, len(lines)-tailLines)
		fmt.Printf("\n    ... %d lines cut here, all of them in %s ...\n\n", cut, file)
		lines = lines[max(0, len(lines)-tailLines):]
	}
	for _, l := range lines {
		fmt.Print(l)
	}
}

func writeTimings(path string, times []timing) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	fmt.Fprintln(w, "seconds\tpackage\ttest")
	for _, t := range times {
		fmt.Fprintf(w, "%.3f\t%s\t%s\n", t.secs, t.pkg, t.test)
	}
}
