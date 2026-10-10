package gitsync

import (
	"io"
)

// A real git, against the store's handler, over a temp store. The listener is stood in for by a test header
// that picks the Caller, because the listener is what decides who a request is from in the real hub (see
// internal/link). The card headers are the real ones.

// ── the happy path ──────────────────────────────────────

// ── what the user's git sees ────────────────────────────

// ── main ────────────────────────────────────────────────

// ── ownership ───────────────────────────────────────────

// ── release ─────────────────────────────────────────────

// ── names and case ──────────────────────────────────────

// ── a whole push, or none of it ─────────────────────────

// ── a repository the hub lacks ──────────────────────────

// ── size ────────────────────────────────────────────────

// ── who is pushing ──────────────────────────────────────

// ── what is served ──────────────────────────────────────

// ── the log ─────────────────────────────────────────────

// ── racing ──────────────────────────────────────────────

// ── the board's listing ─────────────────────────────────

// ── what the mutation checks found missing ──────────────

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// ── what the second review found ────────────────────────
