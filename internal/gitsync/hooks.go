package gitsync

import "time"

// hubHooks are the seams a test sets on one Hub. A zero value is production: nothing is hooked and the limits take
// their defaults. They are fields and not package variables, so a test's setting cannot reach another test's hub.
type hubHooks struct {
	// maxPush is the most a push may carry, defaultMaxPush when zero. The same figure is receive.maxInputSize.
	maxPush int64
	// ownerAskTotal is how long one push may spend asking owners' rooms, defaultOwnerAskTotal when zero.
	ownerAskTotal time.Duration
	// inStoreWideLock, when set, runs inside the store-wide lock after the case check. Tests widen the window with it.
	inStoreWideLock func()
	// inNameCheck, when set, runs before each `git check-ref-format`. Tests use it to see whether the repository's
	// lock is held at that moment.
	inNameCheck func()
	// onContains is told of each repository whose branches are searched, which is only one that has the object.
	onContains func(dir string)
	// config is further git config for a push, after the hub's own.
	config [][2]string
}

const (
	defaultMaxPush       int64 = 500 << 20
	defaultOwnerAskTotal       = 10 * time.Second
)

func (h *Hub) pushLimit() int64 {
	if h.hk.maxPush > 0 {
		return h.hk.maxPush
	}
	return defaultMaxPush
}

func (h *Hub) ownerAskLimit() time.Duration {
	if h.hk.ownerAskTotal > 0 {
		return h.hk.ownerAskTotal
	}
	return defaultOwnerAskTotal
}
