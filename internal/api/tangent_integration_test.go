//go:build integration

package api

import (
	"encoding/json"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

func tangentHarness(t *testing.T) (*openHarness, string) {
	t.Helper()
	oh := newOpenHarness(t, &fakeForge{})
	scm := realSlash(t.TempDir())
	if err := oh.srv.st.SetSetting(gitsync.SettingSCMRoot, scm); err != nil {
		t.Fatal(err)
	}
	return oh, scm
}

func tangentCall(t *testing.T, oh *openHarness, name string) (int, map[string]any) {
	t.Helper()
	rec := post(t, oh.h, "/v1/tangent", map[string]any{"name": name})
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestTangentMakesItsFolderAndACardInIt(t *testing.T) {
	oh, scm := tangentHarness(t)
	code, out := tangentCall(t, oh, "spike")
	want := scm + "/scratch/tangents/spike"
	if code != 200 || out["id"] == nil || !exists(want) {
		t.Fatalf("%d %v, want %s", code, out, want)
	}
	l := lastLaunch(t, oh)
	if l.Cwd != want || l.Title != "spike" || l.Repo != "" || len(l.Tags) != 1 || l.Tags[0] != "tangent" {
		t.Errorf("the launch was %+v", l)
	}
	var w struct {
		Window    string `json:"window"`
		IfRunning string `json:"if_running"`
	}
	_ = json.Unmarshal(oh.launched[0], &w)
	if w.Window != "tangent" || w.IfRunning != "skip" {
		t.Errorf("the launch was %+v", w)
	}
}

func TestTangentOfANameAlreadyThereReusesItsFolder(t *testing.T) {
	oh, scm := tangentHarness(t)
	if code, out := tangentCall(t, oh, "spike"); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if code, out := tangentCall(t, oh, "spike"); code != 200 {
		t.Fatalf("the second tangent of a name: %d %v", code, out)
	}
	if len(oh.launched) != 2 || lastLaunch(t, oh).Cwd != scm+"/scratch/tangents/spike" {
		t.Errorf("the launches were %d, the last %+v", len(oh.launched), lastLaunch(t, oh))
	}
}

func TestTangentSanitisesABadName(t *testing.T) {
	oh, scm := tangentHarness(t)
	code, out := tangentCall(t, oh, "  ../My idea: v2!  ")
	want := scm + "/scratch/tangents/My-idea-v2"
	if code != 200 || !exists(want) || lastLaunch(t, oh).Cwd != want {
		t.Fatalf("%d %v, want %s", code, out, want)
	}
	if code, out := tangentCall(t, oh, "/../"); code != 400 || len(oh.launched) != 1 {
		t.Errorf("a name with nothing left: %d %v", code, out)
	}
}

func TestTangentNameIsAFolderName(t *testing.T) {
	for in, want := range map[string]string{
		"spike": "spike", "a b": "a-b", "..\\x/y": "x-y", ".hidden": "hidden", "--": "", "v1.2_x": "v1.2_x",
	} {
		if got := tangentName(in); got != want {
			t.Errorf("tangentName(%q) = %q, want %q", in, got, want)
		}
	}
}
