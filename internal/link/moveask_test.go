package link

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestAStopBeforeTheParkHasTheDriverUndoTheMove(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	checking, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	var asked []string
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/v1/tasks/c1":
			_, _ = w.Write([]byte(`{"id":"c1"}`))
		case "/v1/move/check":
			close(checking)
			<-release
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer board.Close()
	c := &controlMCP{board: board.URL}

	ans := c.moveAsk("sgg", http.MethodPost, hubMovePath, []byte(`{"card":"c1","to":"sg4"}`))
	var st moveStatus
	if !ans.OK || json.Unmarshal(ans.Data, &st) != nil {
		t.Fatalf("start: %+v", ans)
	}
	<-checking
	if ans := c.moveAsk("sg4", http.MethodPost, hubMovePath+"/"+st.MoveID+"/undo", nil); ans.OK {
		t.Fatalf("the room it moves to stopped it: %+v", ans)
	}
	if ans := c.moveAsk("sgg", http.MethodPost, hubMovePath+"/"+st.MoveID+"/undo", nil); !ans.OK {
		t.Fatalf("stop: %+v", ans)
	}
	close(release)
	deadline := time.Now().Add(10 * time.Second)
	for st.State == "running" && time.Now().Before(deadline) {
		ans = c.moveAsk("sgg", http.MethodGet, hubMovePath+"/"+st.MoveID, nil)
		_ = json.Unmarshal(ans.Data, &st)
	}
	if st.State != "failed" || !strings.Contains(st.Error, "stopped on request") || st.Step != "undone" {
		t.Fatalf("a stopped move ended %+v", st)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, a := range asked {
		if strings.HasSuffix(a, "/gitpack") || strings.HasSuffix(a, "/park") {
			t.Fatalf("the driver went on after the stop: %v", asked)
		}
	}
	if ans := c.moveAsk("sgg", http.MethodPost, hubMovePath+"/"+st.MoveID+"/undo", nil); ans.OK {
		t.Fatalf("an ended move was stopped again: %+v", ans)
	}
}

func TestAnUndoAfterARestartUndoesFromTheRecordOnlyBeforeThePark(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer board.Close()
	c := &controlMCP{board: board.URL}
	dir := filepath.Join(home, ".atrium", "moves")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, step := range map[string]string{"mv-a": "frozen", "mv-b": "parked"} {
		raw, _ := json.Marshal(map[string]string{"id": id, "card": "c1", "from": "sgg", "to": "sg4", "step": step})
		if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if ans := c.moveAsk("sg4", http.MethodPost, hubMovePath+"/mv-a/undo", nil); ans.OK {
		t.Fatalf("another room undid it: %+v", ans)
	}
	if ans := c.moveAsk("sgg", http.MethodPost, hubMovePath+"/mv-b/undo", nil); ans.OK || ans.Code != http.StatusConflict {
		t.Fatalf("a parked move was undone: %+v", ans)
	}
	ans := c.moveAsk("sgg", http.MethodPost, hubMovePath+"/mv-a/undo", nil)
	var st moveStatus
	if !ans.OK || json.Unmarshal(ans.Data, &st) != nil || st.Step != "undone" || st.State != "failed" {
		t.Fatalf("undo: %+v %+v", ans, st)
	}
}
