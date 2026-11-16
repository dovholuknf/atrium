//go:build integration

package daemon

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/seenhost"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestTheHostTheBrowserUsedBuildsTheWalkLink(t *testing.T) {
	seenhost.Reset()
	t.Cleanup(func() { seenhost.Reset() })
	d := testDaemon(t)
	d.opts.HumanAddr = "127.0.0.1:7781"
	d.opts.Room = "r1"
	line := func() string { return d.walkLinkLine(&store.Task{ID: "c1"}) }
	if !strings.Contains(line(), "http://127.0.0.1:7781/#card=c1&walk=") {
		t.Fatalf("no host seen did not fall back: %q", line())
	}
	cli := httptest.NewRequest("POST", "http://127.0.0.1:7781/v1/x", nil)
	cli.Host = "other"
	seenhost.Note(cli)
	if seenhost.Base("") != "" {
		t.Fatal("a request with no Sec-Fetch-Mode was recorded")
	}
	in := httptest.NewRequest("POST", "http://atrium.ziti/v1/tasks/c1/input", nil)
	in.Header.Set("Sec-Fetch-Mode", "cors")
	seenhost.Note(in)
	if got := line(); !strings.Contains(got, "http://atrium.ziti/room/r1/c1/files/") || !strings.Contains(got, "a zrok share") {
		t.Fatalf("walk line = %q", got)
	}
	fwd := httptest.NewRequest("GET", "http://room/v1/events", nil)
	fwd.Header.Set("Sec-Fetch-Mode", "cors")
	fwd.Header.Set("X-Forwarded-Host", "desk:7778")
	fwd.Header.Set("X-Forwarded-Proto", "https")
	seenhost.Note(fwd)
	if got := seenhost.Base("x"); got != "https://desk:7778" {
		t.Fatalf("humanBase = %q", got)
	}
}
