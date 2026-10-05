package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/resources"
)

func TestResourcesInitWritesOnceAndNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKTREE_ROOT", root)
	file := resources.Path(filepath.Join(root, "hub"))

	run := func() (string, error) {
		c := newResources()
		var out bytes.Buffer
		c.SetOut(&out)
		c.SetArgs([]string{"init"})
		err := c.Execute()
		return out.String(), err
	}
	out, err := run()
	if err != nil || !strings.Contains(out, file) {
		t.Fatalf("first init: %q %v", out, err)
	}
	if err := os.WriteFile(file, []byte("## mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(); err == nil {
		t.Fatal("second init did not refuse")
	}
	if b, _ := os.ReadFile(file); string(b) != "## mine\n" {
		t.Fatalf("overwritten: %q", b)
	}
}
