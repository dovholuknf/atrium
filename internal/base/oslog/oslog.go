// Package oslog sends the standard logger to the OS's own log as well as a bounded rolling file, and reads the file back.
package oslog

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	FileName  = "atrium.log"
	maxBytes  = 5 << 20
	keepFiles = 2
)

// Setup tees the standard logger into <dir>/atrium.log and the OS log, keeping what it wrote to before. The returned
// func undoes it. A sink that cannot open is skipped: logging never stops a process.
func Setup(dir string) func() {
	prev := log.Writer()
	ws := []io.Writer{prev}
	var closers []io.Closer
	if err := os.MkdirAll(dir, 0o700); err == nil {
		r := &rolling{path: filepath.Join(dir, FileName), max: maxBytes}
		ws = append(ws, r)
		closers = append(closers, r)
	}
	if s := openSink(); s != nil {
		ws = append(ws, quiet{s})
		closers = append(closers, s)
	}
	log.SetOutput(io.MultiWriter(ws...))
	return func() {
		log.SetOutput(prev)
		for _, c := range closers {
			_ = c.Close()
		}
	}
}

// Event logs one process lifecycle line at level info: what happened to which pid, why, and who asked.
func Event(component, what string, pid int, reason, by string) {
	event("info", component, what, pid, reason, by)
}

// Warn is Event for an end nobody asked for or that went wrong, so the OS log files it as a warning.
func Warn(component, what string, pid int, reason, by string) {
	event("warn", component, what, pid, reason, by)
}

func event(level, component, what string, pid int, reason, by string) {
	log.Printf("[%s] %s pid=%d reason=%q by=%q level=%s", component, what, pid, reason, by, level)
}

// Level is the level= field of a log line, info when it has none.
func Level(line string) string {
	i := strings.LastIndex(line, " level=")
	if i < 0 {
		return "info"
	}
	l := strings.TrimSpace(line[i+len(" level="):])
	switch l {
	case "warn", "error", "debug":
		return l
	}
	return "info"
}

// quiet drops a sink's errors so the writers after it still run.
type quiet struct{ io.Writer }

func (q quiet) Write(p []byte) (int, error) { _, _ = q.Writer.Write(p); return len(p), nil }

// rolling appends to path and, past max bytes, moves it to path.1 and so on, dropping the oldest.
type rolling struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func (r *rolling) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil && r.size > 0 && r.size+int64(len(p)) > r.max {
		_ = r.f.Close()
		r.f = nil
		for i := keepFiles - 1; i >= 1; i-- {
			_ = os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
		}
		_ = os.Rename(r.path, r.path+".1")
	}
	if r.f == nil {
		f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return len(p), nil
		}
		r.f, r.size = f, 0
		if fi, err := f.Stat(); err == nil {
			r.size = fi.Size()
		}
	}
	n, _ := r.f.Write(p)
	r.size += int64(n)
	return len(p), nil
}

func (r *rolling) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// Tail returns the last n lines of the log in dir, oldest first, keeping only lines of component when it is set.
func Tail(dir, component string, n int) ([]string, error) {
	var out []string
	for i := keepFiles; i >= 0; i-- {
		p := filepath.Join(dir, FileName)
		if i > 0 {
			p = fmt.Sprintf("%s.%d", p, i)
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			if Match(sc.Text(), component) {
				out = append(out, sc.Text())
			}
		}
		_ = f.Close()
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

// Match reports whether a log line belongs to component: its "[component]" tag. An empty component matches all.
func Match(line, component string) bool {
	return component == "" || strings.Contains(line, "["+component+"]")
}

// Follow prints lines of component appended to path from now on, until ctx ends. It reopens the file when a roll
// replaces it.
func Follow(ctx context.Context, path, component string, w io.Writer, every time.Duration) error {
	var f *os.File
	var br *bufio.Reader
	defer func() {
		if f != nil {
			_ = f.Close()
		}
	}()
	first := true
	for {
		if f != nil {
			if cur, err := os.Stat(path); err == nil {
				if open, err := f.Stat(); err == nil && !os.SameFile(cur, open) {
					_ = f.Close()
					f = nil
				}
			}
		}
		if f == nil {
			if nf, err := os.Open(path); err == nil {
				f = nf
				if first {
					_, _ = f.Seek(0, io.SeekEnd)
				}
				br = bufio.NewReader(f)
			}
			first = false
		}
		for f != nil {
			line, err := br.ReadString('\n')
			if err != nil {
				break
			}
			if line = strings.TrimRight(line, "\r\n"); Match(line, component) {
				fmt.Fprintln(w, line)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(every):
		}
	}
}
