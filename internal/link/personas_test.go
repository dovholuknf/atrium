package link

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Each room has its own persona pack, so the catalog is merged with the room on
// every row and the ids left alone: two machines can both have go-security-
// reviewer, and the lessons view names the room in a header.
func TestThePersonaCatalogIsMergedAcrossRooms(t *testing.T) {
	pack := func(room string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/personas" {
				fmt.Fprintf(w, `{"personas":[{"id":"go-sec","name":%q}]}`, room)
				return
			}
			fmt.Fprintf(w, `{"served_by":%q}`, room)
		})
	}
	front, _, done := two(t, pack("alpha"), pack("beta"))
	defer done()

	res, err := http.Get(front.URL + "/v1/personas")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Personas []map[string]any `json:"personas"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Personas) != 2 {
		t.Fatalf("want one persona from each room, got %v", body.Personas)
	}
	for _, p := range body.Personas {
		if p["id"] != "go-sec" || p["room"] != p["name"] {
			t.Fatalf("row %v: the id must stay bare and the room must be on it", p)
		}
	}

	// The lessons view names its room and lands there.
	req, _ := http.NewRequest(http.MethodGet, front.URL+"/v1/personas/go-sec/lessons", nil)
	req.Header.Set(RoomHeader, "beta")
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	var got map[string]any
	_ = json.NewDecoder(res2.Body).Decode(&got)
	if got["served_by"] != "beta" {
		t.Fatalf("the lessons view went to %v", got)
	}
}
