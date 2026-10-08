package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The hub reads CI for its rooms: every action through a fake gh, no network and no real repository.

type ciHub struct {
	p    *Proxy
	cmds []forge.Cmd
	err  error
	logs string
	dir  string
}

func newCIHub(t *testing.T) *ciHub {
	t.Helper()
	x := &ciHub{p: NewProxy(NewHub(Timings{}), nil, "", nil), dir: t.TempDir()}
	run := func(_ context.Context, c forge.Cmd) ([]byte, error) {
		x.cmds = append(x.cmds, c)
		if x.err != nil {
			return nil, x.err
		}
		a := strings.Join(c.Args, " ")
		switch {
		case strings.HasPrefix(a, "run list"):
			return []byte(`[{"databaseId":7,"workflowName":"ci","status":"completed","conclusion":"failure","headSha":"abc"}]`), nil
		case strings.HasPrefix(a, "run view") && strings.Contains(a, "--json"):
			return []byte(`{"databaseId":7,"jobs":[{"databaseId":8,"name":"win","steps":[{"number":1,"name":"t","conclusion":"failure"}]}]}`), nil
		case strings.HasPrefix(a, "run view"):
			w := &forge.TailWriter{Lines: c.Tail, Bytes: c.Limit}
			_, _ = w.Write([]byte(x.logs))
			text, _, _ := w.Result()
			return []byte(text), nil
		case strings.HasPrefix(a, "api"):
			return []byte(`{"artifacts":[{"id":1,"name":"ci","size_in_bytes":5}]}`), nil
		case strings.HasPrefix(a, "run download"):
			_ = os.WriteFile(filepath.Join(c.Args[len(c.Args)-1], "out.txt"), []byte("one\ntwo\n"), 0o644)
			return nil, nil
		}
		return nil, errors.New("unexpected " + a)
	}
	x.p.SetForge(&memSettings{m: map[string]string{}}, run)
	x.p.SetCIDir(x.dir)
	return x
}

func (x *ciHub) room(a forge.HubCIAsk) (int, string) {
	raw, _ := json.Marshal(a)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://hub"+forge.HubCIPath, strings.NewReader(string(raw)))
	req.Header.Set(ForgeRoomHeader, "alpha")
	x.p.serveForge(rec, req)
	return rec.Code, rec.Body.String()
}

func ci(action string) forge.HubCIAsk {
	return forge.HubCIAsk{Host: "github.com", Org: "o", Repo: "r", Action: action}
}

func TestTheHubAnswersEveryCIActionForARoom(t *testing.T) {
	x := newCIHub(t)
	x.logs = strings.Repeat("l\n", 5000) + "last\n"
	for _, c := range []struct {
		ask  forge.HubCIAsk
		want string
	}{
		{forge.HubCIAsk{Host: "github.com", Org: "o", Repo: "r", Action: "runs", Branch: "b"}, `"workflow":"ci"`},
		{forge.HubCIAsk{Host: "github.com", Org: "o", Repo: "r", Action: "run", RunID: 7}, `"name":"win"`},
		{forge.HubCIAsk{Host: "github.com", Org: "o", Repo: "r", Action: "log", RunID: 7, Tail: 20}, `"truncated":true`},
		{forge.HubCIAsk{Host: "github.com", Org: "o", Repo: "r", Action: "artifacts", RunID: 7}, `"size_bytes":5`},
		{forge.HubCIAsk{Host: "github.com", Org: "o", Repo: "r", Action: "artifact", RunID: 7, Name: "ci", File: "out.txt"}, `"text":"one\ntwo\n"`},
	} {
		code, body := x.room(c.ask)
		if code != 200 || !strings.Contains(body, c.want) || !strings.Contains(body, `"kind":"github"`) {
			t.Errorf("%s: %d %s", c.ask.Action, code, body)
		}
	}
	// A run's log is read failed-only, and the tail is the one asked for.
	var log forge.Cmd
	for _, c := range x.cmds {
		if strings.Contains(strings.Join(c.Args, " "), "--log") {
			log = c
		}
	}
	if !strings.Contains(strings.Join(log.Args, " "), "--log-failed") || log.Tail != 20 {
		t.Errorf("log cmd = %+v", log)
	}
	// The artifact is on the hub's disk under its folder.
	if _, err := os.Stat(filepath.Join(x.dir, "github.com", "o", "r", "7", "ci", "out.txt")); err != nil {
		t.Error(err)
	}
}

