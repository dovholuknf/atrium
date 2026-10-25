package runnersetup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledVersionFollowsALauncherSymlinkIntoAnotherPrefix(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node", "lib", "node_modules", "@openai", "codex")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"version":"0.163.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(pkg, "bin", "codex.js")
	if err := os.WriteFile(target, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "codex")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if got := InstalledVersion(link, "@openai/codex"); got != "0.163.0" {
		t.Errorf("InstalledVersion = %q, want 0.163.0", got)
	}
}
