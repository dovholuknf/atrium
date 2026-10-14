//go:build integration

package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A blocked reply is answered by the CLI itself and never posted: the turn is
// going on, so the room is not told it ended.
func TestTurnBlocksAReplyHandingADeployCommandWithoutPosting(t *testing.T) {
	replyEnv(t, "on")
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { posted = true }))
	defer srv.Close()
	restore := withStdin(t, `{"cwd":"/w","hook_event_name":"Stop","last_assistant_message":"`+"```"+`\natrium-deploy.ps1\n`+"```"+`"}`)
	defer restore()

	got := turnEnded(srv.URL, "end", "probe", "")
	if !strings.Contains(got, `"decision":"block"`) {
		t.Fatalf("got %q, wanted a block", got)
	}
	if posted {
		t.Fatal("the room was told the turn ended while it went on")
	}
}

func TestTurnAnswersTheBlockedReplyOnceThenLetsGo(t *testing.T) {
	replyEnv(t, "on")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()
	restore := withStdin(t, `{"cwd":"/w","hook_event_name":"Stop","stop_hook_active":true,"last_assistant_message":"`+"```"+`\natrium-deploy.ps1\n`+"```"+`"}`)
	defer restore()
	if got := turnEnded(srv.URL, "end", "probe", ""); got != keepGoing {
		t.Fatalf("got %q", got)
	}
}
