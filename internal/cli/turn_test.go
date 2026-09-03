package cli

import (
	"os"
	"strings"
	"testing"
)

// withStdin hands the command a hook payload the way Claude Code does, through
// a pipe. A pipe rather than a file because `interactive()` decides whether
// there is a payload to read at all by asking whether stdin is a terminal.
func withStdin(t *testing.T, payload string) func() {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = w.WriteString(payload)
		w.Close()
	}()
	was := os.Stdin
	os.Stdin = r
	return func() {
		os.Stdin = was
		r.Close()
	}
}

// A Stop hook that blocks tells the model to keep going. Every one of these is
// a way a session could be told to keep working forever, so each is a rule
// about what may never be printed rather than a check on what usually is.

// Anything that is not a block with something to say is a plain continue.
func TestTurnAnswerOnlyPassesThroughARealBlock(t *testing.T) {
	cases := []struct {
		name, body string
		wantBlock  bool
	}{
		{"a message to deliver",
			`{"decision":"block","reason":"run the tests first"}`, true},
		{"nothing to say",
			`{"continue":true}`, false},
		{"a block with no reason, which would stop the turn and say nothing",
			`{"decision":"block","reason":""}`, false},
		{"a block whose reason is only whitespace",
			`{"decision":"block","reason":"   "}`, false},
		{"an empty body, which is a read that got nothing",
			``, false},
		{"a proxy's error page rather than the daemon",
			`<html><body>502 Bad Gateway</body></html>`, false},
		{"json that is not an object",
			`[1,2,3]`, false},
		{"a truncated read",
			`{"decision":"block","reas`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := turnAnswer(strings.NewReader(c.body))
			if c.wantBlock {
				if got != c.body {
					t.Fatalf("a real block was not passed through: got %q", got)
				}
				return
			}
			if got != keepGoing {
				t.Fatalf("got %q, wanted a plain continue", got)
			}
		})
	}
}

// stop_hook_active means this turn is only running because a Stop hook already
// blocked it. Blocking again never terminates.
func TestTurnRefusesToBlockInsideABlockedTurn(t *testing.T) {
	restore := withStdin(t, `{"cwd":"/w","stop_hook_active":true}`)
	defer restore()

	// No daemon is running on this address, but that is not what is being
	// tested: this must return before it would ever reach one.
	if got := turnEnded("http://127.0.0.1:1", "end", "probe"); got != keepGoing {
		t.Fatalf("got %q, wanted a plain continue inside a blocked turn", got)
	}
}

// The daemon being down is the ordinary case on a machine where atrium is not
// running, and it must cost a session nothing.
func TestTurnKeepsGoingWhenAtriumIsNotThere(t *testing.T) {
	restore := withStdin(t, `{"cwd":"/w","session_id":"abc"}`)
	defer restore()

	if got := turnEnded("http://127.0.0.1:1", "end", "probe"); got != keepGoing {
		t.Fatalf("got %q, wanted a plain continue with no daemon", got)
	}
}

// Unparseable stdin is fatal here where it is survivable elsewhere: without
// the payload there is no way to know whether this pass is already inside a
// blocked turn.
func TestTurnKeepsGoingOnUnreadablePayload(t *testing.T) {
	restore := withStdin(t, `{"cwd":`)
	defer restore()

	if got := turnEnded("http://127.0.0.1:1", "end", "probe"); got != keepGoing {
		t.Fatalf("got %q, wanted a plain continue on a bad payload", got)
	}
}

// The gate being switched off means atrium is not in the loop at all.
func TestTurnKeepsGoingWhenTheGateIsOff(t *testing.T) {
	t.Setenv("ATRIUM_PERM_GATE", "off")
	restore := withStdin(t, `{"cwd":"/w"}`)
	defer restore()

	if got := turnEnded("http://127.0.0.1:1", "end", "probe"); got != keepGoing {
		t.Fatalf("got %q, wanted a plain continue with the gate off", got)
	}
}
