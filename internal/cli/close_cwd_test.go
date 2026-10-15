package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloseCwdPostsTheFolderAndWaitsByDefault(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"cards":["c1"],"closed":[{"card":"c1","closed":true}]}`))
	}))
	defer srv.Close()

	// without --yes nothing is called
	if err := closeByCwdCmd(closeOpts{boardURL: srv.URL, cwd: `C:\wt\a\`}); err != nil || path != "" {
		t.Fatalf("preview called the board: %v %q", err, path)
	}
	if err := closeByCwdCmd(closeOpts{boardURL: srv.URL, cwd: `C:\wt\a\`, yes: true, work: "keep"}); err != nil {
		t.Fatal(err)
	}
	if path != "POST /v1/cards/archive" || got["cwd"] != `C:\wt\a\` || got["wait"] != true || got["work"] != "keep" {
		t.Fatalf("%s %v", path, got)
	}
	if err := closeByCwdCmd(closeOpts{boardURL: srv.URL, cwd: "/x", yes: true, noWait: true}); err != nil || got["wait"] != false {
		t.Fatalf("%v %v", err, got)
	}
}

func TestCloseCommandTakesACardOrCwdNotBoth(t *testing.T) {
	c := newClose()
	c.SetArgs([]string{"card1", "--cwd", "/x", "--yes"})
	if err := c.Execute(); err == nil {
		t.Fatal("both were accepted")
	}
	c = newClose()
	c.SetArgs([]string{})
	if err := c.Execute(); err == nil {
		t.Fatal("neither was refused")
	}
}
