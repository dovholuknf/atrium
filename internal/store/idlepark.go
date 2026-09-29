package store

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SettingIdleParkAfter is how long a subject card sits idle before it is parked.
// Whole seconds, or `off`. Empty means the default. See
// docs/keepalive-policy-design.md section 7.
const SettingIdleParkAfter = "idle_park_after"

const (
	// DefaultIdleParkAfter is two hours.
	DefaultIdleParkAfter = 2 * time.Hour
	// MinIdleParkAfter is the floor, so a mistyped value cannot park a card
	// between two of its own turns.
	MinIdleParkAfter = 30 * time.Minute
)

// ParseIdleParkAfter reads a stored value: the duration and whether idle parking
// is on at all. Empty is the default, `off` is off, a number under the floor is
// raised to it, and anything unreadable is the default rather than an error, since
// the daemon must keep running on whatever is stored.
func ParseIdleParkAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "":
		return DefaultIdleParkAfter, true
	case "off":
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return DefaultIdleParkAfter, true
	}
	d := time.Duration(n) * time.Second
	if d < MinIdleParkAfter {
		d = MinIdleParkAfter
	}
	return d, true
}

// IdleParkAfter reads the setting.
func (s *Store) IdleParkAfter() (time.Duration, bool) {
	v, err := s.Setting(SettingIdleParkAfter)
	if err != nil {
		return DefaultIdleParkAfter, true
	}
	return ParseIdleParkAfter(v)
}

// CheckIdleParkAfter validates a typed value for the settings page. Empty, `off`,
// or a number of seconds, and a number under the floor is refused rather than
// quietly raised, so the box never shows a value that is not in force.
func CheckIdleParkAfter(v string) (string, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" || v == "off" {
		return v, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("idle_park_after is a number of seconds, or off: %q", v)
	}
	if time.Duration(n)*time.Second < MinIdleParkAfter {
		return "", fmt.Errorf("idle_park_after has a floor of %d seconds, so a card is not parked between two of its "+
			"own turns: %q", int(MinIdleParkAfter/time.Second), v)
	}
	return v, nil
}

// SettingParkHandoffPrefix keys the handoff a parked card took, by card id. The
// value is `file|idle seconds`. Read and cleared when the card is woken, so its
// wake prompt can say what to read.
const SettingParkHandoffPrefix = "park_handoff:"
