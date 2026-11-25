package dictate

import (
	"log"
	"strconv"
	"sync/atomic"
	"time"
)

// LastTrip is the header the board sends with the previous clip's round trip in ms, so the log shows network time.
const LastTrip = "X-Atrium-Last-Trip"

var seenClip atomic.Bool

// LogClip writes the one line per clip. It takes sizes and times only, never the audio or the text.
func LogClip(where string, size int, read, whisper, total time.Duration, lastTrip string, firstWhisper bool) {
	trip := "-"
	if n, err := strconv.Atoi(lastTrip); err == nil && n >= 0 {
		trip = strconv.Itoa(n)
	}
	log.Printf("[dictate] %s clip bytes=%d read_ms=%d whisper_ms=%d total_ms=%d last_trip_ms=%s first_since_start=%t first_since_whisper=%t",
		where, size, read.Milliseconds(), whisper.Milliseconds(), total.Milliseconds(), trip, !seenClip.Swap(true), firstWhisper)
}

// FirstClip is true once per server start, and again after the server is restarted.
func (e *Engine) FirstClip() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	first := !e.clipped
	e.clipped = true
	return first
}
