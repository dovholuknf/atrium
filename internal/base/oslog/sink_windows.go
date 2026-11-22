//go:build windows

package oslog

import (
	"io"
	"strings"

	"golang.org/x/sys/windows/svc/eventlog"
)

type eventSink struct{ l *eventlog.Log }

func (e eventSink) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\r\n")
	switch Level(msg) {
	case "warn":
		return len(p), e.l.Warning(1, msg)
	case "error":
		return len(p), e.l.Error(1, msg)
	}
	return len(p), e.l.Info(1, msg)
}
func (e eventSink) Close() error { return e.l.Close() }

// openSink is the Application log under source "atrium". Registering the source needs admin: without it, the file
// still has everything.
func openSink() io.WriteCloser {
	_ = eventlog.InstallAsEventCreate("atrium", eventlog.Info|eventlog.Warning|eventlog.Error)
	l, err := eventlog.Open("atrium")
	if err != nil {
		return nil
	}
	return eventSink{l}
}
