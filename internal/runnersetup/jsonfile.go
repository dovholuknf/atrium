package runnersetup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Reading and writing a runner's own JSON config.
//
// The file belongs to the runner and to the operator, so the rules are the ones
// `claudeconf.InstallOnlyTarget` keeps: refuse what cannot be parsed, keep every
// key that was there, write through a symlink rather than over it. Two differ.
// The write is atomic, because a runner reading a half-written trust file stops
// with a fatal error. And the backups are two named files, not one per write,
// because launch-time trust writes once per new worktree and a timestamped copy
// each time would bury the runner's directory. See docs/runner-setup-design.md.

// readJSONObject reads a file holding one JSON object. A missing file is an
// empty object and existed is false.
func readJSONObject(path string) (obj map[string]json.RawMessage, raw []byte, existed bool, err error) {
	raw, err = os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	obj = map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, raw, true, fmt.Errorf("%s is not a JSON object atrium can read: %w", filepath.ToSlash(path), err)
	}
	return obj, raw, true, nil
}

// Backup suffixes. The original is the file before atrium first touched it and
// is never overwritten. The last is the file before the most recent change.
const (
	backupOriginal = ".atrium-original.bak"
	backupLast     = ".atrium-last.bak"
)

// writeJSONFile replaces the file with obj, keeping both backups of what was
// there. raw is the previous content, nil when there was no file.
//
// The backups are written before the file is touched, and a failure to write
// them leaves the file alone.
func writeJSONFile(path string, obj map[string]json.RawMessage, raw []byte) (Applied, error) {
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return Applied{}, err
	}
	out = append(out, '\n')

	target := path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		target = real
	}
	res := Applied{Changed: true, Path: filepath.ToSlash(path)}
	if raw != nil {
		orig := target + backupOriginal
		if !exists(orig) {
			if err := os.WriteFile(orig, raw, 0o600); err != nil {
				return Applied{}, fmt.Errorf("could not keep a copy of %s, so nothing was changed: %w", path, err)
			}
		}
		last := target + backupLast
		if err := os.WriteFile(last, raw, 0o600); err != nil {
			return Applied{}, fmt.Errorf("could not keep a copy of %s, so nothing was changed: %w", path, err)
		}
		res.Backup = filepath.ToSlash(last)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return Applied{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".atrium-*.tmp")
	if err != nil {
		return Applied{}, err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return Applied{}, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return Applied{}, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return Applied{}, err
	}
	return res, nil
}
