package daemon

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/userenv"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
)

const promptFileName = "PROMPT.md"

// cmdSafePrompt moves a prompt cmd.exe would read as syntax into PROMPT.md in dir and points the runner at it, for a
// runner atrium can start only through cmd.exe. Anything else is returned as it is.
func cmdSafePrompt(exe, dir string, args []string, prompt string) ([]string, string) {
	p := strings.TrimSpace(prompt)
	resolved, err := userenv.LookPath(exe)
	if p == "" || err != nil || !shared.RefusedByCmd(resolved, p) {
		return args, prompt
	}
	path := filepath.ToSlash(filepath.Join(dir, promptFileName))
	pointer := "Read " + path + " first and do what it says. It is your opening instruction."
	if shared.RefusedByCmd(resolved, pointer) {
		return args, prompt
	}
	if err := os.WriteFile(path, []byte(p+"\n"), 0o644); err != nil {
		log.Printf("[atrium] could not write the opening prompt to %s: %v", path, err)
		return args, prompt
	}
	next := make([]string, len(args))
	for i, a := range args {
		next[i] = strings.ReplaceAll(a, p, pointer)
	}
	return next, pointer
}
