//go:build integration

package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// declared matches a top-level declaration: one at column zero, since anything
// indented is inside something and is not in the global namespace.
var declared = regexp.MustCompile(`(?m)^(?:async\s+)?(?:function|const|let|var|class)\s+([A-Za-z_$][\w$]*)`)

func TestNoTwoBoardScriptsDeclareTheSameName(t *testing.T) {
	dir := filepath.Join("web", "js")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Where each name was first seen, so a clash can say both files.
	where := map[string]string{}
	var clashes []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		// Every name this file declares, deduplicated: a `let` reassigned in a
		// branch below is one declaration as far as the namespace is concerned,
		// and a file clashing with itself is the compiler's problem, not this
		// test's.
		mine := map[string]bool{}
		for _, m := range declared.FindAllStringSubmatch(string(raw), -1) {
			mine[m[1]] = true
		}
		for name := range mine {
			if first, seen := where[name]; seen {
				clashes = append(clashes, name+" is declared in both "+first+" and "+e.Name())
				continue
			}
			where[name] = e.Name()
		}
	}
	if len(clashes) > 0 {
		t.Fatalf("the board has one global scope, so the file loaded last wins "+
			"and the other file's callers get the wrong function:\n  %s",
			strings.Join(clashes, "\n  "))
	}
}
