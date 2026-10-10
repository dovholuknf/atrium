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
//
// With -guard-only it prints nothing of the above. It reads the same stream, and
// says only which packages and tests ran longer than the limits and are not on
// the allowlist. scripts/test-time-guard.sh runs it that way. See guard below.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	guardOnly := flag.Bool("guard-only", false, "print only the time guard's verdict")
	allowPath := flag.String("allow", "scripts/test-time-allow.txt", "the time guard's allowlist")
	pkgLimit := flag.Float64("pkg-limit", defaultPkgLimit, "seconds a package may take before it needs an allowlist line")
	testLimit := flag.Float64("test-limit", defaultTestLimit, "seconds a test may take before it needs an allowlist line")
	flag.Parse()
	var allow []allowEntry
	realOut := os.Stdout
	if *guardOnly {
		var err error
		if allow, err = loadAllow(*allowPath); err != nil {
			fmt.Fprintln(os.Stderr, "ci-report:", err)
			os.Exit(2)
		}
		// The loop below prints as it reads. Here that goes nowhere, and what it writes to
		// -dir goes to a folder that is removed after.
		if devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
			os.Stdout = devnull
		}
		tmp, err := os.MkdirTemp("", "ci-report-guard")
		if err != nil {
			fmt.Fprintln(os.Stderr, "ci-report:", err)
			os.Exit(2)
		}
		defer os.RemoveAll(tmp)
		*dir = tmp
	}
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
	if *guardOnly {
		os.Stdout = realOut
		over := overLimit(times, pkgTimes, allow, *pkgLimit, *testLimit)
		for _, m := range over {
			fmt.Println(m)
		}
		if len(over) > 0 {
			fmt.Printf("%d over the time limits. see scripts/test-time-allow.txt\n", len(over))
			_ = os.RemoveAll(*dir)
			os.Exit(1)
		}
		fmt.Printf("ok: no package over %.0fs and no test over %.0fs that is not on the allowlist (%d lines)\n",
			*pkgLimit, *testLimit, len(allow))
		_ = os.RemoveAll(*dir)
		return
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

// THE TIME GUARD. A suite that has been made fast stays fast only if something
// fails when it is not. A package past defaultPkgLimit seconds, or one test past
// defaultTestLimit, fails the run unless scripts/test-time-allow.txt has a line
// for it. A line carries a ceiling in seconds, which replaces the default for
// that package or test, and a reason. The ceilings are a ratchet: set a little
// over what the thing takes today, and lower when it is made faster.
const (
	defaultPkgLimit  = 60
	defaultTestLimit = 5
)

// allowEntry is one line of the allowlist. test is empty for a package line, a
// test name for Pkg.TestName, and "*" for every test of the package.
type allowEntry struct {
	pkg, test string
	ceiling   float64
	reason    string
}

func loadAllow(path string) ([]allowEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseAllow(f, path)
}

// parseAllow reads `<package or Pkg.TestName or Pkg.*> <seconds> <reason>`, one per line. Blank
// lines and lines starting with # are skipped. A line with no ceiling or no reason is an error,
// since a reason is the point of the file.
func parseAllow(r io.Reader, name string) ([]allowEntry, error) {
	var out []allowEntry
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return nil, fmt.Errorf("%s:%d: want `<package or Pkg.TestName> <seconds> <reason>`, got %q", name, n, line)
		}
		secs, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || secs <= 0 {
			return nil, fmt.Errorf("%s:%d: %q is not a ceiling in seconds", name, n, fields[1])
		}
		e := allowEntry{ceiling: secs, reason: strings.Join(fields[2:], " ")}
		e.pkg = fields[0]
		if i := strings.Index(fields[0], ".Test"); i >= 0 {
			e.pkg, e.test = fields[0][:i], fields[0][i+1:]
		} else if strings.HasSuffix(fields[0], ".*") {
			e.pkg, e.test = strings.TrimSuffix(fields[0], ".*"), "*"
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// samePkg: go test names a package by its import path and the allowlist by the
// end of it, so `internal/store` is github.com/.../internal/store and nothing else.
func samePkg(importPath, entry string) bool {
	return importPath == entry || strings.HasSuffix(importPath, "/"+entry)
}

// ceilingFor is the limit that applies, the allowlist's when it has a line.
// A subtest answers to its own line and then to its parent's.
func ceilingFor(allow []allowEntry, pkg, test string, def float64) (limit float64, listed bool) {
	names := []string{test}
	if i := strings.Index(test, "/"); i >= 0 {
		names = append(names, test[:i])
	}
	for _, name := range append(names, "*") {
		if test == "" && name != "" {
			continue
		}
		for _, e := range allow {
			if e.test == name && samePkg(pkg, e.pkg) {
				return e.ceiling, true
			}
		}
	}
	return def, false
}

// overLimit says, one line each, what ran past its limit. Every line names the
// package or test, its time, the limit, and what to do.
func overLimit(tests, pkgs []timing, allow []allowEntry, pkgLimit, testLimit float64) []string {
	var out []string
	const fix = "make it faster, or add a line to scripts/test-time-allow.txt with a ceiling and the reason"
	for _, p := range pkgs {
		if limit, listed := ceilingFor(allow, p.pkg, "", pkgLimit); p.secs > limit {
			out = append(out, fmt.Sprintf("SLOW PACKAGE %s took %.1fs, the limit is %.0fs%s: %s",
				p.pkg, p.secs, limit, listedNote(listed), fix))
		}
	}
	for _, t := range tests {
		if limit, listed := ceilingFor(allow, t.pkg, t.test, testLimit); t.secs > limit {
			out = append(out, fmt.Sprintf("SLOW TEST %s %s took %.2fs, the limit is %.0fs%s: %s",
				t.pkg, t.test, t.secs, limit, listedNote(listed), fix))
		}
	}
	sort.Strings(out)
	return out
}

func listedNote(listed bool) string {
	if listed {
		return " (its allowlist ceiling)"
	}
	return ""
}
