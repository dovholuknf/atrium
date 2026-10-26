//go:build integration

package conn

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// announceWait bounds each wait in these tests for something to arrive. Every
// wait is polled and returns as soon as its condition holds, so the bound only
// costs time when a test is failing. It was 5 and 10 seconds, and a loaded
// machine running three suites at once took longer than that to carry one
// announcement through the ceiling, a dial and a store write.
const announceWait = 30 * time.Second

const testAnnounceEvery = 200 * time.Millisecond

// roomState is a handler that answers `/v1/state` with whatever it is set to,
// and `/v1/events` with a stream that says something when told.
type roomState struct {
	mu    sync.Mutex
	cards []map[string]any
	// bump is closed and replaced to wake the event stream.
	bump chan struct{}
	// broken is what `/v1/state` answers instead, for the case a room cannot
	// read its own store. Empty means answer normally.
	broken string
	code   int
	// streams counts the event streams that have started listening. A change made before one is listening is
	// never seen by it, so a test that changes the state waits for one first.
	streams int
	refused int
}

func newRoomState(cards ...map[string]any) *roomState {
	return &roomState{cards: cards, bump: make(chan struct{})}
}

func (s *roomState) set(cards ...map[string]any) {
	s.mu.Lock()
	s.cards = cards
	old := s.bump
	s.bump = make(chan struct{})
	s.mu.Unlock()
	close(old)
}

// fail makes `/v1/state` answer with something that is not a card list, the way
// a room whose store has halted does.
func (s *roomState) fail(body string, code int) {
	s.mu.Lock()
	s.broken, s.code = body, code
	old := s.bump
	s.bump = make(chan struct{})
	s.mu.Unlock()
	close(old)
}

// mended waits for the room to refuse a read, then mends it and waits for an announcement after `sent`, which
// must come after anything the refused read sent.
func mended(t *testing.T, s *roomState, got *[]announcement, mu *sync.Mutex, sent int) {
	t.Helper()
	waitFor(t, announceWait, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.refused > 0
	})
	s.fail("", 0)
	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*got) > sent
	})
}

func (s *roomState) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/state":
		s.mu.Lock()
		cards, broken, code := s.cards, s.broken, s.code
		s.mu.Unlock()
		// The same shape the real endpoint answers with: none is an empty
		// list, never a null, because null is what "I could not tell you"
		// looks like on the wire.
		if cards == nil {
			cards = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		if broken != "" {
			if code == 0 {
				code = http.StatusInternalServerError
			}
			s.mu.Lock()
			s.refused++
			s.mu.Unlock()
			w.WriteHeader(code)
			_, _ = w.Write([]byte(broken))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"cards": cards})
	case "/v1/events":
		w.Header().Set("Content-Type", "text/event-stream")
		first := true
		for {
			s.mu.Lock()
			wait := s.bump
			if first {
				s.streams++ // taken with the channel, so a change after the count is a change this stream hears
				first = false
			}
			s.mu.Unlock()
			select {
			case <-r.Context().Done():
				return
			case <-wait:
			}
			if _, err := w.Write([]byte("event: tasks\ndata: {}\n\n")); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	default:
		http.NotFound(w, r)
	}
}

// caching stands up a hub that keeps what it is told, and a room attached to it.
func caching(t *testing.T, state http.Handler) (*Hub, *[]announcement, *sync.Mutex, func()) {
	return cachingWith(t, state, nil)
}

