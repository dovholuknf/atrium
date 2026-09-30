package link

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestTheGitToolsAreAudited(t *testing.T) {
	var rows []string
	c := &controlMCP{audit: func(room, kind, detail string) { rows = append(rows, room+"|"+kind+"|"+detail) }}
	sync := audited(c, "ctl-git-sync", describeGitSync, func(context.Context, *mcp.CallToolRequest, gitSyncInput) (
		*mcp.CallToolResult, gitSyncOutput, error) {
		return nil, gitSyncOutput{}, nil
	})
	if _, _, err := sync(context.Background(), nil, gitSyncInput{Room: "sg3", Name: "github/o/r", Init: true}); err != nil {
		t.Fatal(err)
	}
	col := audited(c, "ctl-git-collect", describeGitCollect, func(context.Context, *mcp.CallToolRequest, gitCollectInput) (
		*mcp.CallToolResult, gitsync.CollectResult, error) {
		return nil, gitsync.CollectResult{}, nil
	})
	if _, _, err := col(context.Background(), nil, gitCollectInput{Room: "sg3"}); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "sg3|ctl-git-sync|") || !strings.Contains(rows[0], "git sync github/o/r (init)") ||
		!strings.HasPrefix(rows[1], "sg3|ctl-git-collect|") {
		t.Fatalf("rows = %v", rows)
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
