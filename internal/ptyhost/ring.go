package ptyhost

// ring keeps the last `cap` bytes a pty produced, the absolute offset of the next byte, and the size cuts.
//
// It is NOT the daemon's ringBuffer. That one lives in package daemon, is unexported, and carries the width
// bookkeeping of a screen model, which the host must not know. Sharing it meant touching internal/daemon, which
// this stage may not do. This is the small part the host needs: bytes, offsets, cuts. The daemon's ReplayCuts
// shape is kept, `(offset, cols, rows)`, so the daemon can feed both into the same screen model.
//
// Offsets are absolute and never reset. `total` is the offset of the next byte and the bytes retained are
// `[total-len, total)`, at most `cap` of them. Not safe for concurrent use: the owner holds its lock.
type ring struct {
	buf   []byte
	cap   int
	total int64
	cuts  []Cut
}

func newRing(cap int, cols, rows int) *ring {
	return &ring{cap: cap, cuts: []Cut{{Off: 0, Cols: cols, Rows: rows}}}
}

// retained is how many bytes a reader could still ask for.
func (r *ring) retained() int { return min(len(r.buf), r.cap) }

// start is the absolute offset of the oldest retained byte.
func (r *ring) start() int64 { return r.total - int64(r.retained()) }

// write appends p. The backing slice is allowed to run a quarter over `cap` so a full ring does not copy on every
// write, and the window the ring reports is still exactly the last `cap` bytes.
func (r *ring) write(p []byte) {
	r.buf = append(r.buf, p...)
	r.total += int64(len(p))
	if slack := max(r.cap/4, 4096); len(r.buf) > r.cap+slack {
		r.buf = append([]byte(nil), r.buf[len(r.buf)-r.cap:]...)
	}
	r.forgetCuts()
}

// forgetCuts drops cuts wholly behind the window, keeping the last one at or before its start, which says what
// size the oldest retained byte was written at.
func (r *ring) forgetCuts() {
	st := r.start()
	keep := 0
	for i, c := range r.cuts {
		if c.Off <= st {
			keep = i
		}
	}
	if keep > 0 {
		r.cuts = append([]Cut(nil), r.cuts[keep:]...)
	}
}

// size is the size the terminal is at now.
func (r *ring) size() (cols, rows int) {
	c := r.cuts[len(r.cuts)-1]
	return c.Cols, c.Rows
}

// cut records a size change at the current offset. The caller records it BEFORE applying the resize, so a byte
// produced at the new size can never sit ahead of its cut.
func (r *ring) cut(cols, rows int) Cut {
	c := Cut{Off: r.total, Cols: cols, Rows: rows}
	// A cut at the same offset as the last one replaces it: no byte was written between the two sizes.
	if n := len(r.cuts); n > 0 && r.cuts[n-1].Off == c.Off {
		r.cuts[n-1] = c
	} else {
		r.cuts = append(r.cuts, c)
	}
	return c
}

// snapshot returns the bytes from `from`, the offset they really start at, whether `from` was older than the ring
// (the replay case: the caller gets everything retained), and the cuts in force from there. The first cut is
// rebased to the returned offset, so a reader replaying the bytes knows the width the first one was written at.
func (r *ring) snapshot(from int64) (data []byte, eff int64, truncated bool, cuts []Cut) {
	eff = from
	if eff < 0 {
		eff = 0
	}
	if st := r.start(); eff < st {
		eff, truncated = st, true
	}
	if eff > r.total {
		eff = r.total
	}
	data = append([]byte(nil), r.buf[len(r.buf)-int(r.total-eff):]...)
	for i, c := range r.cuts {
		if c.Off <= eff {
			cuts = []Cut{{Off: eff, Cols: c.Cols, Rows: c.Rows}}
			continue
		}
		cuts = append(cuts, r.cuts[i])
	}
	return data, eff, truncated, cuts
}
