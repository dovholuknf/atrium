//go:build integration

package link

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
)

func TestTheLaunchLineNamesDoneAndBlockedAndNotAtriumReport(t *testing.T) {
	for _, want := range []string{"atrium_done <sha or artifact>", "atrium_blocked <up to 50 words>", "Wait for every command", "background"} {
		if !strings.Contains(reportLine, want) {
			t.Errorf("the launch line misses %q: %q", want, reportLine)
		}
	}
	if strings.Contains(reportLine, "atrium_report") {
		t.Errorf("the launch line names atrium_report: %q", reportLine)
	}
	if reportLine != daemon.LaunchEndingLine {
		t.Errorf("the hub's ending and the room's differ:\n%q\n%q", reportLine, daemon.LaunchEndingLine)
	}
}

func TestARelayedFyiKeepsItsKind(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()

	for _, kind := range []string{"fyi", ""} {
		ans, err := x.miniR.Relay(relayCtx(t), RelayRequest{Op: RelaySay, From: "sa1", Room: "sg4",
			To: "atrium-87300", Text: "stop, nothing else to do", Kind: kind})
		if err != nil || !ans.OK {
			t.Fatalf("%q: answer = %+v, %v", kind, ans, err)
		}
		got := x.sg4.messages()
		last := got[len(got)-1]
		if k := last["kind"]; k != kind {
			t.Fatalf("sg4 got kind %q, want %q: %+v", k, kind, last)
		}
	}
}
