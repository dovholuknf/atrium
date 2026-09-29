//go:build windows

package daemon

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aymanbagabas/go-pty"
	"golang.org/x/sys/windows"
)

// scratch repro for backlog-2 item 74. not for commit.
func TestConPTYScrollChild(t *testing.T) {
	if os.Getenv("ATRIUM_CONPTY_CHILD") == "" {
		t.Skip("child only")
	}
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	_ = windows.GetConsoleMode(h, &mode)
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT)
	var direct strings.Builder
	defer func() {
		if p := os.Getenv("ATRIUM_CONPTY_DIRECT"); p != "" {
			_ = os.WriteFile(p, []byte(direct.String()), 0o644)
		}
	}()
	w := func(s string) { direct.WriteString(s); _, _ = os.Stdout.WriteString(s) }
	sep, eol := strings.Repeat("-", 60), "\r\n"
	if os.Getenv("ATRIUM_CONPTY_FULLSEP") != "" {
		sep = strings.Repeat("─", 120)
	}
	if os.Getenv("ATRIUM_CONPTY_LF") != "" {
		eol = "\n"
	}
	dyn := func(tag string) string {
		return "* spinner " + tag + eol + eol + sep + eol + "> " + eol + sep + eol + "  footer"
	}
	for i := 1; i <= 80; i++ {
		w(fmt.Sprintf("base %03d\r\n", i))
	}
	park := os.Getenv("ATRIUM_CONPTY_PARK") != ""
	lead := os.Getenv("ATRIUM_CONPTY_LEAD") != ""
	// the dynamic region is 6 rows, or 7 with a leading "Calling tool…" row
	rows := 6
	first := ""
	if lead {
		first = "  Calling tool… (ctrl+o)\r\n"
		rows = 7
	}
	w(first + dyn("a"))
	if park {
		// claude parks its cursor on the prompt row, 2 above the bottom
		w("\x1b[2A\r\x1b[2C")
	}
	time.Sleep(400 * time.Millisecond)
	var b strings.Builder
	b.WriteString("\x1b[?2026h")
	up := rows - 1
	if park {
		up -= 2
	}
	b.WriteString("\r")
	for i := 0; i < up; i++ {
		b.WriteString("\x1b[2K\x1b[1A")
	}
	b.WriteString("\x1b[2K\r\x1b[J")
	if clr := os.Getenv("ATRIUM_CONPTY_CLEAR"); clr != "" {
		// claude's full redraw: clear, clear scrollback, home, and every line again
		b.Reset()
		b.WriteString("\x1b[?2026h" + clr + "\x1b[H")
		for i := 1; i <= 80; i++ {
			b.WriteString(fmt.Sprintf("base %03d\r\n", i))
		}
	}
	n := 12
	if s := os.Getenv("ATRIUM_CONPTY_NEW"); s != "" {
		fmt.Sscan(s, &n)
	}
	if lead {
		b.WriteString("  Called tool (ctrl+o)\r\n\r\n")
	}
	nl := "\r\n"
	if os.Getenv("ATRIUM_CONPTY_LF") != "" {
		nl = "\n"
	}
	wide := os.Getenv("ATRIUM_CONPTY_WIDE") != ""
	for i := 1; i <= n; i++ {
		s := fmt.Sprintf("new %02d", i)
		if wide && i%3 == 0 {
			// exactly the terminal's width, so the cursor sits in the pending wrap
			s += strings.Repeat(".", 120-len(s))
		}
		b.WriteString(s + nl)
	}
	b.WriteString(dyn("b"))
	if park {
		b.WriteString("\x1b[2A\r\x1b[2C")
	}
	b.WriteString("\x1b[?2026l")
	frame := b.String()
	switch mode := os.Getenv("ATRIUM_CONPTY_REGION"); mode {
	case "stbm", "su":
		// make room above a fixed bottom block: scroll rows 1..44 up by n,
		// then write the new lines into the rows that opened
		var r strings.Builder
		r.WriteString("\x1b[?2026h")
		if mode == "stbm" {
			r.WriteString("\x1b[1;44r\x1b[44;1H")
			r.WriteString(strings.Repeat("\n", n))
			r.WriteString("\x1b[r")
		} else {
			r.WriteString("\x1b[1;44r" + fmt.Sprintf("\x1b[%dS", n) + "\x1b[r")
		}
		r.WriteString(fmt.Sprintf("\x1b[%d;1H", 44-n+1))
		for i := 1; i <= n; i++ {
			r.WriteString(fmt.Sprintf("new %02d\x1b[K", i))
			if i < n {
				r.WriteString("\r\n")
			}
		}
		r.WriteString("\x1b[48;3H\x1b[?2026l")
		frame = r.String()
	}
	switch os.Getenv("ATRIUM_CONPTY_SPLIT") {
	case "sync":
		// BSU, the frame, and ESU as three writes, a moment apart
		body := strings.TrimSuffix(strings.TrimPrefix(frame, "\x1b[?2026h"), "\x1b[?2026l")
		w("\x1b[?2026h")
		time.Sleep(20 * time.Millisecond)
		w(body)
		time.Sleep(20 * time.Millisecond)
		w("\x1b[?2026l")
	case "nosync":
		w(strings.ReplaceAll(strings.ReplaceAll(frame, "\x1b[?2026h", ""), "\x1b[?2026l", ""))
	default:
		w(frame)
	}
	time.Sleep(600 * time.Millisecond)
}

func TestConPTYScrollRepro(t *testing.T) {
	out := os.Getenv("ATRIUM_CONPTY_OUT")
	if out == "" {
		t.Skip("set ATRIUM_CONPTY_OUT")
	}
	exe, _ := os.Executable()
	p, err := pty.New()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_ = p.Resize(120, 50)
	c := p.Command(exe, "-test.run=^TestConPTYScrollChild$", "-test.count=1")
	c.Env = append(os.Environ(), "ATRIUM_CONPTY_CHILD=1")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	var got []byte
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := p.Read(buf)
			got = append(got, buf[:n]...)
			if err != nil {
				close(done)
				return
			}
		}
	}()
	_ = c.Wait()
	time.Sleep(500 * time.Millisecond)
	p.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	_ = os.WriteFile(out, got, 0o644)
	_ = exec.Command
	_ = io.EOF
	t.Logf("wrote %d bytes", len(got))
}
