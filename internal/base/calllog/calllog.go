// Package calllog is the durable record of every tool and hook call: one line when it starts, one when it ends.
package calllog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Max is the size at which the live file becomes <file>.1. One old file is kept, so a log is bounded at twice this.
const Max = 8 << 20

const (
	OK         = "ok"
	Error      = "error"
	Timeout    = "timeout"
	ClientGone = "client gone"
)

// Line is one start or end. An end carries the start time, so a log cut at the end line still reads alone.
type Line struct {
	Event   string     `json:"event"`
	ID      uint64     `json:"id"`
	Kind    string     `json:"kind"`
	Name    string     `json:"name"`
	Caller  string     `json:"caller,omitempty"`
	Room    string     `json:"room,omitempty"`
	Start   time.Time  `json:"start"`
	End     *time.Time `json:"end,omitempty"`
	Ms      int64      `json:"ms,omitempty"`
	Outcome string     `json:"outcome,omitempty"`
	Error   string     `json:"error,omitempty"`
}

// Log appends JSON lines to one file under dir. The zero value and a nil *Log drop every line.
type Log struct {
	path string
	max  int64
	mu   sync.Mutex
	f    *os.File
	size int64
	seq  uint64
}

// New logs to dir/file, rotating at max bytes (Max when max is not positive).
func New(dir, file string, max int64) *Log {
	if max <= 0 {
		max = Max
	}
	return &Log{path: filepath.Join(dir, file), max: max}
}

// Begin writes the start line before the caller runs anything and returns the function that writes the end line.
// A write error is dropped: a log never fails a call.
func (l *Log) Begin(kind, name, caller, room string) func(outcome string, err error) {
	if l == nil || l.path == "" {
		return func(string, error) {}
	}
	start := time.Now()
	l.mu.Lock()
	l.seq++
	id := l.seq
	l.mu.Unlock()
	base := Line{ID: id, Kind: kind, Name: name, Caller: caller, Room: room, Start: start}
	s := base
	s.Event = "start"
	l.write(s)
	return func(outcome string, err error) {
		e := base
		e.Event = "end"
		now := time.Now()
		e.End = &now
		e.Ms = now.Sub(start).Milliseconds()
		e.Outcome = outcome
		if err != nil {
			e.Error = err.Error()
		}
		l.write(e)
	}
}

// Close releases the file. A later line reopens it.
func (l *Log) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}

func (l *Log) write(v Line) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		if os.MkdirAll(filepath.Dir(l.path), 0o700) != nil {
			return
		}
		f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			return
		}
		l.f, l.size = f, fi.Size()
	}
	if l.size >= l.max {
		l.f.Close()
		l.f = nil
		_ = os.Remove(l.path + ".1")
		_ = os.Rename(l.path, l.path+".1")
		f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		l.f, l.size = f, 0
	}
	// An unbuffered write goes to the OS at once, so a crash of this process loses nothing.
	n, err := l.f.Write(b)
	l.size += int64(n)
	if err != nil {
		l.f.Close()
		l.f = nil
	}
}

// Outcome names how a call ended, a gone caller and a deadline apart from a failure.
func Outcome(ctx context.Context, err error) string {
	switch {
	case err == nil:
		return OK
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return Timeout
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return ClientGone
	}
	return Error
}
