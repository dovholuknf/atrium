package testdiag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// logged keeps what Dump says, so the test can read it.
type logged struct {
	testing.TB
	lines []string
}

func (l *logged) Log(args ...any) { l.lines = append(l.lines, fmt.Sprint(args...)) }
func (l *logged) Helper()         {}

func TestDumpWritesEveryGoroutineAndSaysWhere(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(DirEnv, dir)
	l := &logged{TB: t}
	Dump(l, "a reason")

	if len(l.lines) != 1 || !strings.Contains(l.lines[0], "a reason") || !strings.Contains(l.lines[0], dir) {
		t.Fatalf("the log does not give the reason and the file: %q", l.lines)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	if len(files) != 1 {
		t.Fatalf("want one file in %s, got %v", dir, files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); !strings.Contains(s, "why a reason") || !strings.Contains(s, "TestDumpWritesEveryGoroutineAndSaysWhere") {
		t.Fatalf("the file has no header or no stacks:\n%.400s", s)
	}
}
