//go:build integration

package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

func postNeed(t *testing.T, d *Daemon, id string, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/need", bytes.NewReader(raw))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	d.handleNeed(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestANeedWithNoWhyIsRefusedAndNothingIsAdded(t *testing.T) {
	d := testDaemon(t)
	w := peerCard(t, d, "worker")
	if err := d.st.SetTags(w.ID, []string{LeanTag, SuperleanTag}); err != nil {
		t.Fatal(err)
	}
	if code, _ := postNeed(t, d, w.ID, map[string]any{"needs": []string{"tool:Skill"}, "why": "  "}); code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400", code)
	}
	if got, _ := d.st.Get(w.ID); small.HasTag(got.Tags, "atrium:tool:Skill") {
		t.Fatalf("tags %v after a refused need", got.Tags)
	}
}

func TestANeedIsGrantedAtOnceAndSaysWhatToTellTheHuman(t *testing.T) {
	d := testDaemon(t)
	d.hk.needQuiet = time.Hour
	_, w := launchedPair(t, d)
	if err := d.st.SetTags(w.ID, []string{LeanTag, SuperleanTag}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.CreateWorkItem(mustTask(t, d, w.ID), store.NewWorkItem{Brief: "review"}); err != nil {
		t.Fatal(err)
	}
	code, out := postNeed(t, d, w.ID, map[string]any{"needs": []string{"tool:Skill", "tool:atrium_launch", "tool:Bogus"},
		"why": "the brief says run /review-panel", "by": "worker"})
	if code != http.StatusOK || out["restarting"] != true {
		t.Fatalf("code %d, out %v", code, out)
	}
	if g, _ := json.Marshal(out["granted"]); string(g) != `["tool:Skill","tool:atrium_launch"]` {
		t.Fatalf("granted %s", g)
	}
	got, _ := d.st.Get(w.ID)
	if !small.HasTag(got.Tags, "atrium:tool:Skill") || !small.HasTag(got.Tags, ElevatedTag) {
		t.Fatalf("tags %v", got.Tags)
	}
	logs, err := d.st.WorkLog(w.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range logs {
		if strings.Contains(e.Text, "review-panel") && strings.Contains(e.Text, "refused: tool:Bogus") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no need line carrying the why on the work item: %+v", logs)
	}
}

func TestALaunchGrantNeedsNoLeanCard(t *testing.T) {
	d := testDaemon(t)
	d.hk.needQuiet = time.Hour
	w := peerCard(t, d, "worker")
	code, out := postNeed(t, d, w.ID, map[string]any{"needs": []string{"tool:atrium_launch", "skill:x"}, "why": "fan out reviewers"})
	if code != http.StatusOK || out["restarting"] != true {
		t.Fatalf("code %d, out %v", code, out)
	}
	if g, _ := json.Marshal(out["granted"]); string(g) != `["tool:atrium_launch"]` {
		t.Fatalf("granted %s", g)
	}
	got, _ := d.st.Get(w.ID)
	if !small.HasTag(got.Tags, ElevatedTag) || small.HasTag(got.Tags, "atrium:skill:x") {
		t.Fatalf("tags %v", got.Tags)
	}
}

func TestAChatCardIsGrantedABuiltInToolAndAPlainOneIsNot(t *testing.T) {
	d := testDaemon(t)
	d.hk.needQuiet = time.Hour
	chat := peerCard(t, d, "chatty")
	if err := d.st.SetTags(chat.ID, []string{LeanTag, SuperleanTag, ChatTag}); err != nil {
		t.Fatal(err)
	}
	code, out := postNeed(t, d, chat.ID, map[string]any{"needs": []string{"tool:Read"}, "why": "look at the file"})
	if code != http.StatusOK || out["restarting"] != true {
		t.Fatalf("code %d, out %v", code, out)
	}
	if got, _ := d.st.Get(chat.ID); !small.HasTag(got.Tags, "atrium:tool:Read") {
		t.Fatalf("tags %v", got.Tags)
	}
	plain := peerCard(t, d, "plain")
	if err := d.st.SetTags(plain.ID, []string{LeanTag, SuperleanTag}); err != nil {
		t.Fatal(err)
	}
	_, out = postNeed(t, d, plain.ID, map[string]any{"needs": []string{"tool:Read"}, "why": "look at the file"})
	if out["restarting"] == true {
		t.Fatalf("a card that is not chat was restarted for Read: %v", out)
	}
	if got, _ := d.st.Get(plain.ID); small.HasTag(got.Tags, "atrium:tool:Read") {
		t.Fatalf("tags %v", got.Tags)
	}
}
