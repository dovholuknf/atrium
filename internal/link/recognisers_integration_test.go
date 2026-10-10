//go:build integration

package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

func withSeed(t *testing.T, rows ...store.Recogniser) {
	t.Helper()
	was := recogniserSeed
	recogniserSeed = func() ([]store.Recogniser, error) { return append([]store.Recogniser(nil), rows...), nil }
	t.Cleanup(func() { recogniserSeed = was })
}

func seedRow(id string, rank int, pattern string) store.Recogniser {
	return store.Recogniser{ID: id, Label: id, Enabled: true, Rank: rank, Pattern: pattern, FetchArgs: []string{}}
}

func rowIDs(rows []*store.Recogniser) string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return strings.Join(ids, ",")
}

// SEEDED ONCE PER ROW: a new hub gets the built-in rows, an edit is never overwritten, a delete stays deleted, and a
// seed row added by a later build arrives once.
func TestTheHubSeedsItsRecognisersOncePerRow(t *testing.T) {
	st := &memSettings{m: map[string]string{}}
	withSeed(t, seedRow("pr", 10, `/pull/\d+`), seedRow("issue", 20, `/issues/\d+`))

	rows, err := hubRecogniserRows(st)
	if err != nil || rowIDs(rows) != "pr,issue" {
		t.Fatalf("first read = %s, %v", rowIDs(rows), err)
	}
	edited := seedRow("pr", 10, `/pull/(?P<num>\d+)`)
	edited.Label = "mine"
	if _, err := saveHubRecogniser(st, edited); err != nil {
		t.Fatal(err)
	}
	if err := deleteHubRecogniser(st, "issue"); err != nil {
		t.Fatal(err)
	}
	withSeed(t, seedRow("pr", 10, `/pull/\d+`), seedRow("issue", 20, `/issues/\d+`), seedRow("zendesk", 30, `/tickets/\d+`))
	rows, err = hubRecogniserRows(st)
	if err != nil || rowIDs(rows) != "pr,zendesk" {
		t.Fatalf("after a new build = %s, %v", rowIDs(rows), err)
	}
	if rows[0].Label != "mine" {
		t.Fatalf("the edit was overwritten: %+v", rows[0])
	}
}

// A SEEDED ROW HELD FROM BEFORE THE DEFAULT REPO gets the seed's once. One emptied after that stays empty.
func TestTheHubBackfillsTheDefaultRepoOnce(t *testing.T) {
	st := &memSettings{m: map[string]string{}}
	withSeed(t, seedRow("zendesk", 30, `/tickets/\d+`))
	if _, err := hubRecogniserRows(st); err != nil {
		t.Fatal(err)
	}
	delete(st.m, SettingRecognisersBackfill)
	zd := seedRow("zendesk", 30, `/tickets/\d+`)
	zd.DefaultRepo = "github.com/openziti/ziti"
	withSeed(t, zd)
	rows, err := hubRecogniserRows(st)
	if err != nil || len(rows) != 1 || rows[0].DefaultRepo != "github.com/openziti/ziti" {
		t.Fatalf("the backfill = %v %+v", err, rows)
	}
	again, _ := hubRecogniserRows(st)
	if again[0].DefaultRepo != "github.com/openziti/ziti" {
		t.Fatalf("the backfill was not written: %+v", again[0])
	}
	emptied := *again[0]
	emptied.DefaultRepo = ""
	if _, err := saveHubRecogniser(st, emptied); err != nil {
		t.Fatal(err)
	}
	if rows, _ := hubRecogniserRows(st); rows[0].DefaultRepo != "" {
		t.Errorf("an emptied default repo came back: %+v", rows[0])
	}
}

// A ROW THAT DOES NOT COMPILE IS REFUSED at save, on the hub as on a room.
func TestTheHubRefusesARecogniserThatDoesNotCompile(t *testing.T) {
	withSeed(t)
	if _, err := saveHubRecogniser(&memSettings{m: map[string]string{}}, seedRow("bad", 1, `(`)); err == nil {
		t.Fatal("a bad pattern was saved")
	}
}

