//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestWithLauncherDept(t *testing.T) {
	for _, c := range []struct {
		name         string
		tags, parent []string
		want         []string
	}{
		{"passes the launcher's dept on", []string{"x"}, []string{"origin:agent", "dept:ui"}, []string{"x", "dept:ui"}},
		{"the caller's own dept wins", []string{"dept:runtime"}, []string{"dept:ui"}, []string{"dept:runtime"}},
		{"no dept on the launcher stamps none", []string{"x"}, []string{"origin:agent"}, []string{"x"}},
		{"an empty dept is not one", []string{"x"}, []string{"dept:"}, []string{"x"}},
		{"no launcher known", nil, nil, nil},
	} {
		if got := WithLauncherDept(c.tags, c.parent); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// The hub's atrium_launch gives the new card its launcher's dept, read off the launcher's card.
func TestHubLaunchStampsTheLaunchersDept(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "d1", "wire_name": "director", "status": "running", "tags": []string{"atrium:director", "dept:ui"}},
				{"id": "n1", "wire_name": "nodept", "status": "running", "tags": []string{"origin:agent"}},
			}})
		case r.URL.Path == "/v1/launch":
			_ = json.NewDecoder(r.Body).Decode(&got)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "wire_name": "kid"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	for _, k := range []struct {
		who  string
		in   []string
		want bool
	}{{"director", nil, true}, {"nodept", nil, false}, {"director", []string{"dept:runtime"}, false}} {
		if _, _, err := c.launchHandler(context.Background(), ctlReq(k.who, "beta"),
			launchInput{Cwd: "/work/dir", Tags: k.in}); err != nil {
			t.Fatal(err)
		}
		tags, _ := got["tags"].([]any)
		has := false
		for _, v := range tags {
			has = has || v == "dept:ui"
		}
		if has != k.want {
			t.Fatalf("launcher %s with tags %v: dept:ui stamped = %v, want %v (tags %v)", k.who, k.in, has, k.want, tags)
		}
	}
}

// launcherTags drops a `@room` suffix and the first dept tag is the one passed on.
func TestLauncherTagsHandlesAtRoomAndFirstDept(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
			{"id": "d1", "wire_name": "boss", "tags": []string{"dept:ui", "dept:runtime"}},
		}})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	tags := c.launcherTags(context.Background(), "beta", "boss@elsewhere")
	if asked != "name=boss" {
		t.Fatalf("asked %q, want the one card by bare name", asked)
	}
	if got := WithLauncherDept(nil, tags); !reflect.DeepEqual(got, []string{"dept:ui"}) {
		t.Fatalf("got %v, want the first dept only", got)
	}
}
