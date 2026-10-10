//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
)

func TestTheInterviewTagIsTheOneTheRoomReads(t *testing.T) {
	if InterviewTag != daemon.InterviewTag {
		t.Fatalf("hub says %q, room reads %q", InterviewTag, daemon.InterviewTag)
	}
}

type launchBody struct {
	Prompt  string   `json:"prompt"`
	Brief   string   `json:"brief"`
	Tags    []string `json:"tags"`
	Scratch bool     `json:"scratch"`
}

func launchSeen(t *testing.T, in launchInput) (launchBody, error) {
	t.Helper()
	var got launchBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/tasks" {
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{}})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "wire_name": "kid"})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"), in)
	return got, err
}

func TestAnInterviewLaunchWritesTheRulesAheadOfTheTopic(t *testing.T) {
	got, err := launchSeen(t, launchInput{Cwd: "/work/dir", Kind: "interview", Brief: "Topic: the new board"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasTag(got.Tags, InterviewTag) {
		t.Errorf("tags = %v, want %s", got.Tags, InterviewTag)
	}
	rules := strings.Index(got.Brief, "YOUR REPLY IS THE QUESTION")
	topic := strings.Index(got.Brief, "Topic: the new board")
	if rules < 0 || topic < 0 || rules > topic {
		t.Errorf("the brief has the rules at %d and the topic at %d:\n%s", rules, topic, got.Brief)
	}
	if strings.Contains(got.Prompt, "atrium_blocked") {
		t.Errorf("an interviewer was told to end each turn with a report: %q", got.Prompt)
	}
}

func TestAnOrdinaryLaunchIsNotAnInterview(t *testing.T) {
	got, err := launchSeen(t, launchInput{Cwd: "/work/dir", Brief: "do it", Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if hasTag(got.Tags, InterviewTag) || got.Brief != "do it" || !strings.Contains(got.Prompt, "atrium_done") {
		t.Errorf("an ordinary launch changed: %+v", got)
	}
}

func TestAnUnknownKindIsRefused(t *testing.T) {
	if _, err := launchSeen(t, launchInput{Cwd: "/work/dir", Kind: "poem"}); err == nil ||
		!strings.Contains(err.Error(), "interview") {
		t.Fatalf("err = %v, want a refusal naming the kind there is", err)
	}
}

func TestScratchIsPassedToTheRoom(t *testing.T) {
	got, err := launchSeen(t, launchInput{Cwd: "/work/dir", Scratch: true})
	if err != nil || !got.Scratch {
		t.Fatalf("scratch = %v, err = %v", got.Scratch, err)
	}
}
