//go:build integration

package link

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const designSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// fakeDesignGit is a hub's git with one design commit and a doc whose newest commit a test moves.
type fakeDesignGit struct {
	last    string
	touches int
}

func (f *fakeDesignGit) FindCommit(_ context.Context, sha string) (string, string, bool) {
	return "repo", designSHA, strings.HasPrefix(designSHA, sha)
}
func (f *fakeDesignGit) CommitFiles(context.Context, string, string) ([]string, error) {
	return []string{"docs/rnd/f-x-design.md", "main.go"}, nil
}
func (f *fakeDesignGit) LastTouch(context.Context, string, string, string) (string, error) {
	return f.last, nil
}
func (f *fakeDesignGit) TouchesSince(context.Context, string, string, string) (int, error) {
	return f.touches, nil
}

func (x *itemRig) designerFinished(t *testing.T) {
	t.Helper()
	code, body := x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-d", "room": "sg4", "card": "c1",
		"generation": 1, "state": "finished", "revision": 1, "owner_kind": "human", "created_by": "orch",
		"note": designSHA[:12], "work_id": "f-x", "step": "designed"}), loopAddr)
	if code != http.StatusOK {
		t.Fatalf("push: %d %s", code, body)
	}
}

func (x *itemRig) stepState(t *testing.T, step string) string {
	t.Helper()
	code, body := x.send("GET", "/_hub/work/f-x", "", loopAddr)
	if code != http.StatusOK {
		t.Fatalf("work: %d %s", code, body)
	}
	i := strings.Index(body, `"step":"`+step+`"`)
	if i < 0 {
		t.Fatalf("no step %s in %s", step, body)
	}
	rest := body[i:]
	j := strings.Index(rest, `"state":"`)
	if j < 0 {
		t.Fatalf("no state after %s in %s", step, body)
	}
	rest = rest[j+len(`"state":"`):]
	return rest[:strings.Index(rest, `"`)]
}

func TestADesignerFinishingMakesApprovedActiveThenApproveThenStale(t *testing.T) {
	x := newItemRig(t)
	g := &fakeDesignGit{last: designSHA}
	x.p.designGit = g
	x.designerFinished(t)

	if got := x.stepState(t, "approved"); got != "active" {
		t.Fatalf("approved after the designer finished: %s", got)
	}
	if code, body := x.send("POST", "/_hub/work/f-x", js(map[string]any{"do": "approve", "sha": "bbbb"}), loopAddr); code != http.StatusConflict {
		t.Fatalf("approve at a sha that is not the design: %d %s", code, body)
	}
	if code, body := x.send("POST", "/_hub/work/f-x", js(map[string]any{"do": "approve", "sha": designSHA}), "203.0.113.9:4000"); code != http.StatusForbidden {
		t.Fatalf("approve from elsewhere: %d %s", code, body)
	}
	if code, body := x.send("POST", "/_hub/work/f-x", js(map[string]any{"do": "approve", "sha": designSHA}), loopAddr); code != http.StatusOK {
		t.Fatalf("approve: %d %s", code, body)
	}
	if got := x.stepState(t, "approved"); got != "done" {
		t.Fatalf("approved after approving: %s", got)
	}
	g.last, g.touches = "cccccccccccccccccccccccccccccccccccccccc", 1
	if got := x.stepState(t, "approved"); got != "stale" {
		t.Fatalf("approved after the doc moved: %s", got)
	}
}

func TestSendBackTypesTheNoteToTheDesignerAndReopensDesigned(t *testing.T) {
	x := newItemRig(t)
	x.p.designGit = &fakeDesignGit{last: designSHA}
	x.designerFinished(t)

	if code, body := x.send("POST", "/_hub/work/f-x", js(map[string]any{"do": "send_back"}), loopAddr); code != http.StatusBadRequest {
		t.Fatalf("send back with no note: %d %s", code, body)
	}
	if code, body := x.send("POST", "/_hub/work/f-x", js(map[string]any{"do": "send_back", "note": "cover the halt"}), loopAddr); code != http.StatusOK {
		t.Fatalf("send back: %d %s", code, body)
	}
	if len(x.calls) != 1 || !strings.Contains(x.calls[0], "/v1/items/it-d/sendback") || !strings.Contains(x.calls[0], "cover the halt") {
		t.Fatalf("the room was not asked to type the note: %v", x.calls)
	}
	if got := x.stepState(t, "designed"); got != "active" {
		t.Fatalf("designed after send back: %s", got)
	}
}
