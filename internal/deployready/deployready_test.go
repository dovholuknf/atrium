package deployready

import (
	"testing"
)

func TestParseTrailers(t *testing.T) {
	msg := "Review of x\n\nbody\n\nAtrium-Verdict: hub-ok 108ced12~1..86240e3a\nAtrium-Verdict: room-ok abc1234\n" +
		"Atrium-Verdict: hold -rf..x\nAtrium-Verdict: maybe abc1234\n  Atrium-Verdict: hub-ok indented\n"
	got := parseTrailers(msg)
	if len(got) != 2 || got[0].kind != "hub-ok" || got[0].spec != "108ced12~1..86240e3a" ||
		got[1].kind != "room-ok" || got[1].spec != "abc1234" {
		t.Fatalf("trailers = %+v", got)
	}
}
