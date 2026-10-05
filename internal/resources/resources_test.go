package resources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesStarterAndNeverOverwrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	p, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != Starter || !strings.Contains(Starter, "Never a token, password or key") {
		t.Fatalf("starter not written: %q", b)
	}
	if err := os.WriteFile(p, []byte("## mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(dir); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second init err = %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "## mine\n" {
		t.Fatalf("file was overwritten: %q", b)
	}
}

func TestReadMissingIsNotAnError(t *testing.T) {
	text, exists, cut, err := Read(t.TempDir())
	if err != nil || exists || cut || text != "" {
		t.Fatalf("got %q %v %v %v", text, exists, cut, err)
	}
}

func TestReadCutsAHugeFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(strings.Repeat("x", MaxBytes+10)), 0o644); err != nil {
		t.Fatal(err)
	}
	text, exists, cut, err := Read(dir)
	if err != nil || !exists || !cut || len(text) != MaxBytes {
		t.Fatalf("got len %d %v %v %v", len(text), exists, cut, err)
	}
}
