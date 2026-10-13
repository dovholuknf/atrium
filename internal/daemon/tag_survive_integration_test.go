//go:build integration

package daemon

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// An interview card carries its tag for life: it is what files the card in the operator's interview group. A launch
// onto the card is a machine writing, and it must add to the tags the card has NOW, never replace them with the ones
// the launch happened to carry.
func interviewCard(t *testing.T) (*Daemon, string, http.Handler) {
	t.Helper()
	d, card, h := unleanDaemon(t)
	if err := d.st.SetTags(card.ID, []string{InterviewTag, OriginAgentTag, "subagent"}); err != nil {
		t.Fatal(err)
	}
	return d, card.ID, h
}

func tagsOf(t *testing.T, d *Daemon, id string) []string {
	t.Helper()
	got, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Tags
}

func TestAnInterviewTagSurvivesEveryLaunchOntoTheCard(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	cases := []struct{ name, extra string }{
		{"a relaunch carrying fresh launch tags", `,"tags":["subagent","origin:agent"]`},
		{"a resume", `,"resume":"abc"`},
		{"a restart with nothing said", ``},
		{"an unlean", `,"lean":false`},
		{"an unlean carrying tags", `,"lean":false,"tags":["subagent"]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, id, h := interviewCard(t)
			card, _ := d.st.Get(id)
			w := launchOnto(h, card, c.extra)
			if w.Code != http.StatusOK {
				t.Skipf("could not start onto the card, answered %d: %s", w.Code, w.Body)
			}
			t.Cleanup(func() { _ = d.StopRunner(id) })
			if got := tagsOf(t, d, id); !slices.Contains(got, InterviewTag) {
				t.Fatalf("the interview tag was dropped, tags are %q", got)
			}
		})
	}
}

// The other half: a human taking a tag off on the board still takes it off, interview included.
func TestAHumanTagEditStillRemovesATag(t *testing.T) {
	d, id, h := interviewCard(t)
	if w := send(h, "PATCH", "/v1/tasks/"+id, `{"tags":["`+OriginAgentTag+`"]}`); w.Code != http.StatusOK {
		t.Fatalf("the tag edit answered %d: %s", w.Code, w.Body)
	}
	if got := strings.Join(tagsOf(t, d, id), ","); got != OriginAgentTag {
		t.Fatalf("a human removing tags should stick, tags are %q", got)
	}
}
