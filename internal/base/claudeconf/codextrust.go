package claudeconf

import (
	"os"
	"path/filepath"
	"strings"
)

// TrustCodexProjects appends a trusted entry per unlisted folder, since config.toml is all codex's trust prompt reads.
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
