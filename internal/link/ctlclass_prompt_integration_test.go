//go:build integration

package link

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// Every tool the worker and interviewer prompts name, and atrium_publish, is in the worker set of the HTTP control
// server, for the plain and the resident worker.
func TestWorkerClassesServeEveryToolTheLeanPromptsName(t *testing.T) {
	h := newClassHarness(t, classTasks)
	want := append(daemon.PromptToolNames(), "atrium_publish")
	for _, agent := range []string{"worker1"} {
		have := map[string]bool{}
		for _, n := range toolNames(t, h.connect(t, agent, "alpha")) {
			have[n] = true
		}
		for _, name := range want {
			if !have[name] {
				t.Errorf("%s lacks %s", agent, name)
			}
		}
	}
	for _, c := range []ctl.Class{ctl.ClassWorker, ctl.ClassWorkerResident} {
		for _, name := range want {
			if !inClass(name, c) {
				t.Errorf("class %d lacks %s", c, name)
			}
		}
	}
}

// A room's stdio atrium_publish reaches the hub as a relayed ask and lands as a document by that room's card.
func TestRoomPublishRelayedThroughHubAsk(t *testing.T) {
	h := newPublishHarness(t)
	if !hubAskPath(hubPublishPath) {
		t.Fatal("the publish path is not relayable")
	}
	ans := h.c.hubAsk(context.Background(), "alpha", RelayRequest{Op: "hub", From: "r-031", Method: "POST",
		Path: hubPublishPath, Body: json.RawMessage(`{"title":"t","content":"hello"}`)})
	if !ans.OK {
		t.Fatalf("answer = %+v", ans)
	}
	var out publishOutput
	if err := json.Unmarshal(ans.Data, &out); err != nil || out.Slug == "" {
		t.Fatalf("data = %s, %v", ans.Data, err)
	}
	bad := h.c.hubAsk(context.Background(), "alpha", RelayRequest{Op: "hub", From: "r-031", Method: "POST",
		Path: hubPublishPath, Body: json.RawMessage(`{"content":"x"}`)})
	if bad.OK {
		t.Fatal("a new document without a title was accepted")
	}
}
