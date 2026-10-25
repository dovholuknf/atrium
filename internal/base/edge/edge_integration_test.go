//go:build integration

package edge

import (
	"net/http"
	"os"
	"testing"
)

// For CHOOSES BY THE ADDRESS: loopback names, the bound address, or every name
// this machine has for a bind on every interface. Never any name at all.
// Integration: a bind on every interface asks the OS for this machine's names.
func TestForChoosesByAddress(t *testing.T) {
	host, _ := os.Hostname()
	cases := []struct {
		addr, host string
		want       int
	}{
		{"127.0.0.1:7781", "sg4:7781", 403},
		{"localhost:7781", "localhost:7781", 200},
		{"192.168.1.68:7781", "192.168.1.68:7781", 200},
		{"192.168.1.68:7781", "evil.example:7781", 403},
		{"0.0.0.0:7781", host + ":7781", 200},
		{":7781", "evil.example:7781", 403},
	}
	for _, c := range cases {
		if got := status(For(c.addr, ok), http.MethodGet, c.host, nil); got != c.want {
			t.Errorf("%s with Host %s answered %d, want %d", c.addr, c.host, got, c.want)
		}
	}
}
