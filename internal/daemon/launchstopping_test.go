package daemon

import "testing"

func TestStopperIsStoppingOnceRequested(t *testing.T) {
	s := newStopper()
	if s.stopping() {
		t.Fatal("a fresh stopper says it is stopping")
	}
	s.request("a test")
	if !s.stopping() {
		t.Fatal("a requested stop is not stopping")
	}
	var none *stopper
	if none.stopping() {
		t.Fatal("a nil stopper says it is stopping")
	}
}
