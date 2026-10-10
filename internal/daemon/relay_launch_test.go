package daemon

import (
	"context"
)

// A launch on another room, as this room sees it: the endpoint behind the stdio
// atrium_launch with `room`. The hub is the fake relay.

func (f *fakeRelay) Launch(_ context.Context, l RelayLaunch) (RelayResult, error) {
	f.mu.Lock()
	f.launches = append(f.launches, l)
	launch := f.launch
	f.mu.Unlock()
	if launch != nil {
		return launch(l)
	}
	card := l.Room + "~kid"
	return RelayResult{OK: true, To: "kid@" + l.Room, Card: card, Watch: "http://hub/#term=kid", Brief: l.Cwd + "/BRIEF.md",
		Model: l.Model, Effort: l.Effort, Task: &RemoteTask{Card: card, Handle: "kid@" + l.Room, Title: l.Title,
			Status: "running"}}, nil
}

func (f *fakeRelay) launched() []RelayLaunch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RelayLaunch(nil), f.launches...)
}
