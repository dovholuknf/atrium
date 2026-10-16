// Package zitictx is the one place atrium builds an OpenZiti context.
//
// A ziti context starts goroutines that live until it is closed: the api session refresh, the posture cache, the
// router channels. One that is dropped without a Close lives for the life of the process, so every context is counted
// here, logged with who asked for it, and closed at most once.
package zitictx

import (
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

// New builds a context for `who` ("link", "capabilities", "native") and counts it.
func New(who string, cfg *ziti.Config) (ziti.Context, error) {
	c, err := ziti.NewContext(cfg)
	if err != nil {
		return nil, err
	}
	log.Printf("[ziti] context built (%s), %d live", who, live.Add(1))
	return &counted{Context: c, who: who}, nil
}
