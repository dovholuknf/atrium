//go:build windows

package daemon

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

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
	if s := os.Getenv("ATRIUM_CONPTY_STREAM"); s != "" {
		// a steady scroll for the parent to resize under
		var n int
		fmt.Sscan(s, &n)
		for i := 1; i <= n; i++ {
			w(fmt.Sprintf("new %02d\r\n", i))
			time.Sleep(3 * time.Millisecond)
		}
		time.Sleep(600 * time.Millisecond)
		return
	}
	if s := os.Getenv("ATRIUM_CONPTY_LOOP"); s != "" {
		// claude-shaped frames, over and over: the cursor parked on the prompt,
		// erase the 7-row bottom block from there, write "Called" plus 10 new
		// numbered lines, redraw the block, park again. Spinner ticks between.
		var loops int
		fmt.Sscan(s, &loops)
		block := func() string {
			return "  Calling tool… (ctrl+o)" + eol + dyn("x")
		}
		w(block() + "\x1b[2A\r\x1b[2C")
		k := 0
		for l := 0; l < loops; l++ {
			for tick := 0; tick < 4; tick++ {
				time.Sleep(15 * time.Millisecond)
				w(fmt.Sprintf("\x1b[?2026h\x1b[s\x1b[3A\r* spinner %d\x1b[K\x1b[u\x1b[?2026l", tick))
			}
			var b strings.Builder
			b.WriteString("\x1b[?2026h\r")
			for i := 0; i < 4; i++ {
				b.WriteString("\x1b[2K\x1b[1A")
			}
			b.WriteString("\x1b[2K\r\x1b[J  Called tool (ctrl+o)" + eol + eol)
			for i := 0; i < 10; i++ {
				k++
				b.WriteString(fmt.Sprintf("new %02d%s", k, eol))
			}
			b.WriteString(eol + block() + "\x1b[2A\r\x1b[2C\x1b[?2026l")
			w(b.String())
		}
		time.Sleep(600 * time.Millisecond)
		return
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
	// a hook or a Bash tool call: a child process on the same console, its
	// output to a pipe, started just before (or after) the frame
	var kid *exec.Cmd
	spawn := func() {
		if cl := os.Getenv("ATRIUM_CONPTY_SPAWN"); cl != "" {
			parts := strings.Fields(cl)
			kid = exec.Command(parts[0], parts[1:]...)
			kid.Stdout, kid.Stderr = io.Discard, io.Discard
			_ = kid.Start()
		}
	}
	after := os.Getenv("ATRIUM_CONPTY_SPAWN_AFTER") != ""
	if !after {
		spawn()
		if ms := os.Getenv("ATRIUM_CONPTY_SPAWN_MS"); ms != "" {
			var d int
			fmt.Sscan(ms, &d)
			time.Sleep(time.Duration(d) * time.Millisecond)
		}
	}
	defer func() {
		if kid != nil {
			_ = kid.Wait()
		}
	}()
	defer func() {
		if after {
			spawn()
			time.Sleep(600 * time.Millisecond)
		}
	}()
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
	// ATRIUM_CONPTY_DLL names a conpty.dll to use instead of the inbox one
	p, err := openHarnessPTY(os.Getenv("ATRIUM_CONPTY_DLL"), 120, 50,
		[]string{exe, "-test.run=^TestConPTYScrollChild$", "-test.count=1"}, "",
		append(os.Environ(), "ATRIUM_CONPTY_CHILD=1"))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	c := struct{ Wait func() error }{func() error { p.Wait(); return nil }}
	var got []byte
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := p.Read(buf)
			mu.Lock()
			got = append(got, buf[:n]...)
			mu.Unlock()
			if err != nil {
				close(done)
				return
			}
		}
	}()
	// what an attach, a refit or a second viewer does: setViewport -> Resize.
	// Each one is recorded at the byte offset it happened at, so a replay can
	// resize its grid at the same point the board's would have.
	flipRows, flipHold := 49, 0
	if s := os.Getenv("ATRIUM_CONPTY_FLIP_ROWS"); s != "" {
		fmt.Sscan(s, &flipRows)
	}
	if s := os.Getenv("ATRIUM_CONPTY_FLIP_HOLD"); s != "" {
		fmt.Sscan(s, &flipHold)
	}
	var marks []string
	mark := func(cols, rows int) {
		mu.Lock()
		marks = append(marks, fmt.Sprintf("%d %d %d", len(got), cols, rows))
		mu.Unlock()
	}
	defer func() { _ = os.WriteFile(out+".sizes", []byte(strings.Join(marks, "\n")), 0o644) }()
	stop := make(chan struct{})
	resizes := 0
	// ATRIUM_CONPTY_VIA=runner sends each flip through a runner's viewports,
	// the way a second viewer passing through does, so the height hold decides
	// what reaches the pseudo console. Only resizes that really land are
	// marked and counted.
	var via *runner
	if os.Getenv("ATRIUM_CONPTY_VIA") == "runner" {
		f := newFakePTY()
		defer f.Close()
		via = &runner{
			taskID:   "harness",
			pty:      &harnessViaPTY{fakePTY: f, p: p, mark: mark, n: &resizes},
			buf:      newRingSized(1<<16, 120, 50),
			watchers: map[chan []byte]struct{}{},
			done:     make(chan struct{}),
		}
		_ = via.setViewport("main", 120, 50)
	}
	if mode := os.Getenv("ATRIUM_CONPTY_RESIZE"); mode != "" && via != nil {
		go func() {
			time.Sleep(time.Second)
			for {
				select {
				case <-stop:
					return
				case <-time.After(150 * time.Millisecond):
				}
				_ = via.setViewport("flip", 120, flipRows)
				if flipHold > 0 {
					time.Sleep(time.Duration(flipHold) * time.Millisecond)
				}
				via.dropViewport("flip")
			}
		}()
	} else if mode != "" {
		go func() {
			time.Sleep(time.Second)
			for {
				select {
				case <-stop:
					return
				case <-time.After(150 * time.Millisecond):
				}
				if mode == "focus" {
					// what xterm sends on blur and focus, forwarded by the board
					_, _ = p.Write([]byte("\x1b[O"))
					time.Sleep(20 * time.Millisecond)
					_, _ = p.Write([]byte("\x1b[I"))
					resizes++
					continue
				}
				if mode == "alt" || mode == "altc" {
					// one single-step change per tick, alternating
					c, r := 120, 50
					if resizes%2 == 0 {
						if mode == "alt" {
							r = flipRows
						} else {
							c = 121
						}
					}
					mark(c, r)
					_ = p.Resize(c, r)
					resizes++
					continue
				}
				if mode == "flip" {
					mark(120, flipRows)
					_ = p.Resize(120, flipRows)
					if flipHold > 0 {
						time.Sleep(time.Duration(flipHold) * time.Millisecond)
					}
				}
				mark(120, 50)
				_ = p.Resize(120, 50)
				resizes++
			}
		}()
	}
	_ = c.Wait()
	close(stop)
	if via != nil {
		// Taken so the count is read after the last resize that landed.
		via.resizeMu.Lock()
		via.resizeMu.Unlock()
	}
	t.Logf("resizes %d", resizes)
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
