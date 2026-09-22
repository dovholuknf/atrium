// Package inputlag is the switch for terminal input-lag logging on the hub and
// the room.
//
// OFF UNLESS ASKED FOR. The keystroke path is the hottest one in atrium and the
// point of this is to find what slows it, so the default costs one integer
// compare and logs nothing. Turned on with an environment variable, read once
// at start:
//
//	ATRIUM_DEBUG_INPUTLAG=1     log any hop slower than 20ms
//	ATRIUM_DEBUG_INPUTLAG=5     log any hop slower than 5ms
//
// Only a hop OVER the threshold is logged, so a healthy session stays quiet
// and a slow one names the hop that added the delay. See
// docs/input-lag-logging.md for how to read the lines.
package inputlag

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env is the variable that turns the logging on.
const Env = "ATRIUM_DEBUG_INPUTLAG"

// defaultThreshold is what a bare "1" or "on" means. A keystroke echo a person
// notices is somewhere past 50ms, so 20ms per hop leaves room to see which one
// is eating the budget before it adds up.
const defaultThreshold = 20 * time.Millisecond

var threshold = parse(os.Getenv(Env))

// parse turns the variable into a threshold, zero meaning off.
//
// A number is milliseconds. "1" is the exception and means the default, since
// a 1ms threshold would log every frame and nobody setting a flag to 1 means
// that.
func parse(v string) time.Duration {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "", "0", "off", "false", "no":
		return 0
	case "1", "on", "true", "yes":
		return defaultThreshold
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultThreshold
	}
	return time.Duration(n) * time.Millisecond
}

// On reports whether the logging is switched on.
func On() bool { return threshold > 0 }

// Over reports whether a hop took long enough to log. Always false when off.
func Over(d time.Duration) bool { return threshold > 0 && d >= threshold }

// Logf writes one line under a fixed prefix, so the lines grep out of a busy
// log in one pass. The clock is to the millisecond because the log's own is to
// the second, and the board's console lines carry the same shape to match.
func Logf(format string, args ...any) {
	log.Printf("[inputlag] "+time.Now().Format("15:04:05.000")+" "+format, args...)
}

// Ms renders a duration as milliseconds with one decimal, the unit every line
// is compared in.
func Ms(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64) + "ms"
}
