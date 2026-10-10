package daemon

import (
	"context"
	"fmt"
	"sync"
)

// A fake second room, as far as this room can tell: a Relay that records what
// it was asked and answers what the test says. See docs/fabric/cross-room-say-design.md.
type fakeRelay struct {
	mu     sync.Mutex
	got    []RelaySay
	answer func(RelaySay) (RelayResult, error)
	peers  []RemotePeer
	// reaches is every Card and Exit asked, and reach answers them.
	reaches []RelaySay
	reach   func(RelaySay) (RelayResult, error)
	// found is every name Find was asked, and find answers it.
	found []string
	find  func(string) (RelayResult, error)
	// launches is every Launch asked, and launch answers them. See relay_launch_test.go.
	launches []RelayLaunch
	launch   func(RelayLaunch) (RelayResult, error)
}

func (f *fakeRelay) Say(_ context.Context, s RelaySay) (RelayResult, error) {
	f.mu.Lock()
	f.got = append(f.got, s)
	answer := f.answer
	f.mu.Unlock()
	if answer == nil {
		return RelayResult{OK: true, Delivered: "queued", When: WhenImmediate, To: s.To + "@" + s.Room,
			Card: s.Room + "~L1"}, nil
	}
	return answer(s)
}

func (f *fakeRelay) Peers(_ context.Context, _, _ bool) ([]RemotePeer, string, error) {
	return f.peers, "", nil
}

func (f *fakeRelay) Find(_ context.Context, name string) (RelayResult, error) {
	f.mu.Lock()
	f.found = append(f.found, name)
	find := f.find
	f.mu.Unlock()
	if find != nil {
		return find(name)
	}
	return RelayResult{Code: 404, Error: "no card called " + name + " on another room"}, nil
}

// Card and Exit record the request as a RelaySay with Text "card" or "exit",
// and answer reach when it is set.
func (f *fakeRelay) Card(_ context.Context, room, to string, events bool) (RelayResult, error) {
	return f.reached(RelaySay{Room: room, To: to, Text: "card", When: fmt.Sprint(events)})
}

func (f *fakeRelay) Exit(_ context.Context, room, to, from string, force bool) (RelayResult, error) {
	s := RelaySay{Room: room, To: to, From: from, Text: "exit"}
	if force {
		s.When = "force"
	}
	return f.reached(s)
}

func (f *fakeRelay) reached(s RelaySay) (RelayResult, error) {
	f.mu.Lock()
	f.reaches = append(f.reaches, s)
	reach := f.reach
	f.mu.Unlock()
	if reach != nil {
		return reach(s)
	}
	card, handle := s.Room+"~L1", s.To+"@"+s.Room
	return RelayResult{OK: true, To: handle, Card: card,
		Task: &RemoteTask{Card: card, Handle: handle, Status: "working"}}, nil
}

func (f *fakeRelay) reachedAll() []RelaySay {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RelaySay(nil), f.reaches...)
}

func (f *fakeRelay) says() []RelaySay {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RelaySay(nil), f.got...)
}

func (f *fakeRelay) set(answer func(RelaySay) (RelayResult, error)) {
	f.mu.Lock()
	f.answer = answer
	f.mu.Unlock()
}
