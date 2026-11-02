package daemon

import (
	"context"
	"github.com/dovholuknf/atrium/internal/daemon/hublink"
)

// A launch on another room, as this room sees it: the endpoint behind the stdio
// atrium_launch with `room`. The hub is the fake relay.

func (f *fakeRelay) Launch(_ context.Context, l hublink.RelayLaunch) (hublink.RelayResult, error) {
	f.mu.Lock()
	f.launches = append(f.launches, l)
	launch := f.launch
	f.mu.Unlock()
	if launch != nil {
		return launch(l)
	}
	card := l.Room + "~kid"
	return hublink.RelayResult{OK: true, To: "kid@" + l.Room, Card: card, Watch: "http://hub/#term=kid", Brief: l.Cwd + "/BRIEF.md",
		Model: l.Model, Effort: l.Effort, Task: &hublink.RemoteTask{Card: card, Handle: "kid@" + l.Room, Title: l.Title,
			Status: "running"}}, nil
}

func (f *fakeRelay) launched() []hublink.RelayLaunch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]hublink.RelayLaunch(nil), f.launches...)
}
