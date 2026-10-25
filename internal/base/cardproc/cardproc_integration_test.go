//go:build integration

package cardproc

import (
	"errors"
	"os"
	"testing"
)

func TestParseExcludedReadsNetshsTable(t *testing.T) {
	out := "\r\nProtocol tcp Port Exclusion Ranges\r\n\r\nStart Port    End Port\r\n----------    --------\r\n" +
		"      5357        5357\r\n     50260       50359     *\r\n\r\n* - Administered port exclusions.\r\n"
	rs := parseExcluded(out)
	if len(rs) != 2 || rs[0] != (Range{5357, 5357}) || rs[1] != (Range{50260, 50359}) {
		t.Fatalf("%v", rs)
	}
}

func TestStartTimeIsStableForALiveProcessAndGoneForNone(t *testing.T) {
	a, err := StartTime(os.Getpid())
	if err != nil || a == "" {
		t.Fatalf("%q %v", a, err)
	}
	if b, _ := StartTime(os.Getpid()); b != a {
		t.Errorf("%q then %q", a, b)
	}
	if _, err := StartTime(0); !errors.Is(err, ErrGone) {
		t.Errorf("pid 0: %v", err)
	}
}
