//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func runRooms(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := hubRoomsCmd("rooms", "atrium-")
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestReposAddListAndRemove(t *testing.T) {
	db := filepath.Join(t.TempDir(), "hub.db")
	ck := t.TempDir()
	if out, err := runRooms(t, "git", "repos", "add", "github/o/r", ck, "--atrium-db", db); err != nil {
		t.Fatalf("add: %v %s", err, out)
	}
	out, err := runRooms(t, "git", "repos", "ls", "--atrium-db", db)
	if err != nil || !strings.Contains(out, "github/o/r") || !strings.Contains(out, "claude/main") {
		t.Fatalf("ls: %v %s", err, out)
	}
	if out, err := runRooms(t, "git", "repos", "add", "../evil", ck, "--atrium-db", db); err == nil {
		t.Fatalf("a bad name was accepted: %s", out)
	}
	if out, err := runRooms(t, "git", "repos", "rm", "github/o/r", "--atrium-db", db); err != nil {
		t.Fatalf("rm: %v %s", err, out)
	}
	if out, _ := runRooms(t, "git", "repos", "ls", "--atrium-db", db); !strings.Contains(out, "no repositories") {
		t.Fatalf("still listed: %s", out)
	}
}

// No verb takes a branch, a url or a refspec.
func TestNoGitVerbTakesABranchURLOrRefspec(t *testing.T) {
	c := roomGitCmd("atrium-")
	for _, sub := range c.Commands() {
		for _, banned := range []string{"branch", "url", "ref", "path", "remote"} {
			if f := sub.Flags().Lookup(banned); f != nil {
				t.Errorf("git %s has --%s", sub.Name(), banned)
			}
		}
	}
	repos := gitReposAddCmd("atrium-")
	if f := repos.Flags().Lookup("branch"); f != nil {
		t.Error("repos add takes a branch")
	}
}

func TestSyncAndCollectTalkToTheHubAndReportNotOK(t *testing.T) {
	var got []map[string]any
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
		switch r.URL.Path {
		case "/_hub/git/sync":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
				{"room": "sg3", "name": "github/o/r", "state": "absent", "detail": "no clone"}}})
		case "/_hub/git/collect":
			_ = json.NewEncoder(w).Encode(map[string]any{"room": "sg3", "repos": []map[string]any{
				{"name": "github/o/r", "moved": true, "delivered": true, "refs": 2}}})
		}
	}))
	defer hub.Close()
	addr := strings.TrimPrefix(hub.URL, "http://")

	out, err := runRooms(t, "git", "sync", "sg3", "--init", "--atrium-board-addr", addr)
	if err == nil || !strings.Contains(out, "absent") {
		t.Fatalf("sync: err=%v out=%s", err, out)
	}
	if got[0]["room"] != "sg3" || got[0]["init"] != true {
		t.Fatalf("hub was sent %v", got[0])
	}
	out, err = runRooms(t, "git", "collect", "sg3", "--atrium-board-addr", addr)
	if err != nil || !strings.Contains(out, "2 branches") {
		t.Fatalf("collect: err=%v out=%s", err, out)
	}
}

func TestAHubThatPredatesGitSyncSaysSo(t *testing.T) {
	hub := httptest.NewServer(http.NotFoundHandler())
	defer hub.Close()
	_, err := runRooms(t, "git", "collect", "sg3", "--atrium-board-addr", strings.TrimPrefix(hub.URL, "http://"))
	if err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("err = %v", err)
	}
}