// cachingWith is `caching` with a chance to adjust the hub before it serves.
func cachingWith(t *testing.T, state http.Handler, tune func(*Hub)) (*Hub, *[]announcement, *sync.Mutex, func()) {
	t.Helper()
	// Most of these tests are waits, and each has a hub and a room of its own.
	parallel(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})

	var mu sync.Mutex
	var got []announcement
	hub.Cached = func(name string, cards []CardState) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, announcement{Room: name, Cards: cards})
		return nil
	}

	if tune != nil {
		tune(hub)
	}
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()

	room := &Room{
		Name: "testroom", Dial: plain{addr: ln.Addr().String()}, Handler: state, AnnounceEvery: testAnnounceEvery,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, announceWait, func() bool { return hub.Has("testroom") })
	// The room watches the event stream after its first announcement, and on a loaded machine that is later than
	// the test's first look. A change made before the watch is listening wakes nobody.
	if rs, ok := state.(*roomState); ok {
		waitFor(t, announceWait, func() bool {
			rs.mu.Lock()
			defer rs.mu.Unlock()
			return rs.streams > 0
		})
	}

	return hub, &got, &mu, func() { stop(); ln.Close() }
}

// ATTACHING IS ITSELF AN ANNOUNCEMENT, and it is the important one. A room that
// has just come back is one the hub may have been holding a stale picture of
// for hours, and that picture is replaced whole rather than reconciled.
func TestARoomSaysWhatItHoldsAsSoonAsItAttaches(t *testing.T) {
	state := newRoomState(
		map[string]any{"id": "one", "status": "running", "title": "the first"},
		map[string]any{"id": "two", "status": "done", "title": "the second"},
	)
	_, got, mu, stop := caching(t, state)
	defer stop()

	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*got) > 0
	})

	mu.Lock()
	first := (*got)[0]
	mu.Unlock()
	if first.Room != "testroom" {
		t.Fatalf("the announcement arrived as %q", first.Room)
	}
	if len(first.Cards) != 2 {
		t.Fatalf("it carried %d card(s), wanted 2", len(first.Cards))
	}
	if first.Cards[0].ID != "one" || first.Cards[0].Status != "running" {
		t.Fatalf("the first card arrived as %+v", first.Cards[0])
	}
	// THE PAYLOAD IS WHAT THE ROOM SENT, UNTOUCHED. Nothing here knows what a
	// card is made of, and a field list would be a second copy of the room's
	// schema drifting from it.
	var back map[string]any
	if err := json.Unmarshal(first.Cards[0].Payload, &back); err != nil {
		t.Fatal(err)
	}
	if back["title"] != "the first" {
		t.Fatalf("the card's own fields did not survive: %v", back)
	}
}

// A CHANGE IS PUSHED, NOT WAITED FOR. The room is the only thing that knows
// something changed, and a hub polling on a timer is either late or wasteful
// and is usually both.
func TestAChangeReachesTheHubWithoutAnybodyAsking(t *testing.T) {
	state := newRoomState(map[string]any{"id": "one", "status": "running"})
	_, got, mu, stop := caching(t, state)
	defer stop()

	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*got) > 0
	})

	state.set(
		map[string]any{"id": "one", "status": "done"},
		map[string]any{"id": "three", "status": "running"},
	)

	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		last := (*got)[len(*got)-1]
		return len(last.Cards) == 2
	})

	mu.Lock()
	last := (*got)[len(*got)-1]
	mu.Unlock()
	if last.Cards[0].Status != "done" {
		t.Fatalf("the change did not arrive: %+v", last.Cards[0])
	}
}

// AN ANNOUNCEMENT IDENTICAL TO THE LAST ONE IS NOT SENT.
//
// This is what keeps a noisy room from being a noisy database. Activity,
// output and telemetry all publish events and none of them are cached, so most
// of what wakes the announcer up produces exactly the same answer. Without this
// a working agent would rewrite the hub's cache every couple of seconds for the
// whole time it worked.
func TestNothingIsSentWhenNothingChanged(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real link, hub and git rig over loopback")
	}
	lightSlot(t)
	state := newRoomState(map[string]any{"id": "one", "status": "running"})
	_, got, mu, stop := caching(t, state)
	defer stop()

	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*got) > 0
	})
	mu.Lock()
	after := len(*got)
	mu.Unlock()

	// Ten events, no change behind any of them, which is what a busy agent
	// looks like to this.
	for i := 0; i < 10; i++ {
		state.set(map[string]any{"id": "one", "status": "running"})
		time.Sleep(20 * time.Millisecond) // wait: events spread over time are what is under test
	}
	// A real change, announced only after anything the ten events sent.
	state.set(map[string]any{"id": "one", "status": "done"})
	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		last := (*got)[len(*got)-1]
		return len(last.Cards) == 1 && last.Cards[0].Status == "done"
	})

	mu.Lock()
	now := len(*got)
	mu.Unlock()
	if now != after+1 {
		t.Fatalf("%d announcement(s) were sent for ten events that changed nothing",
			now-after-1)
	}
}

