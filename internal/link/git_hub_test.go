package link

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

func gitPost(t *testing.T, url string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestTheHubGitRoutesAre404UntilWired(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	if code, _ := gitPost(t, x.board+"/_hub/git/sync", map[string]any{"room": "sg4"}); code != 404 {
		t.Fatalf("unwired = %d", code)
	}
}

func TestTheHubGitRoutesAnswerAndRefuseWhatTheyShould(t *testing.T) {
	x := newRelayPair(t)
	defer x.stop()
	g := &gitsync.Hub{Dir: t.TempDir(), Rooms: x.hub.GitRooms(),
		Repos: func() ([]gitsync.Repo, error) { return nil, nil }, Runner: gitsync.NewRunner()}
	x.proxy.SetGit(g)

	// The fake rooms never said Git, so a sync of every repository (none) is empty and fine.
	code, out := gitPost(t, x.board+"/_hub/git/sync", map[string]any{"room": "sg4"})
	if code != 200 {
		t.Fatalf("sync = %d %v", code, out)
	}
	// A name that is not in git_repos is refused, and so is a room that is not attached.
	if code, _ := gitPost(t, x.board+"/_hub/git/sync", map[string]any{"room": "sg4", "name": "github/o/r"}); code != 409 {
		t.Fatalf("unlisted name = %d", code)
	}
	if code, _ := gitPost(t, x.board+"/_hub/git/sync", map[string]any{"room": "nowhere"}); code != 409 {
		t.Fatalf("unattached room = %d", code)
	}
	// A room that never said Git cannot be collected from.
	if code, out := gitPost(t, x.board+"/_hub/git/collect", map[string]any{"room": "sg4"}); code != 409 ||
		!strings.Contains(out["error"].(string), "predates git sync") {
		t.Fatalf("collect = %d %v", code, out)
	}
	if code, _ := gitPost(t, x.board+"/_hub/git/collect", map[string]any{}); code != 400 {
		t.Fatalf("no room = %d", code)
	}
	res, err := http.Get(x.board + "/_hub/git/status")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status = %d", res.StatusCode)
	}
}

// No route and no tool takes a path, a url, a refspec or a branch.
func TestNoGitToolOrRouteTakesAPathURLRefspecOrBranch(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(gitSyncInput{}), reflect.TypeOf(gitCollectInput{})} {
		for i := 0; i < typ.NumField(); i++ {
			name := strings.ToLower(typ.Field(i).Name)
			for _, banned := range []string{"path", "url", "ref", "branch", "dir", "checkout", "remote"} {
				if strings.Contains(name, banned) {
					t.Errorf("%s has a %q field", typ.Name(), typ.Field(i).Name)
				}
			}
		}
	}
	x := newRelayPair(t)
	defer x.stop()
	// Unknown fields in a route body are ignored, not obeyed.
	g := &gitsync.Hub{Dir: t.TempDir(), Rooms: x.hub.GitRooms(),
		Repos: func() ([]gitsync.Repo, error) { return nil, nil }, Runner: gitsync.NewRunner()}
	x.proxy.SetGit(g)
	code, _ := gitPost(t, x.board+"/_hub/git/sync", map[string]any{"room": "sg4", "branch": "claude/ui", "url": "http://x", "path": "C:/"})
	if code != 200 {
		t.Fatalf("sync = %d", code)
	}
}
