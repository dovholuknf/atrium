package daemon

import "testing"

// A ROOM THAT HAS NOT BOUND ITS LISTENERS forwards nothing, since it cannot tell which ports are its own.
func TestPreviewRefusesEveryPortBeforeTheListenersAreBound(t *testing.T) {
	if err := (&Daemon{}).PreviewPort(50001); err == nil {
		t.Fatal("a room with no listeners yet let a preview through")
	}
}
