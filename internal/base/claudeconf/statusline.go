package claudeconf

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StatusLineIssues is what is wrong with the status line under home, empty when it is fine. exe is this room's atrium.
// A status line that is not `atrium statusline` is accepted as it is: whatever it is, only whether it posts usage
// matters, and the caller checks that. One that runs an atrium that is not there is reported.
func StatusLineIssues(home, exe string) []string {
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return []string{"no status line: claude settings.json has none"}
	}
	var doc struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	f := commandWords(doc.StatusLine.Command)
	if len(f) == 0 {
		return []string{"no status line: claude settings.json has none"}
	}
	prog := strings.ToLower(filepath.Base(filepath.ToSlash(f[0])))
	if len(f) < 2 || f[1] != "statusline" || strings.TrimSuffix(prog, ".exe") != "atrium" {
		return nil
	}
	if _, err := os.Stat(f[0]); err != nil && !strings.EqualFold(filepath.ToSlash(f[0]), filepath.ToSlash(exe)) {
		return []string{"status line runs " + f[0] + ", which is not there"}
	}
	return nil
}

// commandWords splits a command line on spaces, keeping a double-quoted word whole and taking its quotes off.
func commandWords(cmd string) []string {
	var out []string
	var cur strings.Builder
	quoted, any := false, false
	for _, r := range cmd {
		switch {
		case r == '"':
			quoted, any = !quoted, true
		case r == ' ' && !quoted:
			if any {
				out = append(out, cur.String())
				cur.Reset()
				any = false
			}
		default:
			cur.WriteRune(r)
			any = true
		}
	}
	if any {
		out = append(out, cur.String())
	}
	return out
}

// InstallStatusLine sets statusLine in the settings.json under home to `<exe> statusline` and touches no other key. An
// existing file is copied to settings.json.statusline-<stamp>.bak first, and one that is not JSON is left alone. It
// returns the backup's path, empty when the file was new.
func InstallStatusLine(home, exe string) (string, error) {
	path := filepath.Join(home, ".claude", "settings.json")
	doc := map[string]json.RawMessage{}
	backup := ""
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if json.Unmarshal(raw, &doc) != nil {
			return "", errors.New(path + " is not JSON, so it was left as it is")
		}
		backup = path + ".statusline-" + time.Now().Format("20060102-150405") + ".bak"
		if err := os.WriteFile(backup, raw, 0o600); err != nil {
			return "", err
		}
	case !errors.Is(err, os.ErrNotExist):
		return "", err
	}
	cmd := filepath.ToSlash(exe) + " statusline"
	if strings.Contains(exe, " ") {
		cmd = `"` + filepath.ToSlash(exe) + `" statusline`
	}
	doc["statusLine"], _ = json.Marshal(map[string]string{"type": "command", "command": cmd})
	out, err := marshalInOrder(raw, doc)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return backup, os.WriteFile(path, out, 0o644)
}
