//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBriefFileCarriesTheResourcesLineOnceEvenWithoutGitURL(t *testing.T) {
	for _, gitURL := range []bool{true, false} {
		dir := t.TempDir()
		if _, err := writeBriefFile(dir, "do the thing\n\n"+resourcesLine, gitURL); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, briefFileName))
		if n := strings.Count(string(raw), "atrium_resources"); n != 1 {
			t.Fatalf("gitURL=%v: the line appears %d times: %q", gitURL, n, raw)
		}
	}
	dir := t.TempDir()
	if _, err := writeBriefFile(dir, "do the thing", false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, briefFileName))
	if !strings.HasPrefix(string(raw), "do the thing") || !strings.Contains(string(raw), resourcesLine) {
		t.Fatalf("brief body wrong: %q", raw)
	}
}

func TestResourcesLineCarriesNoSecretAndIsOneLine(t *testing.T) {
	got := withResourcesLine("task")
	if got != "task\n\n"+resourcesLine || withResourcesLine(got) != got {
		t.Fatalf("withResourcesLine = %q", got)
	}
	if strings.Contains(resourcesLine, "\n") {
		t.Fatal("the framing is more than one line")
	}
}
