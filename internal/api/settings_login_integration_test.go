//go:build integration

package api

import (
	"net/http"
	"testing"
)

func TestTheLoginCommandCanBeSaved(t *testing.T) {
	t.Parallel()
	srv, _, _ := fileServer(t)

	if rec := settingsPost(t, srv, `{"login_command":"ssh -t m1mini \"zsh -lic 'claude auth login'\""}`); rec.Code != http.StatusOK {
		t.Fatalf("saving a login command answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["login_command"]; got != `ssh -t m1mini "zsh -lic 'claude auth login'"` {
		t.Fatalf("the login command read back as %v", got)
	}
}
