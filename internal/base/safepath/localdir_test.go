package safepath

import (
	"path/filepath"
	"testing"
)

func TestLocalDirRefusesWhatIsNotAPlainAbsoluteFolder(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "repo")
	bad := []string{"", "  ", "relative/dir", ".", "~/x", abs + "/../other", abs + "\x00x", `\server\share\x`, "//server/share/x"}
	for _, p := range bad {
		if got, err := LocalDir(p); err == nil {
			t.Errorf("LocalDir(%q) = %q, want a refusal", p, got)
		}
	}
	got, err := LocalDir(abs + string(filepath.Separator) + "." + string(filepath.Separator) + "")
	if err != nil || got != abs {
		t.Errorf("LocalDir of a clean absolute path = %q, %v, want %q", got, err, abs)
	}
}
