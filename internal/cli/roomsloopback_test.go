package cli

import (
	"strings"
	"testing"
)

func TestLoopbackJoinWarningNamesTheHubsOwnPort(t *testing.T) {
	w := loopbackJoinWarning("direct", "127.0.0.1:9779", "")
	if !strings.Contains(w, "--link 0.0.0.0:9779 --link-advertise <this-host>:9779") || strings.Contains(w, "7779") {
		t.Errorf("warning does not use the hub's port: %q", w)
	}
}

func TestLoopbackJoinWarning(t *testing.T) {
	w := loopbackJoinWarning("direct", "127.0.0.1:7779", "")
	for _, want := range []string{"127.0.0.1:7779", "--link 0.0.0.0:7779", "--link-advertise"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q lacks %q", w, want)
		}
	}
	for name, c := range map[string][3]string{
		"advertised": {"direct", "0.0.0.0:7779", "hub.example.com:7779"},
		"ziti":       {"ziti", "127.0.0.1:7779", ""},
		"wide":       {"direct", "0.0.0.0:7779", ""},
	} {
		if got := loopbackJoinWarning(c[0], c[1], c[2]); got != "" {
			t.Errorf("%s: warned %q", name, got)
		}
	}
}
