package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

// The stdio atrium_launch records the same lineage the hub's does, for the parts that are this room's.
// Without it the room filed every stdio launch as the board's own dialog (`@human`) and the worker had
// no launcher. See launchHandler.

func stdioLaunchBody(t *testing.T, agentName string, in LaunchInput) map[string]any {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "card1", "wire_name": "kid"})
	}))
	defer srv.Close()
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	t.Setenv("ATRIUM_AGENT_NAME", agentName)
	if _, _, err := launchHandler(context.Background(), nil, in); err != nil {
		t.Fatal(err)
	}
	return got
}

func tagsOf(body map[string]any) []string {
	var out []string
	if l, ok := body["tags"].([]any); ok {
		for _, v := range l {
			out = append(out, v.(string))
		}
	}
	return out
}

func hasTagIn(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func TestAStdioLaunchRecordsItsLauncherAndIsAWorker(t *testing.T) {
	body := stdioLaunchBody(t, "fabric-director", LaunchInput{Cwd: "/w", Prompt: "do the thing", Tags: []string{"mine"}})
	if body["spawned_by"] != "fabric-director" {
		t.Fatalf("spawned_by = %v", body["spawned_by"])
	}
	tags := tagsOf(body)
	for _, want := range []string{"mine", link.OriginTag, link.SubagentTag} {
		if !hasTagIn(tags, want) {
			t.Fatalf("tags %v lack %q", tags, want)
		}
	}
	if p, _ := body["prompt"].(string); !strings.HasPrefix(p, "do the thing") || !strings.Contains(p, "atrium_say") ||
		strings.Contains(p, "atrium_report") {
		t.Fatalf("the prompt lacks the report line: %q", p)
	}
}

func TestAStdioLaunchOfADirectorIsNotMarkedASubagent(t *testing.T) {
	for _, tag := range []string{link.DirectorTag, link.SubagentTag} {
		tags := tagsOf(stdioLaunchBody(t, "boss", LaunchInput{Cwd: "/w", Tags: []string{tag}}))
		n := 0
		for _, x := range tags {
			if x == link.SubagentTag {
				n++
			}
		}
		if hasTagIn(tags, link.DirectorTag) && n != 0 {
			t.Fatalf("a director got the subagent tag: %v", tags)
		}
		if n > 1 || !hasTagIn(tags, link.OriginTag) {
			t.Fatalf("tags = %v", tags)
		}
	}
}

func TestAHandRunStdioLaunchIsNotAttributedToABlankName(t *testing.T) {
	for _, name := range []string{"", "   "} {
		body := stdioLaunchBody(t, name, LaunchInput{Cwd: "/w"})
		if v, present := body["spawned_by"]; present {
			t.Fatalf("a nameless session was attributed: %q", v)
		}
		if p, _ := body["prompt"].(string); p != "" {
			t.Fatalf("an empty prompt grew a report line: %q", p)
		}
	}
}

func TestAStdioLaunchWithABriefStillEndsWithTheReportLine(t *testing.T) {
	dir := t.TempDir()
	body := stdioLaunchBody(t, "boss", LaunchInput{Cwd: dir, Brief: "the whole task"})
	if p, _ := body["prompt"].(string); !strings.Contains(p, briefFile) || !strings.HasSuffix(p, "when you need an answer.") {
		t.Fatalf("prompt = %q", p)
	}
}

func TestStdioTaskDismissClosesAnItemOnTheCallersOwnCard(t *testing.T) {
	var posted, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/owed-dismiss"):
			raw := make([]byte, 200)
			n, _ := r.Body.Read(raw)
			posted, body = r.URL.Path, string(raw[:n])
			_, _ = w.Write([]byte(`{"ok":true,"closed":true}`))
		case r.URL.Path == "/v1/tasks":
			_, _ = w.Write([]byte(`{"tasks":[{"id":"BOSSID","wire_name":"boss","status":"running"},{"id":"KIDID","wire_name":"kid","status":"done"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	t.Setenv("ATRIUM_AGENT_NAME", "boss")
	if _, _, err := taskHandler(context.Background(), nil, TaskInput{Dismiss: "kid"}); err != nil {
		t.Fatal(err)
	}
	if posted != "/v1/tasks/BOSSID/owed-dismiss" || !strings.Contains(body, "KIDID") {
		t.Fatalf("posted %q %q", posted, body)
	}
	// A session with no name has no card of its own to dismiss from.
	t.Setenv("ATRIUM_AGENT_NAME", "")
	if _, _, err := taskHandler(context.Background(), nil, TaskInput{Dismiss: "kid"}); err == nil {
		t.Fatal("a nameless session dismissed something")
	}
}

// The stdio atrium_launch passes its own card's dept on, read off the room's task list.
func TestStdioLaunchStampsTheLaunchersDept(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "d1", "wire_name": "boss", "tags": []string{"dept:ui"}},
				{"id": "d2", "wire_name": "plain", "tags": []string{"origin:agent"}},
			}})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "card1", "wire_name": "kid"})
	}))
	defer srv.Close()
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	for _, k := range []struct {
		me   string
		in   []string
		want bool
	}{{"boss", nil, true}, {"plain", nil, false}, {"boss", []string{"dept:runtime"}, false}, {"", nil, false}} {
		t.Setenv("ATRIUM_AGENT_NAME", k.me)
		if _, _, err := launchHandler(context.Background(), nil, LaunchInput{Cwd: "/w", Tags: k.in}); err != nil {
			t.Fatal(err)
		}
		if has := hasTagIn(tagsOf(got), "dept:ui"); has != k.want {
			t.Fatalf("me=%q tags %v: dept:ui stamped = %v, want %v (%v)", k.me, k.in, has, k.want, tagsOf(got))
		}
	}
}

// myTags takes the one card the room resolved a bare name or alias to, not only an exact handle.
func TestMyTagsTakesTheRoomsOneCardForAnAlias(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "bo" {
			t.Errorf("asked %q, want the one card by name", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
			{"id": "d1", "wire_name": "tenant/boss", "alias": "bo", "tags": []string{"dept:ui"}}}})
	}))
	defer srv.Close()
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	_ = os.WriteFile(loc, raw, 0o600)
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	if got := myTags(context.Background(), "bo"); len(got) != 1 || got[0] != "dept:ui" {
		t.Fatalf("myTags = %v, want [dept:ui]", got)
	}
}

// An older room ignores ?name= and may hold exactly one card that is not the caller: its dept is not ours.
func TestMyTagsIgnoresASingleRowThatIsNotMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
			{"id": "x1", "wire_name": "someone-else", "tags": []string{"dept:ui"}}}})
	}))
	defer srv.Close()
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	_ = os.WriteFile(loc, raw, 0o600)
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	if got := myTags(context.Background(), "me"); got != nil {
		t.Fatalf("myTags = %v, want none for a row that is not me", got)
	}
}
