// Package inputlag is the switch for terminal input-lag logging on the hub and
// the room.
//
// OFF UNLESS ASKED FOR. The keystroke path is the hottest one in atrium and the
// point of this is to find what slows it, so the default costs one atomic load
// and logs nothing. Turned on from the gear ("log terminal input lag"), which
// reaches the hub and the room as a setting and takes effect at once, or with
// an environment variable, read once at start:
//
//	ATRIUM_DEBUG_INPUTLAG=1     log any hop slower than 20ms
//	ATRIUM_DEBUG_INPUTLAG=5     log any hop slower than 5ms
//
// THE VARIABLE WINS. Set to anything, it pins the logging for the life of the
// process and the setting is ignored, so a process started with a 5ms threshold
// to chase one hop is not reset by somebody clicking a checkbox.
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
	"sync/atomic"
	"time"
)

// Env is the variable that turns the logging on.
const Env = "ATRIUM_DEBUG_INPUTLAG"

// defaultThreshold is what a bare "1" or "on" means. A keystroke echo a person
// notices is somewhere past 50ms, so 20ms per hop leaves room to see which one
// is eating the budget before it adds up.
const defaultThreshold = 20 * time.Millisecond

// threshold is read on every keystroke hop and written by a settings save on
// another goroutine, so it is atomic. Nanoseconds, zero meaning off.
var threshold atomic.Int64

// pinned is whether the variable was set at start, which is what makes it an
// override rather than a default.
var pinned = strings.TrimSpace(os.Getenv(Env)) != ""

func init() { threshold.Store(int64(parse(os.Getenv(Env)))) }

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
func On() bool { return threshold.Load() > 0 }

// Over reports whether a hop took long enough to log. Always false when off.
func Over(d time.Duration) bool {
	t := threshold.Load()
	return t > 0 && int64(d) >= t
}

// Pinned reports whether the environment variable decides, so a settings
// screen can say why its checkbox changes nothing.
func Pinned() bool { return pinned }

// SetLive switches the logging from a setting, at the default threshold. It
// does nothing when the variable is set, and reports whether it took. A change
// is written to the log, so the lines that follow it have a start.
func SetLive(on bool) bool {
	if pinned {
		return false
	}
	var t int64
	if on {
		t = int64(defaultThreshold)
	}
	if threshold.Swap(t) != t {
		if on {
			log.Printf("[inputlag] on from settings, logging any hop over %s", Ms(defaultThreshold))
		} else {
			log.Printf("[inputlag] off from settings")
		}
	}
	return true
}

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
