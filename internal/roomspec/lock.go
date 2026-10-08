package roomspec

import (
	"encoding/json"
	"fmt"
)

// Step statuses, the lock's and the plan's. todo is what an apply would change. human is what only an administrator can do,
// with the lines in AdminLines. fail is an error that is not that.
const (
	StatusOK    = "ok"
	StatusDone  = "done"
	StatusWarn  = "warn"
	StatusTodo  = "todo"
	StatusFail  = "fail"
	StatusHuman = "human"
)

// Exit codes keep provision-room.ps1's.
const (
	ExitOK        = 0
	ExitArgs      = 1  // the spec or the arguments cannot be used
	ExitFail      = 3  // a step failed and it is not for an administrator
	ExitNeedAdmin = 13 // an administrator has to run the printed lines first
)

// Step is one row of a plan or a lock.
type Step struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// PackLock is what a pack resolved to.
type PackLock struct {
	Runner string `json:"runner"`
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Files  int    `json:"files"`
}

// Lock is room.lock: observed, written by the room after an apply and read by the hub. Observed never overwrites desired.
// A plan prints the same shape without applied_at.
type Lock struct {
	Version    int        `json:"version"`
	SpecHash   string     `json:"spec_hash"`
	AppliedAt  string     `json:"applied_at,omitempty"`
	OS         string     `json:"os"`
	Account    string     `json:"account"`
	WorkRoot   string     `json:"work_root"`
	Steps      []Step     `json:"steps"`
	AdminLines []string   `json:"admin_lines"`
	Packs      []PackLock `json:"packs"`
}

// MarshalLock is the lock as written to ~/.atrium/room.lock: indented, ending in a newline, and never null for a list.
func MarshalLock(l Lock) ([]byte, error) {
	if l.Steps == nil {
		l.Steps = []Step{}
	}
	if l.AdminLines == nil {
		l.AdminLines = []string{}
	}
	if l.Packs == nil {
		l.Packs = []PackLock{}
	}
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ParseLock reads a lock, refusing one of another version.
func ParseLock(data []byte) (Lock, error) {
	var l Lock
	if err := json.Unmarshal(data, &l); err != nil {
		return Lock{}, fmt.Errorf("room lock: %w", err)
	}
	if l.Version != Version {
		return Lock{}, fmt.Errorf("room lock: version %d, and this reads %d", l.Version, Version)
	}
	return l, nil
}

// Code is the exit code a set of steps means: 13 when an administrator is needed, 3 when a step failed, else 0. A todo, a warn
// and a done are all 0: a plan that has work to do is not an error.
func Code(steps []Step) int {
	code := ExitOK
	for _, s := range steps {
		switch s.Status {
		case StatusHuman:
			return ExitNeedAdmin
		case StatusFail:
			code = ExitFail
		}
	}
	return code
}
