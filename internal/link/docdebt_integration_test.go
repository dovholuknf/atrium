//go:build integration

package link

import (
	"encoding/json"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// GET /_hub/doc-debt answers the meter, is read only, and is absent until a backlog is wired.
func TestDocDebtEndpointAnswersTheMeter(t *testing.T) {
	x := newBacklogRig(t)
	body := "feature: f-one\nstale: README.md says daemon\n"
	if code, out := x.send("POST", "/_hub/backlog", `{"dept":"docs","title":"docs behind f-one","body":`+jsonString(body)+`,"by":"t"}`, "127.0.0.1:5000"); code != 201 {
		t.Fatalf("file: %d %s", code, out)
	}
	code, out := x.send("GET", "/_hub/doc-debt", "", "10.0.0.9:5000")
	if code != 200 {
		t.Fatalf("get: %d %s", code, out)
	}
	var m hubstore.DocDebtMeter
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	if m.Items != 1 || m.Pages != 1 || m.Crossed || m.ThresholdItems != 8 || m.ThresholdDays != 10 || m.Counted[0].From != hubstore.DebtFromFiling {
		t.Fatalf("meter = %+v", m)
	}
	if code, _ := x.send("POST", "/_hub/doc-debt", "{}", "127.0.0.1:5000"); code != 405 {
		t.Fatalf("a POST: %d", code)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
