package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// atrium_deploy. The room's side is tested in internal/daemon/roomhold_test.go.

type memSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSettings) HubSetting(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[name], nil
}

func (s *memSettings) SetHubSetting(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[name] = value
	return nil
}

// fakeDeployRoom is a room with a director, a worker and the owner, which holds
// in memory and counts what is said to anybody.
type fakeDeployRoom struct {
	mu    sync.Mutex
	held  map[string]any
	busy  []map[string]string
	says  []string
	start map[string]any
}

func (f *fakeDeployRoom) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "d1", "wire_name": "runtime", "status": "running"},
				{"id": "m1", "wire_name": "merge", "status": "running"},
				{"id": "w1", "wire_name": "worker", "status": "running"},
			}})
		case r.URL.Path == "/v1/hold" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"hold": f.held, "busy": f.busy, "quiet": len(f.busy) == 0})
		case r.URL.Path == "/v1/hold":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["action"] == "start" {
				f.start = in
				f.held = map[string]any{"id": "hold-1", "kind": "deploy"}
			} else {
				f.held = nil
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"hold": f.held})
		default:
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if s, _ := in["text"].(string); s != "" {
				f.says = append(f.says, r.URL.Path+" "+s)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "queued"})
		}
	})
}

func deployControl(t *testing.T, owner string) (*controlMCP, *fakeDeployRoom) {
	t.Helper()
	room := &fakeDeployRoom{}
	srv := httptest.NewServer(room.handler())
	t.Cleanup(srv.Close)
	st := &memSettings{m: map[string]string{}}
	if owner != "" {
		st.m[SettingDeployOwner] = owner
	}
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	c.settings = func() HubSettings { return st }
	return c, room
}

func deploy(t *testing.T, c *controlMCP, agent string, in deployInput) (deployOutput, error) {
	t.Helper()
	_, out, err := c.deployHandler(context.Background(), ctlReq(agent, "beta"), in)
	return out, err
}

func TestARequestWithNoOwnerIsRefused(t *testing.T) {
	c, _ := deployControl(t, "")
	_, err := deploy(t, c, "runtime", deployInput{Action: "request", Why: "r-hold-notices"})
	var ref *refusedError
	if !errors.As(err, &ref) || !strings.Contains(err.Error(), "no deploy owner is set") {
		t.Fatalf("answered %v", err)
	}
}

func TestTwoRequestsTellTheOwnerOnceAndMakeOneList(t *testing.T) {
	c, room := deployControl(t, "merge")
	out, err := deploy(t, c, "runtime", deployInput{Action: "request", Why: "r-hold-notices"})
	if err != nil || out.State != "requested" {
		t.Fatalf("first request: %+v %v", out, err)
	}
	out, err = deploy(t, c, "worker", deployInput{Action: "request", Why: "r-038"})
	if err != nil || out.State != "added" || len(out.Pending) != 2 {
		t.Fatalf("second request: %+v %v", out, err)
	}
	if len(room.says) != 1 || !strings.Contains(room.says[0], "deploy requested for beta by runtime") {
		t.Fatalf("the owner was told %v", room.says)
	}
}

func TestOnlyTheOwnerStartsAndTheHoldCarriesEveryReason(t *testing.T) {
	c, room := deployControl(t, "merge")
	if _, err := deploy(t, c, "runtime", deployInput{Action: "request", Why: "r-hold-notices"}); err != nil {
		t.Fatal(err)
	}
	if _, err := deploy(t, c, "runtime", deployInput{Action: "start"}); err == nil {
		t.Fatal("a director started a deploy")
	}
	out, err := deploy(t, c, "merge", deployInput{Action: "start", Exempt: []string{"watcher"}})
	if err != nil || out.State != "held" {
		t.Fatalf("start: %+v %v", out, err)
	}
	whys, _ := room.start["whys"].([]any)
	if room.start["by"] != "merge" || len(whys) != 1 || !strings.Contains(whys[0].(string), "r-hold-notices (runtime)") {
		t.Fatalf("the room was asked to hold with %v", room.start)
	}
	// A request during the hold is the next one, not part of this deploy.
	out, err = deploy(t, c, "worker", deployInput{Action: "request", Why: "late"})
	if err != nil || out.State != "next" {
		t.Fatalf("request during the hold: %+v %v", out, err)
	}
}

func TestWaitAnswersQuietOrTheBusyList(t *testing.T) {
	c, room := deployControl(t, "merge")
	if _, err := deploy(t, c, "merge", deployInput{Action: "start"}); err != nil {
		t.Fatal(err)
	}
	room.mu.Lock()
	room.busy = []map[string]string{{"card": "w1", "title": "worker"}}
	room.mu.Unlock()
	out, err := deploy(t, c, "merge", deployInput{Action: "wait", MaxSeconds: 1})
	if err != nil || out.State != "busy" || len(out.Busy) != 1 {
		t.Fatalf("busy wait: %+v %v", out, err)
	}
	room.mu.Lock()
	room.busy = nil
	room.mu.Unlock()
	out, err = deploy(t, c, "merge", deployInput{Action: "wait", MaxSeconds: 1})
	if err != nil || out.State != "quiet" {
		t.Fatalf("quiet wait: %+v %v", out, err)
	}
}

func TestCancelPutsTheRequestsBack(t *testing.T) {
	c, room := deployControl(t, "merge")
	if _, err := deploy(t, c, "runtime", deployInput{Action: "request", Why: "r-hold-notices"}); err != nil {
		t.Fatal(err)
	}
	if _, err := deploy(t, c, "merge", deployInput{Action: "start"}); err != nil {
		t.Fatal(err)
	}
	out, err := deploy(t, c, "merge", deployInput{Action: "cancel", Why: "build broke"})
	if err != nil || out.State != "cancelled" || len(out.Pending) != 1 || room.held != nil {
		t.Fatalf("cancel: %+v %v, room held %v", out, err, room.held)
	}
}

func TestADeployThatEndedIsSettledOnTheNextLook(t *testing.T) {
	c, room := deployControl(t, "merge")
	if _, err := deploy(t, c, "runtime", deployInput{Action: "request", Why: "r-hold-notices"}); err != nil {
		t.Fatal(err)
	}
	if _, err := deploy(t, c, "merge", deployInput{Action: "start"}); err != nil {
		t.Fatal(err)
	}
	// The room restarted and lifted its own hold.
	room.mu.Lock()
	room.held = nil
	room.mu.Unlock()
	out, err := deploy(t, c, "runtime", deployInput{Action: "status"})
	if err != nil || out.State != "idle" || len(out.Running) != 0 {
		t.Fatalf("status after the deploy: %+v %v", out, err)
	}
}

func TestAWorkerDoesNotSeeTheDeployTool(t *testing.T) {
	if inClass("atrium_deploy", classWorker) {
		t.Fatal("atrium_deploy is in the worker set")
	}
}
