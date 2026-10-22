package link

import "context"

// Two lanes per room: the control connection rides its own session, so a beat never queues behind a git fetch, a
// relay or a download on the data lane. See docs/fabric/one-session-per-room.md.
//
// The lane is a mark on the context, set by the room's attach and read by a transport's Dial, so the Dialer interface
// keeps its one method and a transport that has one session simply ignores it.

type laneKey struct{}

// controlLane marks a dial as the control connection.
func controlLane(ctx context.Context) context.Context { return context.WithValue(ctx, laneKey{}, true) }

// onControlLane says whether a dial is the control connection.
func onControlLane(ctx context.Context) bool {
	v, _ := ctx.Value(laneKey{}).(bool)
	return v
}
