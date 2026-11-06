package shared

import "time"

// DeliveredSay is one delivery a card remembers across a new context.
type DeliveredSay struct {
	From, Text, Kind string
	// Turn is the turn it went into: the one running, or the one it starts when the card was between turns.
	Turn int
	At   time.Time
}
