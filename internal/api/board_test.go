package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The board is one HTML file with one script block, and until this existed
// nothing parsed that script except a browser. A syntax error therefore
// shipped as a blank dashboard with one line in a console nobody had open,
// and the error named whatever token followed the mistake rather than the
// mistake.
//
// It is checked here rather than only in CI because the failure is invisible
// locally: the daemon serves the file happily, the Go tests pass, and the page
// is dead.

// A comment written inside a template literal is not a comment to the
// JavaScript parser. A backtick in one ends the string.
//
// This has no dependencies and always runs, which is the point: it catches the
// exact mistake that has now been made three times in one file.
func TestNoBacktickInMarkupComments(t *testing.T) {
	src := readBoard(t)
	script := scriptBlock(t, src)

	for i, line := range strings.Split(script, "\n") {
		start := strings.Index(line, "<!--")
		if start < 0 {
			continue
		}
		rest := line[start:]
		if end := strings.Index(rest, "-->"); end >= 0 {
			rest = rest[:end]
		}
		if strings.Contains(rest, "`") {
			t.Errorf("index.html:%d has a backtick inside a markup comment in the script.\n"+
				"  %s\n"+
				"that comment is inside a template literal, so the backtick ends the string "+
				"and the whole page stops parsing. move the prose to a // comment outside "+
				"the literal, or use quotes.",
				scriptStart(t, src)+i, strings.TrimSpace(line))
		}
	}
}

// The real check: hand the script to a parser.
//
// Skipped when node is absent rather than failed. This is a lint on a file Go
// does not build, and a machine without node can still work on the daemon. CI
// has node, so nothing reaches a tag unparsed.
func TestBoardScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH, so the board's script cannot be parsed here")
	}
	src := readBoard(t)

	js := filepath.Join(t.TempDir(), "board.js")
	if err := os.WriteFile(js, []byte(scriptBlock(t, src)), 0o600); err != nil {
		t.Fatal(err)
	}
	// --check parses without running. This code expects a browser and would
	// not survive being executed.
	out, err := exec.Command(node, "--check", js).CombinedOutput()
	if err != nil {
		t.Fatalf("the board's script does not parse:\n%s\n"+
			"line numbers are relative to the script block, which starts at "+
			"index.html:%d", out, scriptStart(t, src))
	}
}

// The service worker is a separate file and a separate way to break the board.
func TestServiceWorkerParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH")
	}
	out, err := exec.Command(node, "--check", filepath.Join("web", "sw.js")).CombinedOutput()
	if err != nil {
		t.Fatalf("sw.js does not parse:\n%s", out)
	}
}

func readBoard(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// scriptBlock returns the one script block. Asserting there is exactly one
// rather than taking the first, because a second would mean these tests
// silently covered only part of the page.
func scriptBlock(t *testing.T, src string) string {
	t.Helper()
	lines := strings.Split(src, "\n")
	var out []string
	opens, on := 0, false
	for _, line := range lines {
		switch {
		case strings.TrimSpace(line) == "<script>":
			opens++
			on = true
		case strings.TrimSpace(line) == "</script>":
			on = false
		case on:
			out = append(out, line)
		}
	}
	if opens != 1 {
		t.Fatalf("expected one script block in index.html, found %d. teach these "+
			"tests about the others before adding them", opens)
	}
	if len(out) == 0 {
		t.Fatal("extracted no javascript from index.html, so the test is broken")
	}
	return strings.Join(out, "\n")
}

func scriptStart(t *testing.T, src string) int {
	t.Helper()
	for i, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == "<script>" {
			return i + 2 // one for the tag, one for 1-based lines
		}
	}
	return 0
}
