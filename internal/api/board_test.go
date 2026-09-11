package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The board is an HTML file and a set of plain scripts it loads in order, and
// until this existed nothing parsed those scripts except a browser. A syntax
// error therefore shipped as a blank dashboard with one line in a console
// nobody had open, and the error named whatever token followed the mistake
// rather than the mistake.
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
	for _, f := range scriptFiles(t) {
		for i, line := range strings.Split(f.body, "\n") {
			start := strings.Index(line, "<!--")
			if start < 0 {
				continue
			}
			rest := line[start:]
			if end := strings.Index(rest, "-->"); end >= 0 {
				rest = rest[:end]
			}
			if strings.Contains(rest, "`") {
				t.Errorf("js/%s:%d has a backtick inside a markup comment.\n"+
					"  %s\n"+
					"that comment is inside a template literal, so the backtick ends the string "+
					"and the whole page stops parsing. move the prose to a // comment outside "+
					"the literal, or use quotes.",
					f.name, i+1, strings.TrimSpace(line))
			}
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
	files := scriptFiles(t)

	// EACH FILE ON ITS OWN FIRST, so the failure names one of two dozen files
	// rather than an offset into their concatenation.
	for _, f := range files {
		out, err := exec.Command(node, "--check",
			filepath.Join("web", "js", f.name)).CombinedOutput()
		if err != nil {
			t.Errorf("js/%s does not parse:\n%s", f.name, out)
		}
	}

	// AND THE CONCATENATION, which is what the browser ends up with. These are
	// classic scripts sharing one global scope, so a block left open at the end
	// of one file is closed by the next and neither file is wrong on its own.
	var all strings.Builder
	for _, f := range files {
		all.WriteString(f.body)
	}
	js := filepath.Join(t.TempDir(), "board.js")
	if err := os.WriteFile(js, []byte(all.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	// --check parses without running. This code expects a browser and would
	// not survive being executed.
	out, err := exec.Command(node, "--check", js).CombinedOutput()
	if err != nil {
		t.Fatalf("the board's script does not parse once the files are joined:\n%s\n"+
			"line numbers are relative to the concatenation, in load order", out)
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

// scriptFile is one of the board's scripts, with the name the page loads it by.
type scriptFile struct {
	name string
	body string
}

// scriptFiles returns the board's scripts IN THE ORDER THE PAGE LOADS THEM.
//
// Read off `index.html` rather than off the directory, because the order is the
// part that matters. These are classic scripts sharing one global scope, so a
// file loaded before the one declaring what it calls at load time throws and
// takes the rest of itself with it, and an alphabetical listing would be a
// different order that happens to parse.
//
// A file on disk and not in the page is also a failure here. It never runs, and
// nothing else would ever say so.
func scriptFiles(t *testing.T) []scriptFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("web", "index.html"))
	if err != nil {
		t.Fatal(err)
	}

	var out []scriptFile
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		const open = `<script src="/js/`
		if !strings.HasPrefix(line, open) {
			continue
		}
		name, _, ok := strings.Cut(strings.TrimPrefix(line, open), `"`)
		if !ok {
			t.Fatalf("could not read a script name out of %q", line)
		}
		if seen[name] {
			t.Fatalf("index.html loads js/%s twice, so every listener in it is "+
				"bound twice and every keystroke is sent twice", name)
		}
		seen[name] = true
		body, err := os.ReadFile(filepath.Join("web", "js", name))
		if err != nil {
			t.Fatalf("index.html loads js/%s and it is not there: %v", name, err)
		}
		out = append(out, scriptFile{name: name, body: string(body)})
	}
	if len(out) == 0 {
		t.Fatal("index.html loads no scripts under /js/, so this test is proving nothing")
	}

	ondisk, err := os.ReadDir(filepath.Join("web", "js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ondisk {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") && !seen[e.Name()] {
			t.Errorf("web/js/%s is never loaded by index.html, so it is dead code "+
				"that reads as live", e.Name())
		}
	}
	return out
}
