package conn

import (
	"bufio"
	"context"
	"log"
	"time"
)

// A ziti Dial takes no context, so a dial that timed out is still running. What bounds them is a count, kept on the
// Room for the life of the process and not per attachment, because a dial belongs to the process until it returns.

const (
	// maxDataDials is how many data dials may be in flight at once.
	maxDataDials = 8
	// dataRedialAfter is how many data dial failures in a row drop the control link, so the next attach dials afresh.
	dataRedialAfter = 5
)

// resetter is a Dialer that can throw away what it holds, a stale ziti context for one, before the link redials.
type resetter interface{ Reset() }

// dialSlot is one data dial in flight.
type dialSlot struct {
	started time.Time
	// late is set once the dial outlived its deadline and was counted as a failure, so it is counted once.
	late bool
}

// startDial takes a slot for a new data dial, or reports there is none. When every slot is taken and every one is past
// its deadline, that ask counts as a failure toward dataRedialAfter.
func (r *Room) startDial() (*dialSlot, bool) {
	r.mu.Lock()
	if len(r.dials) < maxDataDials {
		s := &dialSlot{started: time.Now()}
		if r.dials == nil {
			r.dials = map[*dialSlot]struct{}{}
		}
		r.dials[s] = struct{}{}
		r.mu.Unlock()
		return s, true
	}
	allLate := true
	for s := range r.dials {
		if time.Since(s.started) < r.T.DataDeadline {
			allLate = false
			break
		}
	}
	r.mu.Unlock()
	if allLate {
		r.dataFailed(errDialsStuck)
	}
	return nil, false
}

// endDial gives the slot back. Called when Dial really returns, whatever it returned.
func (r *Room) endDial(s *dialSlot) {
	r.mu.Lock()
	delete(r.dials, s)
	r.mu.Unlock()
}

// dialDeadline counts a dial that is still pending after the deadline as one failure. The dial keeps its slot.
func (r *Room) dialDeadline(s *dialSlot) {
	r.mu.Lock()
	_, pending := r.dials[s]
	first := pending && !s.late
	if first {
		s.late = true
	}
	r.mu.Unlock()
	if first {
		r.dataFailed(errDialsStuck)
	}
}

// dataDial is one data dial in its own goroutine, holding slot s.
func (r *Room) dataDial(ctx context.Context, session string, s *dialSlot) {
	t := time.AfterFunc(r.T.DataDeadline, func() { r.dialDeadline(s) })
	dialCtx, cancel := context.WithTimeout(ctx, r.T.DialWait)
	conn, err := r.Dial.Dial(dialCtx)
	cancel()
	t.Stop()
	r.mu.Lock()
	late := s.late
	r.mu.Unlock()
	r.endDial(s)
	r.payOwed(ctx, session)
	if err != nil {
		// One failed data dial is not a reason to tear the link down by itself. The hub will ask again, and the
		// control connection decides whether the link is alive. But it is said, and dataRedialAfter in a row drops it.
		// A dial already counted at its deadline is not counted twice.
		if !late {
			r.dataFailed(err)
		}
		return
	}
	r.dataWorked()
	// A dial that returned after its attachment ended carries a stale session. Close it.
	if ctx.Err() != nil || r.currentSession() != session {
		conn.Close()
		return
	}
	br := bufio.NewReader(conn)
	if _, err := sayHello(conn, br, hello{Kind: "data", Room: r.Name, Session: session}); err != nil {
		conn.Close()
		return
	}
	// THE BUFFERED READER GOES WITH IT. `sayHello` may have pulled the first bytes of the hub's HTTP request into the
	// buffer along with the welcome line, and handing over the bare conn would lose them.
	select {
	case r.conns <- &buffered{Conn: conn, br: br}:
	case <-ctx.Done():
		conn.Close()
	}
}

func (r *Room) currentSession() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.session
}

// redialLink drops the control connection of the current attachment, so Run backs off and attaches again with a fresh
// dial. A Dialer that can reset is told first.
func (r *Room) redialLink() {
	r.mu.Lock()
	drop := r.dropLink
	r.dataFails = 0
	r.mu.Unlock()
	if drop == nil {
		return
	}
	log.Printf("[link] %d data dials in a row failed or hung on hub %s, dropping the control link to redial",
		dataRedialAfter, r.Dial.Describe())
	if rs, ok := r.Dial.(resetter); ok {
		rs.Reset()
	}
	drop()
}

type stuckError struct{}

func (stuckError) Error() string { return "a data dial is still pending past its deadline" }

var errDialsStuck error = stuckError{}

// maxOwed bounds what is owed, so a hub asking in a loop cannot grow it without end.
const maxOwed = 64

// owe records asks that found every slot taken.
func (r *Room) owe(n int) {
	r.mu.Lock()
	r.owed += n
	if r.owed > maxOwed {
		r.owed = maxOwed
	}
	r.mu.Unlock()
}

// payOwed starts one owed dial in the slot that just came free. Each dial that ends pays one, so what is owed drains
// as fast as the cap allows. Nothing is paid once the attachment is over: its asks went with it.
func (r *Room) payOwed(ctx context.Context, session string) {
	if ctx.Err() != nil || r.currentSession() != session {
		r.mu.Lock()
		r.owed = 0
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	if r.owed == 0 {
		r.mu.Unlock()
		return
	}
	r.owed--
	r.mu.Unlock()
	s, ok := r.startDial()
	if !ok {
		r.owe(1)
		return
	}
	go r.dataDial(ctx, session, s)
}
