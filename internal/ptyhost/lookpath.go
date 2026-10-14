package ptyhost

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// lookPathIn resolves a bare command name against the PATH the spawn carries, then against the host's own. A host made
// outside the room's environment (started through WMI, or by a service) gets the system PATH, which does not hold the
// user's `claude`, while the spawn's env is the room's and does.
func lookPathIn(file string, env []string) (string, error) {
	if strings.ContainsAny(file, `/\`) {
		return exec.LookPath(file)
	}
	if path, ok := envValue(env, "PATH"); ok {
		exts := []string{""}
		if runtime.GOOS == "windows" {
			pe := os.Getenv("PATHEXT")
			if v, found := envValue(env, "PATHEXT"); found {
				pe = v
			}
			if pe == "" {
				pe = ".COM;.EXE;.BAT;.CMD"
			}
			exts = nil
			for _, e := range strings.Split(strings.ToLower(pe), ";") {
				if e != "" {
					exts = append(exts, e)
				}
			}
			if filepath.Ext(file) != "" {
				exts = append([]string{""}, exts...)
			}
		}
		for _, dir := range filepath.SplitList(path) {
			if dir == "" {
				continue
			}
			for _, e := range exts {
				p := filepath.Join(dir, file+e)
				if st, err := os.Stat(p); err == nil && !st.IsDir() && (runtime.GOOS == "windows" || st.Mode()&0o111 != 0) {
					return p, nil
				}
			}
		}
	}
	return exec.LookPath(file)
}

// envValue reads KEY from a KEY=value list, case-insensitively on Windows, where the variable is `Path`.
func envValue(env []string, key string) (string, bool) {
	var v string
	found := false
	for _, kv := range env {
		k, val, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if k == key || (runtime.GOOS == "windows" && strings.EqualFold(k, key)) {
			v, found = val, true
		}
	}
	return v, found
}
