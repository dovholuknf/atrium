package ptyhost

import "sync"

// closeOnce is how every handle in this package closes: a pty, a connection, a listener, a client. ONE function
// behind a sync.Once, so a second path to the same handle gets the first path's answer and does nothing.
//
// It is not tidiness. Closing a Windows handle twice is not a Go panic: the runtime may already have handed the
// number to something else, and the second close takes that down instead ("GetQueuedCompletionStatusEx failed
// (errno=735), fatal error: netpoll failed" in the spike, the whole host gone with every runner). A pty, a pipe and
// a client each have two ways to be closed here (the owner, and the failure of the thing that noticed).
type closeOnce struct {
	once sync.Once
	fn   func() error
	err  error
}

func newCloser(fn func() error) *closeOnce { return &closeOnce{fn: fn} }

// Close runs the close once. A caller that loses the race waits for the winner and returns its result.
func (c *closeOnce) Close() error {
	c.once.Do(func() { c.err = c.fn() })
	return c.err
}
