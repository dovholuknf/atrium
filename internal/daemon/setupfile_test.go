package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSetup(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".atrium"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".atrium", "setup.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func stateOf(results []needResult, name string) string {
	for _, r := range results {
		if r.Name == name {
			return r.State
		}
	}
	return ""
}

func TestSetupRequiredNeedRefusesAndOthersWarn(t *testing.T) {
	empty := t.TempDir()
	repo := writeSetup(t, `
[must]
required = true
why = "builds"
install.linux = "apt install must"
install.macos = "brew install must"
install.windows = "winget install must"
[nice]
`)
	if _, err := setupFor(repo, empty); err == nil || !strings.Contains(err.Error(), "- must: missing") ||
		strings.Contains(err.Error(), "nice") {
		t.Fatalf("err = %v", err)
	}
	soft := writeSetup(t, "[nice]\n")
	text, err := setupFor(soft, empty)
	if err != nil || !strings.HasPrefix(text, setupLine) || !strings.Contains(text, "- nice: missing") {
		t.Fatalf("text = %q, err = %v", text, err)
	}
}

func TestSetupAbsentFileAndAllOKAddNothingOrOnlyTheFraming(t *testing.T) {
	if text, err := setupFor(t.TempDir(), ""); text != "" || err != nil {
		t.Fatalf("text = %q, err = %v", text, err)
	}
	bin := t.TempDir()
	fakeTool(t, bin, "have")
	repo := writeSetup(t, "[have]\n")
	if text, err := setupFor(repo, bin); text != setupLine || err != nil {
		t.Fatalf("text = %q, err = %v", text, err)
	}
}

func TestSetupBadFileWarnsAndIgnoresUnknownFields(t *testing.T) {
	text, err := setupFor(writeSetup(t, "[x\n"), "")
	if err != nil || !strings.Contains(text, "Setup: ") {
		t.Fatalf("text = %q, err = %v", text, err)
	}
	needs, err := parseSetup([]byte("[a]\nfuture = 1\n"))
	if err != nil || len(needs) != 1 {
		t.Fatalf("needs = %v, err = %v", needs, err)
	}
	if _, err := parseSetup([]byte(strings.Repeat("#", setupMaxBytes+1))); err == nil {
		t.Fatal("an oversize file parsed")
	}
}
