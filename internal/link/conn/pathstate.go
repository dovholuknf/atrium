package conn

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// Per path state: what a room can say about each way it has to reach its hub, and what a hub can say about each way it
// listens. See docs/fabric/multi-transport-design.md, sections 4 and 5.
//
// A ROOM'S VIEW IS THE ONLY VIEW THAT MATTERS. A rung is `down` as the room sees it, so the room probes and reports in
// its beat, which a hub that cannot dial the room's rung still hears. It is observed, never acted on: nothing here moves
// a room or refuses one.

// Path states. `on` is the control connection, `up` a probe that answered, `down` one that did not, `none` a kind this
// room has no rung for (no ziti identity), `unlistened` a kind the hub is not listening on.
const (
	PathOn         = "on"
	PathUp         = "up"
	PathDown       = "down"
	PathNone       = "none"
	PathUnlistened = "unlistened"
	// PathOff is a kind somebody turned off on purpose. Never dialled, never probed.
	PathOff = "off"
)

// PathKinds is the fixed order the board draws a room's paths in.
var PathKinds = []string{"direct", "ziti", "zrok"}

// maxPathStates bounds what a hub keeps from a beat. A room is a trusted peer, and still this is read off a socket.
const maxPathStates = 12

// pather is a Dialer that can say what every rung is doing. `Ladder` is the one there is.
type pather interface {
	ProbePaths(ctx context.Context, wait time.Duration) []PathState
}

// ProbePaths dials every rung, together, and says what each did. The rung it stands on stays `on`: the control
// connection is the proof, and its dial only measures a connect time. A kind with no rung at all comes back `none`,
// last, in PathKinds order. It closes what it opens and never moves the ladder.
//
// A PROBE IS BOUNDED BY MORE THAN A TIMEOUT. A ziti Dial ignores its context, so a rung whose last probe has not
// returned is skipped this round and reports its previous round trip marked stale. At most one probe dial is ever in
// flight per rung. A probe is observation: it takes no data dial slot and never reaches dataFailed.
func (l *Ladder) ProbePaths(ctx context.Context, wait time.Duration) []PathState {
	rungs := l.Rungs()
	r, _, has := l.current()
	out := make([]PathState, len(rungs))
	var wg sync.WaitGroup
	for i, rg := range rungs {
		out[i] = PathState{Kind: rg.Path.Transport, Addr: pathAddr(rg.Path), Source: rg.Path.From}
		if m, ok := rg.Dial.(interface{ Mutual() bool }); ok {
			out[i].Mutual = m.Mutual()
		}
		if l.IsOff(rg.Path.Transport) {
			out[i].State = PathOff
			continue
		}
		on := has && samePath(rg.Path, r.Path)
		if on {
			out[i].State = PathOn
		}
		wg.Add(1)
		go func(i int, rg Rung, on bool) {
			defer wg.Done()
			rtt, err, skipped := l.probeRung(ctx, rg, wait)
			switch {
			case skipped:
				if on {
					out[i].RTTMs, out[i].Stale = l.lastRTT(rg), true
				} else {
					out[i].State, out[i].RTTMs, out[i].Stale = PathDown, l.lastRTT(rg), true
					out[i].LastError = "the last probe has not returned"
				}
			case err != nil:
				if !on {
					out[i].State, out[i].LastError = PathDown, err.Error()
				}
			default:
				out[i].RTTMs = rtt
				if !on {
					out[i].State = PathUp
				}
			}
		}(i, rg, on)
	}
	wg.Wait()
	for _, k := range PathKinds {
		have := false
		for _, p := range out {
			have = have || p.Kind == k
		}
		if !have {
			out = append(out, PathState{Kind: k, State: PathNone})
		}
	}
	return out
}

func probeKey(rg Rung) string { return rg.Path.Transport + "|" + pathAddr(rg.Path) }

func (l *Ladder) lastRTT(rg Rung) int64 {
	l.pmu.Lock()
	defer l.pmu.Unlock()
	return l.rtts[probeKey(rg)]
}

// probeRung makes one bounded dial. skipped is true when the previous dial of this rung is still running.
func (l *Ladder) probeRung(ctx context.Context, rg Rung, wait time.Duration) (rtt int64, err error, skipped bool) {
	key := probeKey(rg)
	l.pmu.Lock()
	if l.probing[key] {
		l.pmu.Unlock()
		return 0, nil, true
	}
	if l.probing == nil {
		l.probing, l.rtts = map[string]bool{}, map[string]int64{}
	}
	l.probing[key] = true
	l.pmu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	t0 := time.Now()
	go func() {
		c, err := rg.Dial.Dial(ctx)
		l.pmu.Lock()
		delete(l.probing, key)
		l.pmu.Unlock()
		ch <- res{c, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return 0, r.err, false
		}
		_ = r.c.Close()
		rtt = time.Since(t0).Milliseconds()
		l.pmu.Lock()
		l.rtts[key] = rtt
		l.pmu.Unlock()
		return rtt, nil, false
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.err == nil && r.c != nil {
				_ = r.c.Close()
			}
		}()
		return 0, errors.New("no answer within " + wait.String()), false
	}
}

func pathAddr(p Path) string {
	if p.Transport == "zrok" && p.Share != "" {
		return "share " + wire.MaskShare(p.Share)
	}
	if p.Addr != "" {
		return p.Addr
	}
	return p.Service
}

