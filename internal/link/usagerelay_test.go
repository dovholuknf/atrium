package link

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// A room's usage answer reaches the board whole for a `room~id` card: the hub
// retags the id and keeps totals and by_cause, so the peek's counts are the
// room's own and not zeros.
func TestAUsageReadIsRelayedWhole(t *testing.T) {
	usage := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"acard","context_now":61000,"totals":{"rows":2,"replies":9,"input":5,"output":700,"cache_write_5m":11,"cache_write_1h":0,"cache_read":4000},`+
			`"by_cause":{"operator":{"rows":2,"replies":9}},"rows":[]}`)
	})
	front, _, done := two(t, usage, holds("beta", "bcard"))
	defer done()

	res, err := http.Get(front.URL + "/v1/tasks/alpha~acard/usage?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var got struct {
		ContextNow int64                       `json:"context_now"`
		Totals     map[string]float64          `json:"totals"`
		ByCause    map[string]map[string]int64 `json:"by_cause"`
	}
	raw, _ := io.ReadAll(res.Body)
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%d %s", res.StatusCode, raw)
	}
	if got.ContextNow != 61000 || got.Totals["output"] != 700 || got.Totals["cache_read"] != 4000 ||
		got.ByCause["operator"]["replies"] != 9 {
		t.Fatalf("the usage read was cut on the way: %s", raw)
	}
}
