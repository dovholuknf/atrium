package render

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAFindingStillReadsTheOldLeftForKey(t *testing.T) {
	for _, raw := range []string{`{"sev":"nit","left_for_clint":true}`, `{"sev":"nit","left_for_operator":true}`} {
		var f Finding
		if err := json.Unmarshal([]byte(raw), &f); err != nil || !f.LeftForOperator || f.Sev != "nit" {
			t.Errorf("%s: %+v %v", raw, f, err)
		}
	}
	out, _ := json.Marshal(Finding{LeftForOperator: true})
	if s := string(out); !strings.Contains(s, `"left_for_operator":true`) || strings.Contains(s, "left_for_clint") {
		t.Errorf("written as %s", out)
	}
}
