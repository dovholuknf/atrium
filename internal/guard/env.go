package guard

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Env is everything the guard asks of the machine, so a test can answer it.
type Env interface {
	// Git runs git in dir and returns its stdout.
	Git(dir string, args ...string) (string, error)
	// Stat says whether path exists and is a directory.
	Stat(path string) (isDir, ok bool)
	// HasPrefix says whether dir holds an entry whose name starts with prefix.
	HasPrefix(dir, prefix string) bool
	ReadFile(path string) ([]byte, error)
	Getenv(key string) string
	// Agent is this room's agent address, http://127.0.0.1:<port>, or empty
	// when no room is known. The hub remote must point under <agent>/git/.
	Agent() string
}

// gitTimeout bounds one git call. The hook's own timeout in settings.json is
// the bound on the whole thing.
const gitTimeout = 3 * time.Second

// OSEnv is the real machine. AgentFunc says where the room is.
type OSEnv struct {
	AgentFunc func() string
}

func (e OSEnv) Git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	return string(out), err
}

func (OSEnv) Stat(path string) (bool, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return false, false
	}
	return st.IsDir(), true
}

func (OSEnv) HasPrefix(dir, prefix string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	p := strings.ToLower(prefix)
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(strings.ToLower(e.Name()), p) {
			return true
		}
	}
	return false
}

func (OSEnv) ReadFile(path string) ([]byte, error) { return os.ReadFile(filepath.Clean(path)) }

func (OSEnv) Getenv(key string) string { return os.Getenv(key) }

func (e OSEnv) Agent() string {
	if e.AgentFunc == nil {
		return ""
	}
	return e.AgentFunc()
}
