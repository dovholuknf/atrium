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
