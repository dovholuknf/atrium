package cli

import (
	"context"
	"log"
	"net"
	"time"
)

// advertiseEvery is how often a running hub looks again. A DHCP lease moves the machine
// under a hub that is still up, and the address in every join string and in `room.json`
// is then wrong with nothing to say so.
const advertiseEvery = 5 * time.Minute

// advertiseProblem says why `adv` (host:port, what rooms are told to dial) does not name this machine,
// or "" when it does or when that cannot be told.
//
// A WARNING, NEVER A REFUSAL. A name or an address can legitimately belong to a forwarder or a
// load balancer in front of this machine, so the hub starts anyway. What it must not do is
// stay quiet: a stale address looks exactly like a room that never tries.
//
// `local` lists this machine's interface addresses and `lookup` resolves a name, both passed
// in so a test needs neither a network nor a DHCP lease.
func advertiseProblem(adv string, local func() ([]net.IP, error), lookup func(string) ([]string, error)) string {
	host, _, err := net.SplitHostPort(adv)
	if err != nil || host == "" {
		return ""
	}
	mine, err := local()
	if err != nil {
		return ""
	}
	has := func(ip net.IP) bool {
		if ip.IsLoopback() {
			return true
		}
		for _, m := range mine {
			if m.Equal(ip) {
				return true
			}
		}
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		if has(ip) {
			return ""
		}
		return "it is an address, and it is not on any interface of this machine"
	}
	got, err := lookup(host)
	if err != nil {
		return "the name does not resolve: " + err.Error()
	}
	for _, g := range got {
		if ip := net.ParseIP(g); ip != nil && has(ip) {
			return ""
		}
	}
	return "the name resolves to " + joinOr(got, "nothing") + ", none of which is on an interface of this machine"
}

func joinOr(s []string, none string) string {
	if len(s) == 0 {
		return none
	}
	out := s[0]
	for _, x := range s[1:] {
		out += ", " + x
	}
	return out
}

func interfaceIPs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var out []net.IP
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n.IP)
		}
	}
	return out, nil
}

// watchAdvertise checks the advertised address now and every advertiseEvery, saying so LOUDLY
// when it is not this machine and again when it is right once more. Said on a change and
// not on every tick, so a hub left wrong for a week is not a week of the same line.
func watchAdvertise(ctx context.Context, adv string) {
	wrong := false
	check := func(first bool) {
		why := advertiseProblem(adv, interfaceIPs, net.LookupHost)
		switch {
		case why != "" && !wrong:
			log.Printf("[hub] WARNING: this hub tells rooms to dial %s and that is not this machine: %s. "+
				"a direct room joined with that address cannot attach. restart with the right --link-advertise, "+
				"then point each room at it with `atrium room hub <addr>`", adv, why)
		case why == "" && wrong:
			log.Printf("[hub] the advertised address %s is this machine again", adv)
		}
		wrong = why != ""
	}
	check(true)
	t := time.NewTicker(advertiseEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check(false)
		}
	}
}
