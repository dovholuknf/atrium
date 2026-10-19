package daemon

import (
	"os"
	"strings"
	"testing"
)

func TestAShortLauncherSayIsTypedPlainOnOneLine(t *testing.T) {
	got := launcherSayBody("", "t1", "change how you land")
	if got != "change how you land" {
		t.Fatalf("a short say was altered: %q", got)
	}
	if strings.Contains(got, "\x1b[200~") {
		t.Fatal("a launcher's say is wrapped as a paste")
	}
}

func TestALongOrMultiLineLauncherSayGoesInAFile(t *testing.T) {
	for _, text := range []string{"line one\nline two", strings.Repeat("x ", 200)} {
		got := launcherSayBody("", "t-say-file", text)
		if strings.ContainsAny(got, "\r\n\x1b") || len(got) > launcherSayInline {
			t.Fatalf("the typed line is not one short line: %q", got)
		}
		path := strings.TrimSuffix(strings.TrimPrefix(got, "read "), " and do what it says.")
		b, err := os.ReadFile(path)
		if err != nil || string(b) != text {
			t.Fatalf("the file %q does not hold the say: %v", path, err)
		}
	}
}

func TestFlattenSayJoinsLinesAndDropsEscapes(t *testing.T) {
	if got := flattenSay("a\r\nb\x1b[201~ c"); strings.ContainsAny(got, "\r\n\x1b") || got != "a b[201~ c" {
		t.Fatalf("got %q", got)
	}
}

func TestTheLauncherBannerNamesTheLauncher(t *testing.T) {
	if b := launcherBanner("orch"); !strings.Contains(b, "your launcher @orch") || strings.ContainsAny(b, "\r\n") {
		t.Fatalf("banner %q", b)
	}
}
