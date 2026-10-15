// Package dblock is the hold a store keeps on its database file for as long as it is open.
//
// SQLite lets any number of processes open one file, and two hubs or two rooms doing so would each carry on serving
// what the other is rewriting. The hold is an operating system lock on a sidecar file next to the database, so a
// holder that dies gives it up with the process and a crash never blocks a restart. The pid is written into the
// sidecar only so a refused opener can say who to look at: the lock is the truth, the pid is a hint.
package dblock

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrHeld is what a second opener gets. Test with errors.Is.
var ErrHeld = errors.New("store held by another process")

// HeldError says who holds the file, when that could be read.
type HeldError struct {
	Path string
	Pid  int
}

func (e *HeldError) Error() string {
	if e.Pid > 0 {
		return fmt.Sprintf("store held by another process, pid %d (%s)", e.Pid, e.Path)
	}
	return fmt.Sprintf("store held by another process (%s)", e.Path)
}

func (e *HeldError) Is(target error) bool { return target == ErrHeld }

// Lock is a hold on one database file.
type Lock struct {
	f *os.File
}

// Path is the sidecar for a database path.
func Path(db string) string { return db + ".lock" }

// Acquire takes the hold or returns a *HeldError. Release gives it up.
func Acquire(db string) (*Lock, error) {
	p := Path(db)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", p, err)
	}
	if err := lockFile(f); err != nil {
		pid := readPid(p)
		f.Close()
		if isHeld(err) {
			return nil, &HeldError{Path: db, Pid: pid}
		}
		return nil, fmt.Errorf("lock %s: %w", p, err)
	}
	// Best effort: the lock is already ours, the pid only helps whoever is refused next.
	if err := f.Truncate(0); err == nil {
		f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}
	return &Lock{f: f}, nil
}

// Release gives the hold up. The sidecar is left in place: removing it would race a new holder.
func (l *Lock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	unlockFile(l.f)
	err := l.f.Close()
	l.f = nil
	return err
}

func readPid(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}
