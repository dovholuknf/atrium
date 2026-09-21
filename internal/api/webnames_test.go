package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ONE GLOBAL SCOPE, AND A LATER FILE SILENTLY WINS.
//
// The board is a dozen script tags with no module system and no build step, on
// purpose: it has to work offline, over an overlay, and be editable with a text
// editor. The price is that every top-level name in `web/js` lands in one
// namespace, in load order.
//
// Two files declaring `loadRooms` is not an error anywhere. The browser does not
// warn, the page does not break, and nothing in the build says a word. What
// happens is that the one loaded LAST wins and the other file's callers quietly
// start calling a function that answers a different question. That is how the
// room chip in the header came to be invisible: `js/rooms.js` asked the hub
// what was attached, `js/runners.js` redefined `loadRooms` eighty lines later to
// ask the daemon about federated machines, and the chip spent its life reading
// the wrong answer and hiding itself.
//
// So the names are checked. A collision here is either a rename or a deliberate
// override, and both are worth having to say out loud.

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
