package link

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAMoveIsStartedFromTheAskingRoomAndFollowedOnlyByItsRooms(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"no card ` + r.Header.Get("X-Atrium-Room") + `"}`))
	}))
	defer board.Close()
	c := &controlMCP{board: board.URL}

	if ans := c.moveAsk("sgg", http.MethodPost, hubMovePath, []byte(`{"card":"c1","to":"SGG"}`)); ans.OK {
		t.Fatalf("a move to its own room started: %+v", ans)
	}
	if ans := c.moveAsk("sgg", http.MethodGet, hubMovePath, nil); ans.OK {
		t.Fatalf("a GET started a move: %+v", ans)
	}
	ans := c.moveAsk("sgg", http.MethodPost, hubMovePath, []byte(`{"card":"c1","to":"sg4","from":"other"}`))
	var st moveStatus
	if !ans.OK || json.Unmarshal(ans.Data, &st) != nil || st.MoveID == "" || st.From != "sgg" || st.To != "sg4" {
		t.Fatalf("start: %+v %+v", ans, st)
	}
	if ans := c.moveAsk("laptop", http.MethodGet, hubMovePath+"/"+st.MoveID, nil); ans.OK {
		t.Fatalf("a third room read the move: %+v", ans)
	}
	deadline := time.Now().Add(10 * time.Second)
	for st.State == "running" && time.Now().Before(deadline) {
		ans = c.moveAsk("sg4", http.MethodGet, hubMovePath+"/"+st.MoveID, nil)
		if !ans.OK || json.Unmarshal(ans.Data, &st) != nil {
			t.Fatalf("follow: %+v", ans)
		}
	}
	if st.State != "refused" || st.Error == "" {
		t.Fatalf("a move the source room refused ended %+v", st)
	}
}
