package daemon

import (
	"strings"
	"testing"
)

// A RUNNER MUST NOT BE MONOCHROME BECAUSE OF HOW THE DAEMON WAS STARTED.
//
// Programs decide whether to emit colour by reading the environment. Started
// from a terminal, atrium passed that terminal's markers down and everything
// was in colour. Started as a service, a scheduled task or a detached process,
// it passed none, and every agent came out plain white with nothing on the
// board to explain it: the theme was right and the child had simply decided
// not to use it.
func TestAPseudoTerminalSaysItIsOne(t *testing.T) {
	got := declareATerminal([]string{"PATH=/usr/bin"})
	for _, want := range []string{"TERM=xterm-256color", "COLORTERM=truecolor"} {
		if !envHas(got, want) {
			t.Errorf("a pty child was not told %q, so it will print in black and white", want)
		}
	}
	if !envHas(got, "PATH=/usr/bin") {
		t.Error("the environment it was given was lost")
	}
}

// A harness naming its own TERM is somebody saying they know better about
// their own runner. This is a default, not a policy.
func TestAnExistingTerminalIsLeftAlone(t *testing.T) {
	got := declareATerminal([]string{"TERM=dumb", "COLORTERM=", "PATH=/usr/bin"})
	if envHas(got, "TERM=xterm-256color") {
		t.Error("a runner that asked for TERM=dumb was overruled")
	}
	if envHas(got, "COLORTERM=truecolor") {
		t.Error("a runner that emptied COLORTERM on purpose was overruled")
	}
}

func envHas(env []string, want string) bool {
	for _, kv := range env {
		if strings.EqualFold(kv, want) {
			return true
		}
	}
	return false
}
