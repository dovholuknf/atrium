package cli

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

func TestNoFlagsSeedNothing(t *testing.T) {
	for _, s := range []link.TransportSeed{roomZitiSeed("direct", "x", ""), boardZitiSeed("", "x"), ciHookSeed("")} {
		if s.On != nil {
			t.Errorf("a seed with no flag is on: %+v", s)
		}
	}
}

func TestADirectHubDoesNotBindZrokAtStart(t *testing.T) {
	if listenAtStart("direct", "zrok", 1) {
		t.Error("a hub on --transport direct binds zrok at start, which reserves a share")
	}
	if !listenAtStart("direct,zrok", "zrok", 1) {
		t.Error("a hub on --transport direct,zrok does not bind zrok at start")
	}
	if !listenAtStart("direct", "direct", 0) {
		t.Error("a hub on --transport direct does not bind direct at start")
	}
}
