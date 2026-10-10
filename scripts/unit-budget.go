//go:build ignore

// Reads `go test -json` on stdin, prints it the way plain `go test` would (the output of a failing test, one line per
// package; everything with -v), and fails a unit run in which any test
// took more than 20ms. Run by scripts/test.sh in unit mode:
//
//	go test -json ./... | go run scripts/unit-budget.go
//
// A UNIT TEST IS UNDER 10ms. One that is not is doing something a unit test does not do (opening a store,
// touching a disk, waiting on a timer or a socket), so the run fails with its name and time and the fix is to move
// it into a `*_integration_test.go` file under `//go:build integration`, not to raise the number.
//
// The time is the gap between the test's run and its end events, which has the millisecond precision
// that go test's own two-decimal `Elapsed` lacks. A test that paused for t.Parallel is measured by Elapsed.
//
// Over 10ms is printed. Over 20ms fails the run. A test in `fine` is printed and never fails it: each is pure
// in-memory work that Windows scheduling pushes past 10ms now and then.
//
// -times writes every top-level test's time and result to a table, slowest first. -budget=false turns the 10ms
// and 20ms checks off, which an integration run needs.
//
// Exit status 1 when a test failed, a package failed, or (with the budget on) a test not in `fine` took over 20ms.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	budget = 10 * time.Millisecond
	limit  = 20 * time.Millisecond
)

// fine is package and test name of each unit test allowed over the limit.
var fine = map[string]bool{
	"internal/roomspec TestWindowsPlanWritesNothingAndApplyConverges": true,
	"internal/roomspec TestEditsKeepTheFilesOwnLineEndingAndBOM":      true,
	"internal/claudeconf TestDefaultLocationPathIsNotEmpty":           true,
}

type event struct {
	Time    time.Time
	Action  string
	Package string
	Test    string
	Elapsed float64
	Output  string
}

type over struct {
	pkg, test string
	took      time.Duration
}

var (
	verbose  = flag.Bool("v", false, "print every test's output, as go test -v does")
	enforce  = flag.Bool("budget", true, "print the tests over 10ms and fail the run on one over 20ms")
	timesTSV = flag.String("times", "", "write every top-level test's time and result here, slowest first")
)

// ran is every top-level test, written to -times.
var ran []result

type result struct {
	pkg, test, action string
	took              time.Duration
}

// held is each test's output so far, printed if the test fails.
var held = map[string]string{}

func main() {
	flag.Parse()
	started := map[string]time.Time{}
	paused := map[string]bool{}
	var slow []over
	failed := false

	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	for {
		line, err := in.ReadBytes('\n')
		if len(line) > 0 {
			var e event
			if json.Unmarshal(line, &e) != nil {
				os.Stdout.Write(line)
			} else {
				failed = handle(e, started, paused, &slow) || failed
			}
		}
		if err != nil {
			break
		}
	}

	if *timesTSV != "" {
		if err := writeTimes(*timesTSV); err != nil {
			fmt.Fprintln(os.Stderr, "unit-budget:", err)
		}
	}
	if *enforce && len(slow) > 0 {
		sort.Slice(slow, func(i, j int) bool { return slow[i].took > slow[j].took })
		fmt.Printf("\nOVER THE 10ms UNIT BUDGET (%d). A unit test is under 10ms and touches nothing outside the process.\n", len(slow))
		fmt.Println("Over 20ms fails the run: move it into a *_integration_test.go file under //go:build integration.")
		for _, s := range slow {
			mark := "ok"
			switch {
			case fine[s.pkg+" "+s.test]:
				mark = "fine"
			case s.took > limit:
				mark = "FAIL"
				failed = true
			}
			fmt.Printf("  %-4s %6.1fms  %s  %s\n", mark, float64(s.took.Microseconds())/1000, s.pkg, s.test)
		}
	}
	if failed {
		os.Exit(1)
	}
}

// handle prints what go test would and records the time of a top-level test. It reports whether something failed.
func handle(e event, started map[string]time.Time, paused map[string]bool, slow *[]over) bool {
	key := e.Package + " " + e.Test
	switch e.Action {
	case "output":
		switch {
		case *verbose || e.Test == "" && !strings.HasPrefix(e.Output, "PASS") && !strings.HasPrefix(e.Output, "FAIL\n") && e.Output != "FAIL\n":
			fmt.Print(e.Output)
		case e.Test != "":
			held[key] += e.Output
		}
	case "run":
		if !strings.Contains(e.Test, "/") {
			started[key] = e.Time
		}
	case "pause", "cont":
		paused[key] = true
	case "pass", "fail", "skip":
		if e.Test == "" || strings.Contains(e.Test, "/") {
			return e.Action == "fail" && e.Test == ""
		}
		if e.Action == "fail" {
			fmt.Print(held[key])
		}
		delete(held, key)
		took := e.Time.Sub(started[key])
		if paused[key] || started[key].IsZero() {
			took = time.Duration(e.Elapsed * float64(time.Second))
		}
		pkg := strings.TrimPrefix(e.Package, "github.com/dovholuknf/atrium/")
		ran = append(ran, result{pkg, e.Test, e.Action, took})
		if e.Action != "skip" && took > budget {
			*slow = append(*slow, over{pkg, e.Test, took})
		}
		return e.Action == "fail"
	}
	return false
}

// writeTimes writes ran as a table: milliseconds, result, package, test, slowest first.
func writeTimes(path string) error {
	sort.Slice(ran, func(i, j int) bool { return ran[i].took > ran[j].took })
	var b strings.Builder
	b.WriteString("ms\tresult\tpackage\ttest\n")
	for _, r := range ran {
		fmt.Fprintf(&b, "%.1f\t%s\t%s\t%s\n", float64(r.took.Microseconds())/1000, r.action, r.pkg, r.test)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
