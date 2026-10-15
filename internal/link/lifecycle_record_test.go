package link

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// TESTED AND REVIEWED (item lifecycle L4), the pure part.

const (
	shaT1 = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	shaT2 = "a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2"
)

func testFact(sha, source, result string, dirty bool) hubstore.WorkFact {
	return hubstore.WorkFact{Kind: hubstore.FactTest, Source: source, AtSHA: sha, Dirty: dirty, Card: "c1", At: "2026-10-15T12:00:00Z",
		Payload: `{"cmd":"go test","result":"` + result + `"}`}
}

func reviewFact(sha, verdict string) hubstore.WorkFact {
	return hubstore.WorkFact{Kind: hubstore.FactReview, Source: hubstore.SourceSeen, AtSHA: sha, At: "2026-10-15T13:00:00Z",
		Payload: `{"verdict":"` + verdict + `"}`}
}

func evAt(tip string, since map[string]int, cover CoverView) Evidence {
	return Evidence{Known: true, Tip: tip, Since: since, Cover: cover, CoverKnown: true}
}

func TestATestAtTheTipIsDone(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Facts: []hubstore.WorkFact{testFact(shaT1, "seen", "pass", false)}, Evidence: evAt(shaT1, nil, CoverView{})})
	s := derivedStep(l, "tested")
	if s.State != hubstore.StepDone || s.Tag != hubstore.TagObserved {
		t.Fatalf("tested = %+v", s)
	}
}

func TestATestFollowedByACommitIsOneCommitAgo(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Facts: []hubstore.WorkFact{testFact(shaT1, "seen", "pass", false)},
		Evidence: evAt(shaT2, map[string]int{shaT1: 1}, CoverView{})})
	s := derivedStep(l, "tested")
	if s.State != hubstore.StepStale || !strings.Contains(s.Reason, "1 commit ago") {
		t.Fatalf("tested = %+v", s)
	}
	if l.Ready {
		t.Fatal("a stale test made the item ready")
	}
}

func TestATestWithUncommittedChangesSaysSo(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Facts: []hubstore.WorkFact{testFact(shaT1, "seen", "pass", true), reviewFact(shaT1, "ok")},
		Evidence: evAt(shaT1, nil, CoverView{})})
	if s := derivedStep(l, "tested"); !strings.Contains(s.Reason, "plus uncommitted changes") {
		t.Fatalf("tested = %+v", s)
	}
	if l.Ready || !strings.Contains(l.NotReady, "uncommitted") {
		t.Fatalf("ready = %v %q", l.Ready, l.NotReady)
	}
}

func TestAReportedTestDoesNotMakeTheItemReady(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Facts: []hubstore.WorkFact{testFact(shaT1, "reported", "pass", false), reviewFact(shaT1, "ok")},
		Evidence: evAt(shaT1, nil, CoverView{})})
	s := derivedStep(l, "tested")
	if s.Tag != hubstore.TagReported || !strings.Contains(s.Reason, "reported by the card") {
		t.Fatalf("tested = %+v", s)
	}
	if l.Ready || !strings.Contains(l.NotReady, "reported") {
		t.Fatalf("ready = %v %q", l.Ready, l.NotReady)
	}
}

func TestASeenTestAndAReviewAtTheTipIsReady(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Facts: []hubstore.WorkFact{testFact(shaT1, "seen", "pass", false), reviewFact(shaT1, "ok")},
		Evidence: evAt(shaT1, nil, CoverView{})})
	if !l.Ready || derivedStep(l, "reviewed").State != hubstore.StepDone {
		t.Fatalf("ready = %v %q, reviewed = %+v", l.Ready, l.NotReady, derivedStep(l, "reviewed"))
	}
}

func TestAHoldingReviewIsActiveAndNotReady(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Facts: []hubstore.WorkFact{testFact(shaT1, "seen", "pass", false), reviewFact(shaT1, "hold")},
		Evidence: evAt(shaT1, nil, CoverView{})})
	if r := derivedStep(l, "reviewed"); r.State != hubstore.StepActive || l.Ready {
		t.Fatalf("reviewed = %+v ready %v", r, l.Ready)
	}
}

func TestAFailedTestIsNotDone(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Facts: []hubstore.WorkFact{testFact(shaT1, "seen", "fail", false)}, Evidence: evAt(shaT1, nil, CoverView{})})
	if s := derivedStep(l, "tested"); s.State != hubstore.StepActive || !strings.Contains(s.Reason, "failed") {
		t.Fatalf("tested = %+v", s)
	}
}

func TestPartialTrailerCoverageIsStale(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Evidence: evAt(shaT1, nil, CoverView{Total: 2, Covered: 1})})
	if r := derivedStep(l, "reviewed"); r.State != hubstore.StepStale || !strings.Contains(r.Reason, "covers 1 of 2 commits") {
		t.Fatalf("reviewed = %+v", r)
	}
}

func TestFullTrailerCoverageIsReviewedAndHeldTrailerHolds(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Evidence: evAt(shaT1, nil, CoverView{Total: 2, Covered: 2})})
	if r := derivedStep(l, "reviewed"); r.State != hubstore.StepDone {
		t.Fatalf("reviewed = %+v", r)
	}
	l = Derive(DeriveIn{Work: wk("f-x"), Evidence: evAt(shaT1, nil, CoverView{Total: 2, Covered: 2, Held: 1})})
	if r := derivedStep(l, "reviewed"); r.State != hubstore.StepActive || !strings.Contains(r.Reason, "held") {
		t.Fatalf("reviewed = %+v", r)
	}
}

func TestNoTestIsPending(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x")})
	if s := derivedStep(l, "tested"); s.State != hubstore.StepPending || l.Ready {
		t.Fatalf("tested = %+v", s)
	}
}
