package claudeconf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UninstallResult is what an uninstall took out, one line per removed command.
type UninstallResult struct {
	Path    string   `json:"path"`
	Removed []string `json:"removed,omitempty"`
	Backup  string   `json:"backup,omitempty"`
}

// HasAtriumHooks reports whether the runner's file already registers any command an atrium binary runs. A file that
// cannot be read or parsed answers true, so a caller that wires hooks on its own leaves it alone.
func HasAtriumHooks(t Target) bool {
	path, err := t.Path()
	if err != nil {
		return true
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		return true
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return true
	}
	for _, cmds := range registeredCommands(doc) {
		for _, c := range cmds {
			if atriumCommand(c) {
				return true
			}
		}
	}
	return false
}

// UninstallTarget takes every command an atrium binary runs out of the runner's hooks, and claude's statusLine when
// it is `atrium statusline`. Every other entry keeps its own bytes, re-indented. Nothing to take out writes nothing.
func UninstallTarget(t Target) (UninstallResult, error) {
	path, err := t.Path()
	if err != nil {
		return UninstallResult{}, err
	}
	res := UninstallResult{Path: filepath.ToSlash(path)}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return res, fmt.Errorf("%s is not valid json, so nothing was changed: %w", path, err)
	}

	if h, ok := doc["hooks"]; ok {
		var section map[string]json.RawMessage
		if err := json.Unmarshal(h, &section); err != nil {
			return res, fmt.Errorf("the hooks section is not in the shape claude code uses: %w", err)
		}
		changed := false
		for hook, rawList := range section {
			kept, removed, err := withoutAtrium(rawList)
			if err != nil || len(removed) == 0 {
				continue
			}
			changed = true
			for _, c := range removed {
				res.Removed = append(res.Removed, hook+": "+c)
			}
			if kept == nil {
				delete(section, hook)
			} else {
				section[hook] = kept
			}
		}
		if changed {
			if len(section) == 0 {
				delete(doc, "hooks")
			} else if doc["hooks"], err = json.Marshal(section); err != nil {
				return res, err
			}
		}
	}
	if t.ID == Claude.ID {
		if sl, ok := doc["statusLine"]; ok {
			var s struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(sl, &s) == nil && atriumWords(s.Command, "statusline") {
				delete(doc, "statusLine")
				res.Removed = append(res.Removed, "statusLine: "+s.Command)
			}
		}
	}
	if len(res.Removed) == 0 {
		return res, nil
	}

	out, err := marshalInOrder(raw, doc)
	if err != nil {
		return res, err
	}
	out = append(out, '\n')
	backup := path + ".atrium-" + time.Now().Format("20060102-150405") + ".bak"
	if err := os.WriteFile(backup, raw, 0o600); err != nil {
		return UninstallResult{Path: res.Path}, fmt.Errorf("could not keep a copy of the old settings, so nothing was changed: %w", err)
	}
	res.Backup = filepath.ToSlash(backup)
	target := path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		target = real
	}
	return res, os.WriteFile(target, out, 0o600)
}

// withoutAtrium drops atrium's commands from one hook name's matcher list. kept is nil when nothing is left, and an
// entry left with no commands goes too. An entry with nothing removed is its original bytes.
func withoutAtrium(rawList json.RawMessage) (kept json.RawMessage, removed []string, err error) {
	var list []json.RawMessage
	if err := json.Unmarshal(rawList, &list); err != nil {
		return nil, nil, err
	}
	var rest []json.RawMessage
	for _, entry := range list {
		var m map[string]json.RawMessage
		if json.Unmarshal(entry, &m) != nil {
			rest = append(rest, entry)
			continue
		}
		var cmds []json.RawMessage
		if json.Unmarshal(m["hooks"], &cmds) != nil {
			rest = append(rest, entry)
			continue
		}
		var left []json.RawMessage
		for _, c := range cmds {
			var hm struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(c, &hm) == nil && atriumCommand(hm.Command) {
				removed = append(removed, hm.Command)
				continue
			}
			left = append(left, c)
		}
		switch {
		case len(left) == len(cmds):
			rest = append(rest, entry)
		case len(left) == 0:
		default:
			if m["hooks"], err = json.Marshal(left); err != nil {
				return nil, nil, err
			}
			b, err := json.Marshal(m)
			if err != nil {
				return nil, nil, err
			}
			rest = append(rest, b)
		}
	}
	if len(removed) == 0 {
		return rawList, nil, nil
	}
	if len(rest) == 0 {
		return nil, removed, nil
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, e := range rest {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(e)
	}
	buf.WriteByte(']')
	return buf.Bytes(), removed, nil
}

// atriumCommand is a hook command that runs an atrium binary's hook, session or turn subcommand. A script of the
// operator's own, such as the dotfiles gate, is not.
func atriumCommand(command string) bool {
	return atriumWords(command, "hook") || atriumWords(command, "session") || atriumWords(command, "turn")
}

// atriumWords reports whether command runs an atrium binary with sub as its first argument. A leading PowerShell
// call operator is skipped.
func atriumWords(command, sub string) bool {
	f := commandWords(strings.TrimPrefix(strings.TrimSpace(command), "& "))
	if len(f) < 2 || f[1] != sub {
		return false
	}
	base := strings.ToLower(filepath.Base(filepath.ToSlash(f[0])))
	if ext := filepath.Ext(base); ext != "" && ext != ".exe" {
		return false
	}
	return strings.HasPrefix(strings.TrimSuffix(base, ".exe"), "atrium")
}
