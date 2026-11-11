//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"
)

// Text atrium typed once is never typed again when turns end afterwards: the retries and Stop hooks that follow a
// delivered message find nothing to retype, and the typing model holds nothing for the next say to be blocked by.
func TestADeliveredMessageIsNotRetypedWhenTurnsEnd(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("ls\r"))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()

	const word = "DISCONNECTSTOPSGG"
	m, _ := d.st.QueueMessage(target.ID, word)
	d.deferPeerInjection(target.ID, m.ID, "", word, "", false)

	d.pending.attempt(target.ID)
	for i := 0; i < 3; i++ {
		r.resetLineAtStop()
		d.pending.attempt(target.ID)
	}

	if n := strings.Count(f.written(), word); n != 1 {
		t.Fatalf("the message was typed %d times across three turn ends: %q", n, f.written())
	}
	if s := r.typing(); s.Count != 0 || s.Line != "" {
		t.Fatalf("the typing model holds %q after the turns ended", s.Line)
	}
}
