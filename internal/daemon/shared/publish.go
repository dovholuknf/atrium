// Package shared holds what the daemon's feature packages need from the room without importing it. It imports nothing
// from the daemon.
package shared

// PublishTask re-reads one card and pushes it to every board listening.
type PublishTask func(id string)
