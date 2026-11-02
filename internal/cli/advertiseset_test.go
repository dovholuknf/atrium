package cli

import (
	"context"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

func ips(s ...string) func() ([]net.IP, error) {
	return func() ([]net.IP, error) {
		var out []net.IP
		for _, x := range s {
			out = append(out, net.ParseIP(x))
		}
		return out, nil
	}
}

func TestAdvertiseSetTakesAHostNameAndEveryInterfaceOfAWideBind(t *testing.T) {
	got := advertiseSet("0.0.0.0:7779", "hub.example:7779",
		ips("127.0.0.1", "192.168.1.68", "169.254.3.4", "100.64.1.2", "10.0.0.5", "fe80::1", "192.168.1.68"))
	want := []string{"hub.example:7779", "10.0.0.5:7779", "192.168.1.68:7779"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAdvertiseSetOfALoopbackBindIsTheLoopbackAddress(t *testing.T) {
	got := advertiseSet("127.0.0.1:7779", "", ips("192.168.1.68"))
	if !reflect.DeepEqual(got, []string{"127.0.0.1:7779"}) {
		t.Fatalf("got %v", got)
	}
}

func TestAdvertiseSetKeepsAnExplicitListInOrder(t *testing.T) {
	got := advertiseSet("127.0.0.1:7779", "b.example:1, a.example:1,b.example:1", ips())
	if !reflect.DeepEqual(got, []string{"b.example:1", "a.example:1"}) {
		t.Fatalf("got %v", got)
	}
	if a, _ := advertiseFor("127.0.0.1:7779", "b.example:1,a.example:1"); a != "b.example:1" {
		t.Fatalf("join string carries %q", a)
	}
}

func TestWatchPathsPushesOnlyWhenTheListChanged(t *testing.T) {
	var addr atomic.Value
	addr.Store("a:1")
	var pushed, reads atomic.Int32
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go watchPaths(ctx, time.Millisecond, func() []wire.Path {
		reads.Add(1)
		return []wire.Path{{Transport: "direct", Addr: addr.Load().(string)}}
	}, func() { pushed.Add(1) })
	for i := 0; i < 200 && reads.Load() < 3; i++ {
		time.Sleep(time.Millisecond) // wait: the watcher must have read its baseline and ticked before the change
	}
	if n := pushed.Load(); n != 0 {
		t.Fatalf("pushed %d times with nothing changed", n)
	}
	addr.Store("b:1")
	testwait.PollEvery(time.Millisecond, 200*time.Millisecond, func() bool { return pushed.Load() != 0 })
	if pushed.Load() != 1 {
		t.Fatalf("pushed %d times after one change, want 1", pushed.Load())
	}
}
