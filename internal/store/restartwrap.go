package store

import (
	"strconv"
	"strings"
	"time"
)

// The wrap-up a room restart waits for. See docs/changes/r-graceful-room-restart.md.

// SettingRestartWrapWait is how many seconds a restart waits for its working sessions to wrap up. Empty reads as
// DefaultRestartWrapWait.
const SettingRestartWrapWait = "restart_wrap_wait_s"

// The range and default of the wait, in seconds.
const (
	MinRestartWrapWaitS     = 10
	MaxRestartWrapWaitS     = 3600
	DefaultRestartWrapWaitS = 300
)

// RestartWrapWait reads the wait. Unset or unreadable reads as the default.
func (s *Store) RestartWrapWait() time.Duration {
	v, err := s.Setting(SettingRestartWrapWait)
	if err != nil {
		return DefaultRestartWrapWaitS * time.Second
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < MinRestartWrapWaitS || n > MaxRestartWrapWaitS {
		return DefaultRestartWrapWaitS * time.Second
	}
	return time.Duration(n) * time.Second
}

// CheckRestartWrapWait validates a typed wait. Empty is the default and is returned as empty.
func CheckRestartWrapWait(v string) (string, error) {
	return checkInt(SettingRestartWrapWait, "seconds", v, MinRestartWrapWaitS, MaxRestartWrapWaitS)
}
