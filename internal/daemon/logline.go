package daemon

import (
	"fmt"
	"strings"
)

// logLine prints v and drops the CR and LF a caller could use to forge a second log line.
func logLine(v any) string {
	return strings.ReplaceAll(strings.ReplaceAll(fmt.Sprint(v), "\n", ""), "\r", "")
}