// A ROOM THAT CANNOT READ ITS OWN STATE MUST NOT ANNOUNCE THAT IT HAS NONE.
//
// The hub takes an announcement whole, so an empty one means "everything you
// were holding for me is gone". A room whose store has halted answers with an
// error rather than a card list, and treating that as an empty list would have
// the room's own failure erase the record of its work on the hub: the one place
// it was still visible.
//
// Found by review, and it is one `if` between a cache and a wipe.
func TestARoomInTroubleDoesNotAnnounceItselfEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real link, hub and git rig over loopback")
	}
	lightSlot(t)
	state := newRoomState(map[string]any{"id": "one", "status": "running"})
	_, got, mu, stop := caching(t, state)
	defer stop()

	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*got) > 0
	})
	mu.Lock()
	sent := len(*got)
	mu.Unlock()

	// Now the room breaks, the way `s.fail` breaks: a status and an error
	// object, with no card list in it at all.
	state.fail(`{"error":"store is halted"}`, 500)
	mended(t, state, got, mu, sent)

	mu.Lock()
	defer mu.Unlock()
	if len(*got) != sent+1 || len((*got)[sent].Cards) != 1 {
		t.Fatalf("a room that could not read its own state announced anyway: %+v",
			(*got)[sent])
	}
}

// AND AN ANSWER WITH NO CARD LIST IN IT IS NOT AN EMPTY BOARD EITHER.
//
// The subtler half of the same failure: a 200 with a body that simply does not
// mention cards. A room holding nothing sends an empty list. A room that failed
// sends no list. Those mean opposite things and only one of them may reach the
// hub.
func TestAnAnswerWithNoCardListIsNotAnEmptyRoom(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real link, hub and git rig over loopback")
	}
	lightSlot(t)
	state := newRoomState(map[string]any{"id": "one", "status": "running"})
	_, got, mu, stop := caching(t, state)
	defer stop()

	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*got) > 0
	})
	mu.Lock()
	sent := len(*got)
	mu.Unlock()

	state.fail(`{"ok":true}`, 200)
	mended(t, state, got, mu, sent)

	mu.Lock()
	defer mu.Unlock()
	if len(*got) != sent+1 || len((*got)[sent].Cards) != 1 {
		t.Fatalf("an answer carrying no card list was taken as a room with no cards: %+v",
			(*got)[sent])
	}
}

// A ROOM HOLDING NOTHING STILL SAYS SO, which is the case the two tests above
// must not have broken. An empty list is an answer and has to land, or a board
// somebody cleared would show the old cards until something else changed.
func TestARoomWithNoCardsAnnouncesAnEmptyList(t *testing.T) {
	state := newRoomState(map[string]any{"id": "one", "status": "running"})
	_, got, mu, stop := caching(t, state)
	defer stop()

	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(*got) > 0
	})

	state.set()

	waitFor(t, announceWait, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len((*got)[len(*got)-1].Cards) == 0
	})
}

