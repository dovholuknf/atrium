package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequirementsCommandExitStatus(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(good, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("version: 1\nnope: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"requirements", good, "--json"}); code != 0 {
		t.Errorf("a good file exited %d", code)
	}
	if code := run([]string{"requirements", bad, "--json"}); code != 1 {
		t.Errorf("a bad file exited %d, want 1", code)
	}
	if code := run([]string{"requirements", filepath.Join(dir, "missing.yaml")}); code != 1 {
		t.Errorf("a missing file exited %d, want 1", code)
	}
}
