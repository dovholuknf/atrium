package claudeconf

import (
	"os"
	"path/filepath"
	"strings"
)

// TrustCodexProjects marks each folder trusted in codex's config.toml, the only place its trust prompt reads, and
// leaves a folder already listed as its owner set it. Appended, so a write codex makes meanwhile is not lost.
func TrustCodexProjects(dirs []string) error {
	hooks, err := CodexHooksPath()
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(hooks), "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	text := string(raw)
	var add strings.Builder
	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		if dir == "." || strings.ContainsAny(dir, "'\n") {
			continue
		}
		if strings.Contains(text, "[projects.'"+dir+"']") || strings.Contains(text, `[projects."`+dir+`"]`) ||
			strings.Contains(text, `[projects."`+strings.ReplaceAll(dir, `\`, `\\`)+`"]`) {
			continue
		}
		add.WriteString("\n[projects.'" + dir + "']\ntrust_level = \"trusted\"\n")
	}
	if add.Len() == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(add.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
