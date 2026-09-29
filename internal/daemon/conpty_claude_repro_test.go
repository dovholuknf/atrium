//go:build windows

package daemon

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type killer struct{ p *harnessPTY }

func (k killer) Kill() error { k.p.Kill(); return nil }

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
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(kv), "ATRIUM") || strings.HasPrefix(strings.ToUpper(kv), "CLAUDE_CODE_") ||
			strings.HasPrefix(strings.ToUpper(kv), "CLAUDECODE") {
			continue
		}
		env = append(env, kv)
	}
	env = declareATerminal(append(env, "CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1"))
	// ATRIUM_CONPTY_DLL names a conpty.dll to use instead of the inbox one
	p, err := openHarnessPTY(os.Getenv("ATRIUM_CONPTY_DLL"), 206, 50,
		[]string{exe, "--model", "haiku", "--strict-mcp-config", "--settings", `{"disableAllHooks":true}`},
		os.Getenv("ATRIUM_CLAUDE_DIR"), env)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	c := struct{ Process interface{ Kill() error } }{killer{p}}
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
		// what atrium_say does mid-turn: text, a pause, Enter, while claude is
		// still streaming. Claude shows it as a queued message above the prompt.
		if ty := os.Getenv("ATRIUM_CLAUDE_TYPE"); ty != "" {
			var after, every, times int
			fmt.Sscan(os.Getenv("ATRIUM_CLAUDE_TYPE_AFTER"), &after)
			fmt.Sscan(os.Getenv("ATRIUM_CLAUDE_TYPE_EVERY"), &every)
			fmt.Sscan(os.Getenv("ATRIUM_CLAUDE_TYPE_TIMES"), &times)
			time.Sleep(time.Duration(after) * time.Millisecond)
			for i := 0; i < max(times, 1); i++ {
				say(fmt.Sprintf("[atrium] tester says: %s (%d)", ty, i+1))
				time.Sleep(time.Duration(every) * time.Millisecond)
			}
		}
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
