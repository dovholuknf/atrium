//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestBriefFileCarriesTheResourcesLineOnceEvenWithoutGitURL(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	got := withResourcesLine("task")
	if got != "task\n\n"+resourcesLine || withResourcesLine(got) != got {
		t.Fatalf("withResourcesLine = %q", got)
	}
	if strings.Contains(resourcesLine, "\n") {
		t.Fatal("the framing is more than one line")
	}
}

func TestASessionStartIsToldTheRoomLinesAndAHubRoomIsNotToldGh(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "git")
	fakeTool(t, dir, "gh")
	t.Setenv("PATH", dir)
	d := testDaemon(t)
	got := d.roomLines(&store.Task{})
	for _, want := range []string{gitURLLine, resourcesLine, "git=", "gh="} {
		if !strings.Contains(got, want) {
			t.Fatalf("roomLines lacks %q: %q", want, got)
		}
	}
	if strings.Contains(d.roomLines(&store.Task{Tags: []string{OutsideCodeTag}}), "atrium_git_url") {
		t.Fatal("an outside-code card was told atrium_git_url")
	}
	d.SetHubForge(&forge.Remote{})
	if got := d.roomLines(&store.Task{}); strings.Contains(got, "gh=") || !strings.Contains(got, "git=") {
		t.Fatalf("a hub room's tools line: %q", got)
	}
}

func TestAClaudeBriefCarriesNoRoomLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := writeBrief(dir, "do the thing"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, briefFileName))
	if string(raw) != "do the thing\n" {
		t.Fatalf("brief %q", raw)
	}
}