func TestACIQuestionIsChecked(t *testing.T) {
	x := newCIHub(t)
	for _, a := range []forge.HubCIAsk{
		ci("runs-and-more"), ci("run"), ci("log"), ci("artifact"),
		{Host: "github.com", Org: "../x", Repo: "r", Action: "runs"},
		{Host: "", Org: "o", Repo: "r", Action: "runs"},
	} {
		if code, body := x.room(a); code != 400 {
			t.Errorf("%+v: %d %s", a, code, body)
		}
	}
	if len(x.cmds) != 0 {
		t.Errorf("ran %v for a bad question", x.cmds)
	}
}

func TestAMissingHubLoginAnswersTheCommandToRunOnTheHub(t *testing.T) {
	x := newCIHub(t)
	x.err = errors.New("gh run list: To get started with GitHub CLI, please run:  gh auth login")
	a := ci("runs")
	code, body := x.room(a)
	if code != http.StatusBadGateway || !strings.Contains(body, `"code":"access"`) ||
		!strings.Contains(body, "gh auth login --hostname github.com` on the hub") {
		t.Errorf("%d %s", code, body)
	}
	x.err = &exec.Error{Name: "gh", Err: exec.ErrNotFound}
	if code, body = x.room(a); code != http.StatusBadGateway || !strings.Contains(body, "not installed on the hub") {
		t.Errorf("%d %s", code, body)
	}
}

func TestCIOnBitbucketSaysNotSupported(t *testing.T) {
	x := newCIHub(t)
	a := ci("runs")
	a.Host = "bitbucket.org"
	code, body := x.room(a)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "not supported on bitbucket") ||
		!strings.Contains(body, `"code":"not_supported"`) || len(x.cmds) != 0 {
		t.Errorf("%d %s %v", code, body, x.cmds)
	}
}

func TestTheHubsOwnBoardRouteAnswersCIToo(t *testing.T) {
	x := newCIHub(t)
	rec := httptest.NewRecorder()
	raw, _ := json.Marshal(ci("runs"))
	req := httptest.NewRequest(http.MethodPost, "http://hub/_hub/forge/ci", strings.NewReader(string(raw)))
	x.p.serveForgeAdmin(rec, req, "ci")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"head_sha":"abc"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

func TestCIRepoNames(t *testing.T) {
	for in, want := range map[string]string{"o/r": "github.com o r", "github.com/o/r": "github.com o r",
		"https://git.x.io/o/r.git": "git.x.io o r"} {
		h, o, r, err := ciRepo(in)
		if err != nil || h+" "+o+" "+r != want {
			t.Errorf("%s: %s %s %s %v", in, h, o, r, err)
		}
	}
	if _, _, _, err := ciRepo("atrium"); err == nil {
		t.Error("a bare name")
	}
}

func TestTheHubControlMCPOffersCIToWorkersAndFull(t *testing.T) {
	for _, class := range []ctlClass{classFull, classWorker} {
		s := mcp.NewServer(&mcp.Implementation{Name: "t"}, nil)
		(&controlMCP{}).registerGit(s, class)
		ct, st := mcp.NewInMemoryTransports()
		if _, err := s.Connect(context.Background(), st, nil); err != nil {
			t.Fatal(err)
		}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "c"}, nil).Connect(context.Background(), ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, _ := cs.ListTools(context.Background(), nil)
		found := false
		for _, tl := range res.Tools {
			found = found || tl.Name == "atrium_ci"
		}
		if !found {
			t.Errorf("class %d lacks atrium_ci", class)
		}
		_ = cs.Close()
	}
}
