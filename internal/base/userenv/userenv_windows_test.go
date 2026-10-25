package userenv

import (
	"os"
	"strings"
	"testing"
)

func TestCurrentExpandsTheRegistryPath(t *testing.T) {
	p := current()["PATH"]
	if p == "" || strings.Contains(p, "%SystemRoot%") || !strings.Contains(strings.ToLower(p), strings.ToLower(os.Getenv("SystemRoot"))) {
		t.Fatalf("PATH from the registry: %q", p)
	}
}
