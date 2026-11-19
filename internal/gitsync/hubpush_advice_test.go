package gitsync

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPushAdviceNamesWhatHappenedAndWhatToDo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, report string
		err, ctxErr  error
		want         string
	}{
		{"caller gave up", "", errors.New("signal: killed"), context.Canceled, "stopped waiting"},
		{"deadline", "", errors.New("x"), context.DeadlineExceeded, "did not finish"},
		{"runner bound", "", errors.New("git did not finish in 10m0s and was killed"), nil, "did not finish"},
		{"over the cap", "error: RPC failed; HTTP 413 curl 22", errors.New("x"), nil, "500 MB"},
	}
	for _, c := range cases {
		got := PushAdvice(c.report, c.err, c.ctxErr)
		for _, w := range []string{c.want, "Nothing landed", ".gitignore", "Do not split"} {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", c.name, got, w)
			}
		}
	}
	if got := PushAdvice("! [remote rejected] x (atrium: not yours)", errors.New("x"), nil); got != "" {
		t.Fatalf("a hub refusal is git's own words, got advice %q", got)
	}
}

func TestPushFailureLeadsWithTheKillNotGitsSideEffect(t *testing.T) {
	t.Parallel()
	raw := "send-pack: protocol error: bad band #69"
	got := PushFailure(raw, errors.New("git did not finish in 10m0s and was killed"), nil).Error()
	if !strings.HasPrefix(got, "the push did not land: The push did not finish in") || strings.Contains(got, raw) {
		t.Fatalf("a push killed for time must lead with the kill, got %q", got)
	}
	got = PushFailure("remote: atrium: no", errors.New("x"), nil).Error()
	if !strings.Contains(got, "remote: atrium: no") {
		t.Fatalf("a hub refusal keeps git's words, got %q", got)
	}
}