// stampSince carries each path's Since forward from `prev` while its state is unchanged, and starts it at `now` when it
// changed or is new, so the board can say how long a path has been as it is.
func stampSince(prev, cur []PathState, now time.Time) []PathState {
	key := func(p PathState) string { return p.Kind + "|" + p.Addr }
	was := map[string]PathState{}
	for _, p := range prev {
		was[key(p)] = p
	}
	for i := range cur {
		if p, ok := was[key(cur[i])]; ok && p.State == cur[i].State {
			cur[i].Since = p.Since
		} else {
			cur[i].Since = now
		}
	}
	return cur
}

// cleanPaths is what a hub keeps of a beat's paths: bounded in count and length, unknown kinds and states dropped.
func cleanPaths(in []PathState) []PathState {
	var out []PathState
	for _, p := range in {
		if len(out) == maxPathStates {
			break
		}
		switch p.State {
		case PathOn, PathUp, PathDown, PathNone, PathOff:
		default:
			continue
		}
		if !pathKnownKind(p.Kind) {
			continue
		}
		p.Addr, p.LastError, p.Source = pathClip(p.Addr, 200), pathClip(p.LastError, 300), pathClip(p.Source, 20)
		out = append(out, p)
	}
	return out
}

func pathKnownKind(k string) bool {
	for _, x := range PathKinds {
		if x == k {
			return true
		}
	}
	return false
}

func pathClip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ListenerState is one of the hub's own sides: whether it is accepting rooms on a transport.
type ListenerState struct {
	Kind      string    `json:"kind"`
	Up        bool      `json:"up"`
	Since     time.Time `json:"since,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

// listenerSet is the hub's listeners, safe to use at its zero value.
type listenerSet struct {
	mu sync.Mutex
	by map[string]ListenerState
}

// SetListener records a side coming up (err nil) or being down. Since moves only when up flips.
func (h *Hub) SetListener(kind string, up bool, err error) {
	h.lst.mu.Lock()
	defer h.lst.mu.Unlock()
	if h.lst.by == nil {
		h.lst.by = map[string]ListenerState{}
	}
	s, ok := h.lst.by[kind]
	if !ok || s.Up != up {
		s.Since = time.Now()
	}
	s.Kind, s.Up, s.LastError = kind, up, ""
	if err != nil {
		s.LastError = pathClip(err.Error(), 300)
	}
	h.lst.by[kind] = s
}

// Listeners is the hub's sides in PathKinds order, then any other kind by name. Empty for a hub that records none.
func (h *Hub) Listeners() []ListenerState {
	h.lst.mu.Lock()
	defer h.lst.mu.Unlock()
	out := make([]ListenerState, 0, len(h.lst.by))
	for _, s := range h.lst.by {
		out = append(out, s)
	}
	rank := func(k string) int {
		for i, x := range PathKinds {
			if x == k {
				return i
			}
		}
		return len(PathKinds)
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := rank(out[i].Kind), rank(out[j].Kind); ri != rj {
			return ri < rj
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// MergePaths lays the hub's listeners over a room's paths. A kind the hub is not listening on, and the room is not on,
// reads `unlistened` with the reason, which is a different thing from `none`: there the room has no way to dial, here
// there is nothing to dial. With no listeners recorded the room's paths come back untouched, so an older hub wiring
// says nothing it does not know.
func MergePaths(paths []PathState, ls []ListenerState) []PathState {
	if len(ls) == 0 || len(paths) == 0 {
		return paths
	}
	up := map[string]bool{}
	why := map[string]string{}
	for _, l := range ls {
		up[l.Kind] = l.Up
		why[l.Kind] = l.LastError
	}
	out := make([]PathState, len(paths))
	for i, p := range paths {
		if p.State != PathOn && p.State != PathOff && !up[p.Kind] {
			p.State = PathUnlistened
			p.LastError = strings.TrimSpace(why[p.Kind])
			if p.LastError == "" {
				p.LastError = "the hub is not listening on " + p.Kind
			}
		}
		out[i] = p
	}
	return out
}

// pathsNow is the last probe, for the beat to carry. Nil before the first.
func (r *Room) pathsNow() []PathState {
	r.pmu.Lock()
	defer r.pmu.Unlock()
	out := append([]PathState(nil), r.paths...)
	// Said now, not at the last probe: a switch has to show on the row at once.
	if l, ok := r.Dial.(offer); ok {
		hot := l.Hammering()
		for i := range out {
			switch {
			case l.IsOff(out[i].Kind):
				out[i].State, out[i].Hammering = PathOff, false
			default:
				out[i].Hammering = hot[out[i].Kind]
				if out[i].State == PathOff {
					out[i].State = PathDown
				}
			}
		}
	}
	return out
}

// watchPaths probes every rung once at once and then every PathsEvery, until the attachment ends. Each probe is bounded
// by DialWait, and the next waits for it, so a black holed rung never stacks dials. Best effort: a probe that fails
// says `down` and nothing else happens.
func (r *Room) watchPaths(ctx context.Context, p pather) {
	every := r.T.PathsEvery
	if every <= 0 {
		every = DefaultTimings().PathsEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		cur := p.ProbePaths(ctx, r.T.DialWait)
		if ctx.Err() != nil {
			return
		}
		r.pmu.Lock()
		r.paths = stampSince(r.paths, cur, time.Now())
		r.pmu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
