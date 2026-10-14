//go:build integration

package cli

import (
	"errors"
	"net"
	"strings"
	"testing"
)

// sg4 moved from 192.168.1.68 to 192.168.102.41 and the hub went on telling rooms the old one.
func TestAnAdvertisedAddressThatIsNotThisMachineIsFlagged(t *testing.T) {
	local := func() ([]net.IP, error) { return []net.IP{net.ParseIP("192.168.102.41")}, nil }
	none := func(string) ([]string, error) { return nil, errors.New("no such host") }

	if why := advertiseProblem("192.168.1.68:7779", local, none); why == "" {
		t.Error("the old LAN address was not flagged")
	}
	for _, ok := range []string{"192.168.102.41:7779", "127.0.0.1:7779", "[::1]:7779"} {
		if why := advertiseProblem(ok, local, none); why != "" {
			t.Errorf("%s was flagged: %s", ok, why)
		}
	}
}

func TestAnAdvertisedNameIsFlaggedUnlessItResolvesHere(t *testing.T) {
	local := func() ([]net.IP, error) { return []net.IP{net.ParseIP("192.168.102.41")}, nil }
	stale := func(string) ([]string, error) { return []string{"192.168.1.68"}, nil }
	fresh := func(string) ([]string, error) { return []string{"192.168.1.68", "192.168.102.41"}, nil }
	gone := func(string) ([]string, error) { return nil, errors.New("no such host") }

	if why := advertiseProblem("sg4:7779", local, stale); !strings.Contains(why, "192.168.1.68") {
		t.Errorf("a name resolving elsewhere said %q, want the address it resolved to", why)
	}
	if why := advertiseProblem("sg4:7779", local, fresh); why != "" {
		t.Errorf("a name resolving here was flagged: %s", why)
	}
	if why := advertiseProblem("sg4:7779", local, gone); why == "" {
		t.Error("a name that does not resolve was not flagged")
	}
}

func TestAnAdvertiseCheckThatCannotListInterfacesSaysNothing(t *testing.T) {
	local := func() ([]net.IP, error) { return nil, errors.New("boom") }
	if why := advertiseProblem("10.9.9.9:7779", local, nil); why != "" {
		t.Errorf("an unanswerable check complained: %s", why)
	}
}
