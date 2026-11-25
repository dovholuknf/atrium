package daemon

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
	"github.com/dovholuknf/atrium/internal/runnersetup"
	"github.com/dovholuknf/atrium/internal/store"
)

// prepareRunnerSetup runs the runner's adapter step for the launch folder,
// before the process starts. For gemini that trusts a new worktree inside a
// workspace root or the card root, so gemini does not stop at a trust prompt nobody is watching.
//
// NEVER FAILS A LAUNCH. A write that goes wrong is logged and the runner starts
// anyway, and the runner's own prompt is the fallback. See
// docs/runtime/runner-setup-design.md.
//
// env is the environment the runner will get, prepare step included, so a
// GEMINI_CLI_HOME set by the row or by a prepare command is the one honoured.
func (d *Daemon) prepareRunnerSetup(h *store.Harness, cwd string, env []string) {
	a := runnersetup.For(h)
	if a == nil || a.Launch == nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	ps, err := d.st.Providers()
	if err != nil {
		log.Printf("[atrium] runner setup for %s skipped, providers unreadable: %v", h.ID, err)
		return
	}
	vars := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	roots := runnersetup.WorkspaceRoots(ps)
	if root, err := d.CardRoot(); err == nil {
		roots = append(roots, root)
	}
	exe, _ := os.Executable()
	if hook := claudeconf.HookExe(exe); hook != "" {
		exe = hook
	}
	note, err := a.Launch(runnersetup.Env{
		Home:      home,
		Getenv:    func(k string) string { return vars[k] },
		Roots:     roots,
		AtriumExe: filepath.ToSlash(exe),
	}, cwd)
	if err != nil {
		log.Printf("[atrium] runner setup for %s in %s did not apply: %v", h.ID, cwd, err)
		return
	}
	if note != "" {
		log.Printf("[atrium] %s", note)
	}
}
