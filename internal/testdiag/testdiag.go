// Package testdiag makes a failed test explain itself from the CI log alone.
//
// A test that fails on a runner and nowhere else is only fixable from what the
// run left behind, so a test helper that sees a failure calls Dump: every
// goroutine's stack goes to a file the CI job uploads, and the goroutines that
// are inside the store or database/sql (the ones that hold atrium.db open)
// go into the test's own log, where the failure is read.
//
// CI_DIAG_DIR names the folder for the files. scripts/ci.sh sets it to
// build.claude/ci/goroutines, which ci.yml uploads. Not an ATRIUM_* name:
// testguard.Scrub removes every one of those before a test runs. Unset, the
// files go to the temp dir and the log says where.
package testdiag

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// DirEnv names the folder Dump writes to.
const DirEnv = "CI_DIAG_DIR"

// holders are the stack frames of a goroutine that may hold the database open.
var holders = []string{"/internal/store.", "database/sql.", "modernc.org/sqlite"}

var seq atomic.Int64

// Dump records every goroutine of this process now, because of why: the whole
// of them to a file, and those in the store or database/sql to t's log.
func Dump(t testing.TB, why string) {
	t.Helper()
	all := stacks()
	gs := bytes.Split(all, []byte("\n\n"))
	var held []string
	for _, g := range gs {
		for _, h := range holders {
			if bytes.Contains(g, []byte(h)) {
				held = append(held, string(g))
				break
			}
		}
	}
	path, err := write(t.Name(), why, all, len(gs))
	where := path
	if err != nil {
		where = "nowhere (" + err.Error() + ")"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "testdiag: %s. %d goroutines at %s, GOMAXPROCS %d, all of them in %s.",
		why, len(gs), time.Now().Format("15:04:05.000"), runtime.GOMAXPROCS(0), where)
	if len(held) == 0 {
		b.WriteString(" none is in the store or database/sql.")
	} else {
		fmt.Fprintf(&b, " %d in the store or database/sql:", len(held))
		for i, g := range held {
			if i == 20 {
				fmt.Fprintf(&b, "\n\n... and %d more, in the file", len(held)-i)
				break
			}
			b.WriteString("\n\n")
			b.WriteString(g)
		}
	}
	if fds := openFiles(".db"); fds != "" {
		b.WriteString("\n\nopen files of this process ending in .db*:\n")
		b.WriteString(fds)
	}
	t.Log(b.String())
}

// OnFailure dumps the goroutines when t ends failed. Called before t's first
// TempDir, it runs after the temp dir's own cleanup (cleanups run last in,
// first out), so a temp file that could not be removed counts here too.
func OnFailure(t testing.TB) {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			Dump(t, "the test failed")
		}
	})
}

func stacks() []byte {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		if len(buf) >= 256<<20 {
			return buf
		}
		buf = make([]byte, 2*len(buf))
	}
}

func write(name, why string, all []byte, n int) (string, error) {
	dir := os.Getenv(DirEnv)
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "atrium-testdiag")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' || r == '"' || r == '*' || r == '?' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
	if len(safe) > 100 {
		safe = safe[:100]
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d-%d.txt", safe, os.Getpid(), seq.Add(1)))
	head := fmt.Sprintf("test %s\nwhy %s\nat %s\nGOOS %s GOARCH %s GOMAXPROCS %d NumCPU %d\ngoroutines %d\n\n",
		name, why, time.Now().Format(time.RFC3339Nano), runtime.GOOS, runtime.GOARCH,
		runtime.GOMAXPROCS(0), runtime.NumCPU(), n)
	return path, os.WriteFile(path, append([]byte(head), all...), 0o644)
}

// openFiles lists this process's open files whose name contains sub, where the
// OS says (Linux). Elsewhere it is empty and the goroutines are the evidence.
func openFiles(sub string) string {
	ents, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, e := range ents {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name()))
		if err == nil && strings.Contains(target, sub) {
			fmt.Fprintf(&b, "  fd %s -> %s\n", e.Name(), target)
		}
	}
	return b.String()
}
