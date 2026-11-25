//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestGeminiTrustsACardFolderOnARoomWithNoProviders(t *testing.T) {
	d, root := homeRoom(t)
	gemini := t.TempDir()
	cwd := filepath.Join(root, "card-1", "work")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	d.prepareRunnerSetup(&store.Harness{ID: "t-gemini", Cmd: "gemini"}, cwd, []string{"GEMINI_CLI_HOME=" + gemini})
	raw, err := os.ReadFile(filepath.Join(gemini, ".gemini", "trustedFolders.json"))
	if err != nil {
		t.Fatal(err)
	}
	long, err := filepath.EvalSymlinks(cwd) // Windows temp dirs come as 8.3 short names; the trust file holds the long one
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(raw)), strings.ToLower(filepath.ToSlash(long))) {
		t.Fatalf("trustedFolders.json does not trust %s:\n%s", cwd, raw)
	}
}
