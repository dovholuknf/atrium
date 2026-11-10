package cli

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestFakeRoomAnswersValidateAgainstTheAnswerSchemas(t *testing.T) {
	room := newKitRoom(scenarioOK)
	defer room.close()
	for path, schema := range map[string]string{"/session": "session-answer", "/permission": "permission-answer",
		"/stop": "stop-answer", "/finish": "finish-answer", "/contract": "contract-answer"} {
		resp, err := http.Post(room.srv.URL+path, "application/json", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		resp.Body.Close()
		if faults := validateAgainst(schema, b.Bytes()); len(faults) != 0 {
			t.Errorf("%s: %v", path, faults)
		}
	}
	resp, err := http.Get(room.srv.URL + "/gate")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	_, _ = b.ReadFrom(resp.Body)
	resp.Body.Close()
	if faults := validateAgainst("gate-answer", b.Bytes()); len(faults) != 0 {
		t.Errorf("/gate: %v", faults)
	}
}

func TestValidateAgainstNamesTheFault(t *testing.T) {
	for _, tc := range []struct{ schema, body, want string }{
		{"session", `{"event":"start"}`, "none of the alternatives"},
		{"session", `{"agent":"a","event":"start","features":["warp"]}`, "/features/0"},
		{"permission", `{"agent":"a"}`, `missing "command"`},
		{"stop", `{"agent":"a","questions":null,"own_turn":true}`, ""},
		{"stop", `{"agent":"a","own_turn":"yes"}`, "/own_turn"},
		{"session", `not json`, "not JSON"},
	} {
		got := strings.Join(validateAgainst(tc.schema, []byte(tc.body)), "\n")
		if tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
			t.Errorf("%s %s: %q, want %q", tc.schema, tc.body, got, tc.want)
		}
	}
}
