package claudeconf

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// TrustCodexProjects appends a trusted entry per unlisted folder, since config.toml is all codex's trust prompt reads.
func TrustCodexProjects(codexHome string, dirs []string) error {
	path, err := codexConfigPath(codexHome)
	if err != nil {
		return err
	}
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

// codexConfigPath is config.toml in the row's CODEX_HOME when it names one, else where the daemon's codex keeps it.
func codexConfigPath(codexHome string) (string, error) {
	if codexHome != "" {
		return filepath.Join(codexHome, "config.toml"), nil
	}
	hooks, err := CodexHooksPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(hooks), "config.toml"), nil
}

// AnswerCodexSandbox writes the non-admin Windows sandbox, since codex asks for an elevation no headless room can show.
func AnswerCodexSandbox(codexHome string) error {
	path, err := codexConfigPath(codexHome)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	at := -1
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "windows.sandbox ") || strings.HasPrefix(line, "windows.sandbox=") {
			return nil
		}
		if line == "[windows]" {
			at = i
			continue
		}
		if at >= 0 && strings.HasPrefix(line, "[") {
			break
		}
		if at >= 0 && (strings.HasPrefix(line, "sandbox ") || strings.HasPrefix(line, "sandbox=")) {
			return nil
		}
	}
	text := string(raw)
	if at >= 0 {
		text = strings.Join(slices.Insert(lines, at+1, `sandbox = "unelevated"`), "\n")
	} else {
		text += "\n[windows]\nsandbox = \"unelevated\"\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}
