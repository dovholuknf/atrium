package cardproc

import (
	"net"
	"testing"
)

type fakeLn struct {
	port   int
	closed bool
}

func (f *fakeLn) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (f *fakeLn) Close() error              { f.closed = true; return nil }
func (f *fakeLn) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: f.port} }

func TestListenLoopbackSkipsThePreviewBlock(t *testing.T) {
	picks := []*fakeLn{{port: 50000}, {port: 50999}, {port: 49999}}
	i := 0
	ln, err := listenOutside(Previews, func() (net.Listener, error) { i++; return picks[i-1], nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := ln.Addr().(*net.TCPAddr).Port; got != 49999 {
		t.Fatalf("handed out %d", got)
	}
	if !picks[0].closed || !picks[1].closed || picks[2].closed {
		t.Fatalf("closed %v %v %v, wanted the two in the block closed and the one handed out open",
			picks[0].closed, picks[1].closed, picks[2].closed)
	}
}

func TestListenLoopbackFailsClosedWhenEveryPickIsInTheBlock(t *testing.T) {
	var all []*fakeLn
	ln, err := listenOutside(Previews, func() (net.Listener, error) {
		f := &fakeLn{port: 50500}
		all = append(all, f)
		return f, nil
	})
	if err == nil {
		t.Fatalf("handed out %v", ln.Addr())
	}
	for _, f := range all {
		if !f.closed {
			t.Fatal("a port in the block was left open")
		}
	}
}

func TestListenLoopbackIsOutsideTheBlock(t *testing.T) {
	ln, err := ListenLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if p := ln.Addr().(*net.TCPAddr).Port; Previews.Has(p) {
		t.Fatalf("listening on %d", p)
	}
}
