package gitsync

import (
	"context"
	"io"
)

// ── deadlines: a stall frees the slot, progress is never cut ──────────────────────────────────

// endless is a body that is always ready, so a reader that does not read fills every buffer between here and it.
type endless struct{}

func (endless) Read(b []byte) (int, error) { return len(b), nil }
func (endless) Close() error               { return nil }

// ctxBody sends some bytes and then blocks until the request is cancelled, like a room that went quiet mid-pack.
type ctxBody struct {
	ctx  context.Context
	sent bool
}

func (c *ctxBody) Read(b []byte) (int, error) {
	if !c.sent {
		c.sent = true
		return copy(b, "abc"), nil
	}
	<-c.ctx.Done()
	return 0, c.ctx.Err()
}
func (c *ctxBody) Close() error { return nil }

// ctxReader fails once its request is cancelled, as a transport's response body does.
type ctxReader struct {
	ctx context.Context
	rc  io.ReadCloser
}

func (c *ctxReader) Read(b []byte) (int, error) {
	n, err := c.rc.Read(b)
	if cerr := c.ctx.Err(); cerr != nil {
		return 0, cerr
	}
	return n, err
}
func (c *ctxReader) Close() error { return c.rc.Close() }
