// Package zitictx is the one place atrium builds an OpenZiti context.
//
// A ziti context starts goroutines that live until it is closed: the api session refresh, the posture cache, the
// router channels. One that is dropped without a Close lives for the life of the process, so every context is counted
// here, logged with who asked for it, and closed at most once.
package zitictx

import (
	"errors"
	"log"
	"sync"
	"sync/atomic"

	"github.com/openziti/sdk-golang/ziti"
)

var live atomic.Int64

// Live is how many contexts built here have not been closed.
func Live() int { return int(live.Load()) }

type counted struct {
	ziti.Context
	once sync.Once
	who  string
}

// Close closes the context once and drops it from the count.
func (c *counted) Close() {
	c.once.Do(func() {
		c.Context.Close()
		log.Printf("[ziti] context closed (%s), %d live", c.who, live.Add(-1))
	})
}

// MaxLive is how many contexts may be open at once. A room needs one per ziti rung and one per overlay it serves, so a
// count past this is a leak, and each leaked context retries its login against the controller forever.
const MaxLive = 16

// ErrTooMany is what New answers when MaxLive contexts are already open.
var ErrTooMany = errors.New("too many open ziti contexts. one is being built and never closed")

// New builds a context for `who` ("link", "capabilities", "native") and counts it.
func New(who string, cfg *ziti.Config) (ziti.Context, error) {
	if n := live.Add(1); n > MaxLive {
		live.Add(-1)
		log.Printf("[ziti] refusing a context (%s): %d are already live", who, n-1)
		return nil, ErrTooMany
	}
	c, err := ziti.NewContext(cfg)
	if err != nil {
		live.Add(-1)
		return nil, err
	}
	log.Printf("[ziti] context built (%s), %d live", who, live.Load())
	return &counted{Context: c, who: who}, nil
}
