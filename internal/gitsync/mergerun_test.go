package gitsync

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestMergeStatusSaysRunningForTheRunStillGoing(t *testing.T) {
	m := &MergeRunner{}
	m.running = map[string]*mergeRun{"github/o/r": {job: MergeJob{N: 4, Repo: "github/o/r", Candidate: "c", Token: "t"},
		cancel: func() {}}}
	m.last = map[int64]MergeReport{3: {N: 3, Verdict: VerdictGreen}}
	status := func(n string) (int, MergeReport) {
		rec := httptest.NewRecorder()
		m.serveStatus(rec, httptest.NewRequest("GET", "/v1/git/merge/status?n="+n, nil))
		var rep MergeReport
		_ = json.Unmarshal(rec.Body.Bytes(), &rep)
		return rec.Code, rep
	}
	if code, rep := status("4"); code != 200 || rep.Verdict != VerdictRunning || rep.Candidate != "c" || rep.Token != "t" {
		t.Fatalf("running: %d %+v", code, rep)
	}
	if code, rep := status("3"); code != 200 || rep.Verdict != VerdictGreen {
		t.Fatalf("done: %d %+v", code, rep)
	}
	if code, _ := status("5"); code != 404 {
		t.Fatalf("unknown: %d", code)
	}
}
