package cli

import (
	"net"
	"testing"
	"time"
)

func TestLoopbackV6TakesTheSamePort(t *testing.T) {
	v4, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v4.Close()
	v6 := loopbackV6(v4.Addr().String())
	if v6 == nil {
		t.Skip("this machine has no ::1")
	}
	defer v6.Close()
	_, want, _ := net.SplitHostPort(v4.Addr().String())
	_, got, _ := net.SplitHostPort(v6.Addr().String())
	if got != want {
		t.Fatalf("[::1] took port %s, the board is on %s", got, want)
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("::1", want), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("a dial of ::1 was not answered: %v", err)
	}
	c.Close()
}

func TestLoopbackV6LeavesOtherAddressesAlone(t *testing.T) {
	for _, a := range []string{"0.0.0.0:7778", "[::]:7778", "192.168.1.2:7778", "nonsense"} {
		if ln := loopbackV6(a); ln != nil {
			ln.Close()
			t.Fatalf("%s got a [::1] listener", a)
		}
	}
}
