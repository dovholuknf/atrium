//go:build integration

package link

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// @runtime adds GET /v1/tasks/{id}/replies?n= on the room. The hub already
// routes /v1/tasks/<id>/... to the card's room, and this locks in that the
// query string survives the hop, for a tagged id and for a bare one.
func TestRepliesReachTheOwningRoomWithTheQueryIntact(t *testing.T) {
	room := func(name string, id string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/tasks/"+id {
				fmt.Fprint(w, `{"id":"`+id+`"}`)
				return
			}
			if r.URL.Path != "/v1/tasks/"+id+"/replies" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":"no"}`)
				return
			}
			fmt.Fprintf(w, `{"served_by":%q,"path":%q,"query":%q}`, name, r.URL.Path, r.URL.RawQuery)
		})
	}
	front, _, done := two(t, room("alpha", "acard"), room("beta", "bcard"))
	defer done()
	for _, path := range []string{"/v1/tasks/beta~bcard/replies?n=3", "/v1/tasks/bcard/replies?n=3"} {
		checkReplies(t, front.URL, path, "n=3")
	}
	// r-replies-paging: `before` rides along with `n`, colons and all.
	for _, path := range []string{
		"/v1/tasks/beta~bcard/replies?n=50&before=2026-09-30T08:00:00.5Z",
		"/v1/tasks/bcard/replies?n=50&before=2026-09-30T08:00:00.5Z",
	} {
		checkReplies(t, front.URL, path, "n=50&before=2026-09-30T08:00:00.5Z")
	}
}

func checkReplies(t *testing.T, base, path, wantQuery string) {
	t.Helper()
	{
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var plain map[string]string
		_ = json.Unmarshal(raw, &plain)
		if res.StatusCode != http.StatusOK || plain["served_by"] != "beta" ||
			plain["path"] != "/v1/tasks/bcard/replies" || plain["query"] != wantQuery {
			t.Fatalf("%s answered %d %s", path, res.StatusCode, raw)
		}
	}
}

// r-changes: GET /v1/tasks/{id}/changes reaches the owning room with its query intact, for a tagged
// id and a bare one, `against` and `turn` alike.
func TestChangesReachTheOwningRoomWithTheQueryIntact(t *testing.T) {
	room := func(name string, id string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/tasks/"+id {
				fmt.Fprint(w, `{"id":"`+id+`"}`)
				return
			}
			if r.URL.Path != "/v1/tasks/"+id+"/changes" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":"no"}`)
				return
			}
			fmt.Fprintf(w, `{"served_by":%q,"path":%q,"query":%q}`, name, r.URL.Path, r.URL.RawQuery)
		})
	}
	front, _, done := two(t, room("alpha", "acard"), room("beta", "bcard"))
	defer done()
	for _, q := range []string{"against=base", "turn=2026-09-30T08:00:00.5Z"} {
		for _, path := range []string{"/v1/tasks/beta~bcard/changes?" + q, "/v1/tasks/bcard/changes?" + q} {
			res, err := http.Get(front.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := io.ReadAll(res.Body)
			res.Body.Close()
			var plain map[string]string
			_ = json.Unmarshal(raw, &plain)
			if res.StatusCode != http.StatusOK || plain["served_by"] != "beta" ||
				plain["path"] != "/v1/tasks/bcard/changes" || plain["query"] != q {
				t.Fatalf("%s answered %d %s", path, res.StatusCode, raw)
			}
		}
	}
}