// AN ANNOUNCEMENT THAT DID NOT LAND IS TRIED AGAIN, without waiting for
// something else to happen on the room.
//
// The one that matters is the first, which is what replaces a picture the hub
// may have been holding for hours. A room that announces on attaching, fails,
// and then sits quietly because nobody is working on it is exactly the room
// whose board somebody is looking at, and it would have shown yesterday.
func TestAnAnnouncementThatFailedIsSentAgain(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real link, hub and git rig over loopback")
	}
	lightSlot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})

	var mu sync.Mutex
	var tries, landed int
	// Refuses the first two and then works, which is a hub whose store is busy
	// rather than one that is down.
	hub.Cached = func(string, []CardState) error {
		mu.Lock()
		defer mu.Unlock()
		tries++
		if tries <= 2 {
			return errTest("the store is busy")
		}
		landed++
		return nil
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	// Set up once and never touched again: nothing happens on this room after
	// it attaches, which is the whole point of the test.
	state := newRoomState(map[string]any{"id": "one", "status": "running"})
	room := &Room{
		Name: "testroom", Dial: plain{addr: ln.Addr().String()}, Handler: state, AnnounceEvery: testAnnounceEvery,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, announceWait, func() bool { return hub.Has("testroom") })

	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return landed > 0
	})
}

type errTest string

func (e errTest) Error() string { return string(e) }

// A HUB THAT KEEPS NO CACHE IS NOT ANNOUNCED AT, and it says so at the
// handshake rather than by refusing.
//
// A hub with no store is an ordinary thing: it is what every test here builds.
// Finding out by being refused meant a failure logged on every change for the
// life of the attachment, which reads as something being broken.
func TestARoomDoesNotAnnounceAtAHubThatKeepsNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real link, hub and git rig over loopback")
	}
	lightSlot(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: time.Second, Warm: 1})

	var tried int
	var mu sync.Mutex
	// Nothing sets `Cached`, so the hub keeps no cache. This counts any room
	// that tries anyway.
	hub.Attaching = func(string, string, string) error { return nil }

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go func() { _ = hub.Serve(ctx, ln) }()

	state := newRoomState(map[string]any{"id": "one", "status": "running"})
	room := &Room{
		Name: "testroom", Dial: countingDialer{
			addr: ln.Addr().String(),
			saw: func(kind string) {
				if kind == announceKind {
					mu.Lock()
					tried++
					mu.Unlock()
				}
			},
		},
		Handler: state, AnnounceEvery: testAnnounceEvery,
		T: Timings{Beat: 200 * time.Millisecond, Warm: 1, Backoff: 50 * time.Millisecond},
	}
	go func() { _ = room.Run(ctx) }()
	waitFor(t, announceWait, func() bool { return hub.Has("testroom") })

	state.set(map[string]any{"id": "one", "status": "done"})
	time.Sleep(2 * testAnnounceEvery) // wait: a negative check, nothing must be announced

	mu.Lock()
	defer mu.Unlock()
	if tried != 0 {
		t.Fatalf("a room announced %d time(s) at a hub that keeps no cache", tried)
	}
}

// countingDialer reports what kind each connection says it is.
//
// By sniffing the hello the room writes, which is the only place that decision
// is visible from outside the room.
type countingDialer struct {
	addr string
	saw  func(kind string)
}

func (c countingDialer) Dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, err
	}
	return &sniffing{Conn: conn, saw: c.saw}, nil
}

func (c countingDialer) Describe() string { return c.addr }

// index is an everywhere index that keeps what it is handed.
type index struct {
	mu    sync.Mutex
	cards map[string][]CardState
}

func (x *index) Replace(room string, cards []CardState) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.cards[room] = cards
	return true
}

func (x *index) has(room, id string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, c := range x.cards[room] {
		if c.ID == id {
			return true
		}
	}
	return false
}

// AFTER THE CACHE, what a room announced reaches the hub's everywhere index under the room's name.
func TestEverywhereAnAnnouncementReachesTheIndex(t *testing.T) {
	state := newRoomState(map[string]any{"id": "one", "status": "running", "wire_name": "atrium-1",
		"alias": "orchestrator", "tags": []string{"atrium:everywhere"}})
	idx := &index{cards: map[string][]CardState{}}
	_, _, _, stop := cachingWith(t, state, func(h *Hub) { h.Every = idx })
	defer stop()
	waitFor(t, announceWait, func() bool { return idx.has("testroom", "one") })
}
