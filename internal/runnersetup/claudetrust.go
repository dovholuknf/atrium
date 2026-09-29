package runnersetup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Claude Code's folder trust, written at launch. See backlog-2 item 67 and
// docs/runner-setup-design.md.
//
// A launch into a folder claude has never seen stops at "Do you trust the files
// in this folder?", and the first thing typed into the card answers it "No,
// exit". Nobody is watching a launched card's terminal, and on a remote room
// every worktree is new, so the card died "failed to start".
//
// MEASURED FROM THE SHIPPED BINARY (2.1.284), not from the docs:
//
//   - Trust is `projects[<path>].hasTrustDialogAccepted: true` in the global
//     config, `~/.claude.json`. `CLAUDE_CONFIG_DIR` moves it, and a legacy
//     `<config dir>/.config.json` wins when it exists.
//   - The key is the absolute path, with forward slashes on Windows. Claude
//     walks up from the cwd to the git root looking for one, and outside a git
//     repository all the way to the filesystem root. That walk is why home and
//     a filesystem root are never written: either would trust every folder
//     under it that is not in a repository.
//   - Every claude session writes this file, under a proper-lockfile lock: a
//     directory at `<file>.lock`, stale after ten seconds. Under the lock it
//     re-reads the file and merges its change into `projects`. So atrium takes
//     the same lock, re-reads, adds its entry and renames a new file into
//     place. A session writing after that reads the entry back and keeps it.
//   - `CLAUDE_CODE_SANDBOXED` also skips the dialog, and was not used. It
//     trusts every folder the session ever moves to and changes how project
//     permission rules are gated, where this trusts one folder.

// claudeLockStale is proper-lockfile's default, which is what claude uses.
const claudeLockStale = 10 * time.Second

// claudeLockWait bounds how long a launch waits for a session that holds the
// lock. Claude holds it for one read and one write.
var claudeLockWait = 5 * time.Second

// claudeConfigPath is the file claude keeps its global config in.
func claudeConfigPath(env Env) string {
	base := strings.TrimSpace(env.lookup("CLAUDE_CONFIG_DIR"))
	configDir := base
	if configDir == "" {
		configDir = filepath.Join(env.Home, ".claude")
	}
	if legacy := filepath.Join(configDir, ".config.json"); exists(legacy) {
		return legacy
	}
	if base == "" {
		base = env.Home
	}
	return filepath.Join(base, ".claude.json")
}

// claudeTrustKeys are the project keys claude could look the launch folder up
// under: the folder as given, and its real path when a symlink is in the way,
// which on macOS is every temp directory (`/var` is `/private/var`).
func claudeTrustKeys(cwd, goos string) []string {
	var keys []string
	add := func(p string) {
		if p == "" {
			return
		}
		p = filepath.Clean(p)
		if goos == "windows" {
			p = strings.ReplaceAll(p, `\`, "/")
		}
		for _, k := range keys {
			if k == p {
				return
			}
		}
		keys = append(keys, p)
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil
	}
	add(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		add(real)
	}
	return keys
}

// tooWideToTrust is a folder whose trust would cover far more than the launch:
// the home directory, or a filesystem root.
func tooWideToTrust(env Env, cwd string) bool {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return true
	}
	abs = filepath.Clean(abs)
	if filepath.Dir(abs) == abs {
		return true
	}
	if env.Home != "" {
		home := filepath.Clean(env.Home)
		if abs == home || (env.goos() == "windows" && strings.EqualFold(abs, home)) {
			return true
		}
	}
	return false
}

// lockClaudeConfig takes claude's own lock on its config file, the way
// proper-lockfile does: a directory beside it, made atomically. A lock older
// than claude's stale limit belongs to a session that died holding it.
func lockClaudeConfig(path string) (unlock func(), err error) {
	lock := path + ".lock"
	deadline := time.Now().Add(claudeLockWait)
	wait := 20 * time.Millisecond
	for {
		err := os.Mkdir(lock, 0o700)
		if err == nil {
			return func() { _ = os.Remove(lock) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, serr := os.Stat(lock); serr == nil && time.Since(fi.ModTime()) > claudeLockStale {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("a claude session held %s for %s", filepath.ToSlash(lock), claudeLockWait)
		}
		time.Sleep(wait)
		if wait < 200*time.Millisecond {
			wait *= 2
		}
	}
}

// trustClaudeFolder marks cwd trusted in claude's global config. Changed is
// false when every key was already trusted, or when there is no config file:
// a claude that has never run has not been signed in either, and a file atrium
// made up would be missing everything claude's first run writes.
func trustClaudeFolder(env Env, cwd string) (Applied, error) {
	path := claudeConfigPath(env)
	res := Applied{Path: filepath.ToSlash(path)}
	keys := claudeTrustKeys(cwd, env.goos())
	if len(keys) == 0 || !exists(path) {
		return res, nil
	}
	unlock, err := lockClaudeConfig(path)
	if err != nil {
		return res, err
	}
	defer unlock()

	obj, raw, existed, err := readJSONObject(path)
	if err != nil || !existed {
		return res, err
	}
	projects := map[string]json.RawMessage{}
	if p, ok := obj["projects"]; ok && string(p) != "null" {
		if err := json.Unmarshal(p, &projects); err != nil {
			return res, fmt.Errorf("projects in %s is not an object atrium can read: %w", res.Path, err)
		}
	}
	changed := false
	for _, k := range keys {
		entry := map[string]json.RawMessage{}
		if e, ok := projects[k]; ok {
			if err := json.Unmarshal(e, &entry); err != nil {
				return res, fmt.Errorf("projects[%q] in %s is not an object atrium can read: %w", k, res.Path, err)
			}
		}
		if string(entry["hasTrustDialogAccepted"]) == "true" {
			continue
		}
		entry["hasTrustDialogAccepted"] = json.RawMessage("true")
		b, err := json.Marshal(entry)
		if err != nil {
			return res, err
		}
		projects[k] = b
		changed = true
	}
	if !changed {
		return res, nil
	}
	b, err := json.Marshal(projects)
	if err != nil {
		return res, err
	}
	obj["projects"] = b
	return writeJSONFile(path, obj, raw)
}

// claudeLaunch trusts the launch folder before claude starts in it. The folder
// is the one atrium was asked to launch in, which is the operator's choice
// already made, so it is the answer a person at the dialog would have given.
func claudeLaunch(env Env, cwd string) (string, error) {
	if tooWideToTrust(env, cwd) {
		return "", nil
	}
	res, err := trustClaudeFolder(env, cwd)
	if err != nil || !res.Changed {
		return "", err
	}
	return "trusted " + filepath.ToSlash(cwd) + " for claude in " + res.Path, nil
}
