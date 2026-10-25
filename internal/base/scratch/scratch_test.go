package scratch

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRefusesWhatIsNotOneAbsoluteFolder(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "tmp")
	if got, err := Check(abs); err != nil || got != abs {
		t.Fatalf("Check(%q) = %q, %v", abs, got, err)
	}
	for _, bad := range []string{"tmp", "relative/path", "a\nb", filepath.VolumeName(abs) + string(filepath.Separator), "/"} {
		if _, err := Check(bad); err == nil {
			t.Errorf("Check(%q) accepted it", bad)
		}
	}
}

func TestDirNamesTheCardUnderTheRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tmp")
	got := Dir(root, "Fix the OIDC hot loop!", "wire", "sg4~01ABCDEFGHIJ")
	want := filepath.ToSlash(root) + "/fix-the-oidc-hot-loop-01ABCDEF"
	if got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
	if Dir("", "t", "w", "id") != "" {
		t.Fatal("an unset root gave a folder")
	}
	if !strings.Contains(Short("", "box/alpha", ""), "box-alpha") {
		t.Fatal("the wire name is the fallback")
	}
}
