// Package logline makes a value safe to print inside one log line.
package logline

import (
	"fmt"
	"strings"
)

// S prints v and drops the CR and LF a caller could use to forge a second log line.
func S(v any) string {
	return strings.ReplaceAll(strings.ReplaceAll(fmt.Sprint(v), "\n", ""), "\r", "")
}
