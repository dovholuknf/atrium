package cli

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
)

type memSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSettings) HubSetting(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[k], nil
}

func (s *memSettings) SetHubSetting(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
	return nil
}

func TestLiveFlagsSeedAllThreeTransportsWithNoStoredSettings(t *testing.T) {
	ht := &link.HubTransports{Store: &memSettings{m: map[string]string{}}, IdentityFallback: "/ids/hub.json"}
	bound := map[string]string{}
	var mu sync.Mutex
	item := func(name string) link.TransportItem {
		return link.TransportItem{
			Name: name, Kind: name, DefaultService: "default-" + name,
			Listen: func(svc, identity string) (net.Listener, error) {
				mu.Lock()
				bound[name] = svc + "@" + identity
				mu.Unlock()
				return net.Listen("tcp", "127.0.0.1:0")
			},
			Serve: func(ctx_ context.Context, ln net.Listener) error { <-ctx_.Done(); return nil },
		}
	}
	ht.Add(item(link.TransportRoomZiti), roomZitiSeed("direct,ziti", "atrium-hub", "/ids/hub.json"))
	ht.Add(item(link.TransportBoardZiti), boardZitiSeed("ziti", "atrium-board"))
	ht.Add(item(link.TransportCIHook), ciHookSeed("ziti-hook"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ht.Start(ctx)
	want := map[string]string{
		link.TransportRoomZiti:  "atrium-hub@/ids/hub.json",
		link.TransportBoardZiti: "atrium-board@/ids/hub.json",
		link.TransportCIHook:    "ziti-hook@/ids/hub.json",
	}
	for i := 0; i < 100; i++ {
		mu.Lock()
		n := len(bound)
		mu.Unlock()
		if n == 3 {
			break
		}
		time.Sleep(20 * time.Millisecond) // wait: poll step under one deadline
	}
	mu.Lock()
	defer mu.Unlock()
	for k, v := range want {
		if bound[k] != v {
			t.Errorf("%s bound %q, want %q", k, bound[k], v)
		}
	}
	for _, v := range ht.View() {
		if !v.On {
			t.Errorf("%s is not on", v.Name)
		}
	}
}

func TestNoFlagsSeedNothing(t *testing.T) {
	for _, s := range []link.TransportSeed{roomZitiSeed("direct", "x", ""), boardZitiSeed("", "x"), ciHookSeed("")} {
		if s.On != nil {
			t.Errorf("a seed with no flag is on: %+v", s)
		}
	}
}
