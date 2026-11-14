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
// docs/terminal/input-lag-logging.md for how to read the lines.
package inputlag

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Env is the variable that turns the logging on.
const Env = "ATRIUM_DEBUG_INPUTLAG"

// defaultThreshold is what a bare "1" or "on" means. A keystroke echo a person
// notices is somewhere past 50ms, so 20ms per hop leaves room to see which one
// is eating the budget before it adds up.
const defaultThreshold = 20 * time.Millisecond

// Switch is the logging switch. The threshold is read on every keystroke hop and written by a settings save on
// another goroutine, so it is atomic. Nanoseconds, zero meaning off. `pinned` is whether the variable was set at
// start, which is what makes it an override rather than a default. A test builds its own.
type Switch struct {
	threshold atomic.Int64
	pinned    bool
}

// std is the process's switch, read from the variable once at start.
var std = newSwitch(os.Getenv(Env))

func newSwitch(env string) *Switch {
	sw := &Switch{pinned: strings.TrimSpace(env) != ""}
	sw.threshold.Store(int64(parse(env)))
	return sw
}

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
func On() bool { return std.On() }

// On reports whether this switch is on.
func (sw *Switch) On() bool { return sw.threshold.Load() > 0 }

// Over reports whether a hop took long enough to log. Always false when off.
func Over(d time.Duration) bool { return std.Over(d) }

// Over reports whether a hop took long enough to log on this switch. Always false when off.
func (sw *Switch) Over(d time.Duration) bool {
	t := sw.threshold.Load()
	return t > 0 && int64(d) >= t
}

// Pinned reports whether the environment variable decides, so a settings
// screen can say why its checkbox changes nothing.
func Pinned() bool { return std.pinned }

// SetLive switches the logging from a setting, at the default threshold. It
// does nothing when the variable is set, and reports whether it took. A change
// is written to the log, so the lines that follow it have a start.
func SetLive(on bool) bool { return std.SetLive(on) }

// SetLive is the package SetLive for this switch.
func (sw *Switch) SetLive(on bool) bool {
	if sw.pinned {
		return false
	}
	var t int64
	if on {
		t = int64(defaultThreshold)
	}
	if sw.threshold.Swap(t) != t {
		if on {
			log.Printf("[inputlag] on from settings, logging any hop over %s", Ms(defaultThreshold))
		} else {
			log.Printf("[inputlag] off from settings")
		}
	}
	return true
}

// perSecond is how many lines a second may be written. A stalled machine pushes every echo over the threshold, and a
// line per frame is what filled hub.err.
const perSecond = 10

// limiter lets perSecond lines through each second and counts the rest, so the next second's first line says how many
// were held back.
type limiter struct {
	mu      sync.Mutex
	second  int64
	written int
	dropped int
}

// allow reports whether a line may be written at now, and how many lines the last busy second held back.
func (l *limiter) allow(now time.Time) (ok bool, held int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if s := now.Unix(); s != l.second {
		l.second, l.written = s, 0
		held, l.dropped = l.dropped, 0
	}
	if l.written >= perSecond {
		l.dropped++
		return false, held
	}
	l.written++
	return true, held
}

var lines limiter

// Logf writes one line under a fixed prefix, so the lines grep out of a busy
// log in one pass. The clock is to the millisecond because the log's own is to
// the second, and the board's console lines carry the same shape to match.
// At most perSecond lines a second are written.
func Logf(format string, args ...any) {
	now := time.Now()
	ok, held := lines.allow(now)
	if held > 0 {
		log.Printf("[inputlag] %s %d lines held back by the rate limit", now.Format("15:04:05.000"), held)
	}
	if ok {
		log.Printf("[inputlag] "+now.Format("15:04:05.000")+" "+format, args...)
	}
}

// Ms renders a duration as milliseconds with one decimal, the unit every line
// is compared in.
func Ms(d time.Duration) string {
	return strconv.FormatFloat(float64(d)/float64(time.Millisecond), 'f', 1, 64) + "ms"
}
