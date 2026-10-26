package daemon

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// A worker launched without go or make on its PATH is the ROOM's misconfiguration, not the worker's finding: it is
// logged and carried on `/v1/health` and the `health` event as `toolchain_missing`. The launch still goes ahead.

// toolchainNeeds is each tool checked and the file in the card's folder that says the work needs it.
var toolchainNeeds = []struct{ tool, marker string }{
	{"go", "go.mod"},
	{"make", "Makefile"},
}

type toolchainState struct {
	mu      sync.Mutex
	missing []string
	card    string
	since   time.Time
}

// toolchainMissing is each tool the work in cwd needs that the env's PATH does not hold. checked is false when cwd
// needs none, so a launch elsewhere does not clear a miss.
func toolchainMissing(goos, cwd string, env []string, exists func(string) bool) (out []string, checked bool) {
	get := func(k string) string {
		v := ""
		for _, kv := range env {
			if i := strings.Index(kv, "="); i > 0 && strings.EqualFold(kv[:i], k) {
				v = kv[i+1:]
			}
		}
		return v
	}
	sep, exts := ":", []string{""}
	if goos == "windows" {
		sep, exts = ";", strings.Split(strings.ToLower(get("PATHEXT")), ";")
		if get("PATHEXT") == "" {
			exts = []string{".exe", ".cmd", ".bat"}
		}
	}
	for _, n := range toolchainNeeds {
		if cwd == "" || !exists(filepath.Join(cwd, n.marker)) {
			continue
		}
		checked = true
		found := false
		for _, dir := range strings.Split(get("PATH"), sep) {
			for _, ext := range exts {
				if dir = strings.TrimSpace(dir); dir != "" && exists(filepath.Join(dir, n.tool+ext)) {
					found = true
				}
			}
		}
		if !found {
			out = append(out, n.tool)
		}
	}
	return out, checked
}

// checkToolchain records what a launch's PATH lacks, and publishes health when that changes.
func (d *Daemon) checkToolchain(taskID, cwd string, env []string) {
	missing, checked := toolchainMissing(runtime.GOOS, cwd, env, func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	})
	if !checked {
		return
	}
	d.toolchain.mu.Lock()
	changed := strings.Join(missing, " ") != strings.Join(d.toolchain.missing, " ")
	d.toolchain.missing, d.toolchain.card = missing, taskID
	if changed {
		d.toolchain.since = time.Now()
	}
	d.toolchain.mu.Unlock()
	if len(missing) > 0 {
		log.Printf("[atrium] room misconfigured: %s launched without %s on its PATH", taskID, strings.Join(missing, ", "))
	}
	if changed {
		d.publishHealth()
	}
}

// toolchainHealth is what `/v1/health` and the `health` event carry: absent when fine.
func (d *Daemon) toolchainHealth() map[string]any {
	d.toolchain.mu.Lock()
	defer d.toolchain.mu.Unlock()
	if len(d.toolchain.missing) == 0 {
		return nil
	}
	return map[string]any{
		"tools":  d.toolchain.missing,
		"reason": "a worker launched here without " + strings.Join(d.toolchain.missing, ", ") + " on its PATH",
		"card":   d.toolchain.card, "since": d.toolchain.since,
	}
}
