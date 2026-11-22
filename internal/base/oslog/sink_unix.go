//go:build !windows

package oslog

import (
	"io"
	"log/syslog"
	"strings"
)

type sysSink struct{ w *syslog.Writer }

func (s sysSink) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	var err error
	switch Level(msg) {
	case "warn":
		err = s.w.Warning(msg)
	case "error":
		err = s.w.Err(msg)
	case "debug":
		err = s.w.Debug(msg)
	default:
		err = s.w.Info(msg)
	}
	return len(p), err
}

func (s sysSink) Close() error { return s.w.Close() }

// openSink is journald on Linux, which reads /dev/log, and the unified log on macOS, which takes syslog. Go reaches
// os_log, and so a subsystem, only through cgo, and releases build with CGO_ENABLED=0.
func openSink() io.WriteCloser {
	w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "atrium")
	if err != nil {
		return nil
	}
	return sysSink{w}
}