// THE BOARD'S RECOGNISER ROUTES ARE THE HUB'S in every view: listed, saved and removed with no room named.
func TestTheBoardEditsTheHubsRecognisers(t *testing.T) {
	withSeed(t, seedRow("pr", 10, `/pull/\d+`))
	x := newClaimHub(t, map[string]int{"alpha": 0, "beta": 1})
	defer x.done()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)

	call := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, x.front.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := call(http.MethodGet, "/v1/recognisers", ""); code != 200 || !strings.Contains(body, `"id":"pr"`) {
		t.Fatalf("list = %d %s", code, body)
	}
	if code, body := call(http.MethodPut, "/v1/recognisers/ticket", `{"pattern":"/tickets/\\d+","enabled":true,"rank":5}`); code != 200 {
		t.Fatalf("save = %d %s", code, body)
	}
	if code, body := call(http.MethodPut, "/v1/recognisers/bad", `{"pattern":"("}`); code != 400 {
		t.Fatalf("a bad save = %d %s", code, body)
	}
	if code, body := call(http.MethodDelete, "/v1/recognisers/pr", ""); code != 204 {
		t.Fatalf("delete = %d %s", code, body)
	}
	code, body := call(http.MethodGet, "/v1/recognisers", "")
	var got HubRecognisers
	if code != 200 || json.Unmarshal([]byte(body), &got) != nil || rowIDs(got.Recognisers) != "ticket" {
		t.Fatalf("list after = %d %s", code, body)
	}
}

// THE BUG THIS FIXES (u-pulls-no-recogniser-on-hub): no room has a recogniser row, and a GitHub PR pasted on the hub
// with no room named is recognised from the hub's rows and lands on the least busy room, with and without the
// changes or files tab on the end.
func TestAPRPastedOnTheHubIsRecognisedOnARoomWithNoRows(t *testing.T) {
	seed := seedRow("github-pull-request", 10,
		`^https?://(?P<host>github\.com)/(?P<org>[A-Za-z0-9_.-]+)/(?P<repo>[A-Za-z0-9_.-]+)/pull/(?P<num>\d+)(?:[/?#].*)?$`)
	withSeed(t, seed)
	x := newClaimHub(t, map[string]int{"alpha": 2, "beta": 0})
	defer x.done()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)
	for _, cr := range x.rooms {
		cr.recognise = true
	}
	for i, url := range []string{
		"https://github.com/openziti/ziti-console/pull/967/changes",
		"https://github.com/openziti/ziti-console/pull/968",
		"https://github.com/openziti/ziti-console/pull/969/files",
	} {
		code, body := x.pasteVia(t, "", url)
		if code != http.StatusCreated {
			t.Fatalf("%s: %d %s", url, code, body)
		}
		if got := x.rooms["beta"].made(); len(got) != i+1 {
			t.Fatalf("%s: beta made %v", url, got)
		}
	}
	if got := x.rooms["alpha"].made(); len(got) != 0 {
		t.Fatalf("alpha made %v", got)
	}
	code, body := x.pasteVia(t, "", "https://example.com/not/a/pr")
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "no recogniser") {
		t.Fatalf("a link nothing matches = %d %s", code, body)
	}
}

// A ROOM READS THE HUB'S ROWS over its link, through the forge route.
func TestARoomReadsTheHubsRecognisersOverItsLink(t *testing.T) {
	withSeed(t, seedRow("pr", 10, `/pull/\d+`))
	x := newClaimHub(t, map[string]int{"alpha": 0})
	defer x.done()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)
	var got HubRecognisers
	if err := x.rooms["alpha"].room.Forge(context.Background(), forge.HubRecognisersPath, struct{}{}, &got); err != nil {
		t.Fatal(err)
	}
	if rowIDs(got.Recognisers) != "pr" {
		t.Fatalf("rows = %s", rowIDs(got.Recognisers))
	}
}
