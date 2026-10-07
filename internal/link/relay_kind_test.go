package link

import (
	"strings"
	"testing"
)

// r-comms-one-instruction: the launch line names atrium_say and not atrium_report, and a
// relayed say keeps its kind, so a launcher's fyi on another room makes no report owed there.

func TestTheLaunchLineNamesAtriumSayAndNotAtriumReport(t *testing.T) {
	for _, want := range []string{"atrium_say", "done <sha>", "blocked: <one line>"} {
		if !strings.Contains(reportLine, want) {
			t.Errorf("the launch line misses %q: %q", want, reportLine)
		}
	}
	if strings.Contains(reportLine, "atrium_report") {
		t.Errorf("the launch line names atrium_report: %q", reportLine)
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
