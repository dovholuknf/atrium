//go:build windows

package daemon

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// scratch repro for backlog-2 item 74. not for commit.
func TestConPTYClaudeRepro(t *testing.T) {
	out := os.Getenv("ATRIUM_CLAUDE_OUT")
	if out == "" {
		t.Skip("set ATRIUM_CLAUDE_OUT")
	}
	exe, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pty.New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_ = p.Resize(206, 50)
	c := p.Command(exe, "--model", "haiku", "--strict-mcp-config",
		"--settings", `{"disableAllHooks":true}`)
	c.Dir = os.Getenv("ATRIUM_CLAUDE_DIR")
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(kv), "ATRIUM") || strings.HasPrefix(strings.ToUpper(kv), "CLAUDE_CODE_") ||
			strings.HasPrefix(strings.ToUpper(kv), "CLAUDECODE") {
			continue
		}
		env = append(env, kv)
	}
	c.Env = declareATerminal(append(env, "CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1"))
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []byte
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := p.Read(buf)
			mu.Lock()
			got = append(got, buf[:n]...)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	snap := func() []byte { mu.Lock(); defer mu.Unlock(); return append([]byte(nil), got...) }
	waitFor := func(needle string, from int, d time.Duration) int {
		end := time.Now().Add(d)
		for time.Now().Before(end) {
			b := snap()
			if from < len(b) && bytes.Contains(b[from:], []byte(needle)) {
				return len(b)
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Logf("timed out waiting for %q", needle)
		return len(snap())
	}
	settle := func() {
		last := -1
		for {
			time.Sleep(3 * time.Second)
			n := len(snap())
			if n == last {
				return
			}
			last = n
		}
	}
	say := func(s string) {
		_, _ = p.Write([]byte(s))
		time.Sleep(500 * time.Millisecond)
		_, _ = p.Write([]byte("\r"))
	}
	waitFor("❯", 0, 60*time.Second)
	settle()
	if bytes.Contains(snap(), []byte("trust")) {
		t.Fatal("trust dialog showed, refusing to answer it")
	}
	// prompt=>marker|prompt=>marker
	for _, step := range strings.Split(os.Getenv("ATRIUM_CLAUDE_PROMPTS"), "|") {
		pr, marker, _ := strings.Cut(step, "=>")
		at := len(snap())
		say(pr)
		waitFor(marker, at, 180*time.Second)
		settle()
	}
	_ = os.WriteFile(out, snap(), 0o644)
	_, _ = p.Write([]byte("\x03"))
	time.Sleep(300 * time.Millisecond)
	_, _ = p.Write([]byte("\x03"))
	time.Sleep(1 * time.Second)
	_ = c.Process.Kill()
}
