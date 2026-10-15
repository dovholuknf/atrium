package link

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE ENGINE IS PURE, so each case is a table row: what an item has, and what its steps and state are.

const (
	shaD1 = "d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1"
	shaD2 = "d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2"
	shaD3 = "d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3"
)

func wk(id string) hubstore.Work { return hubstore.Work{BacklogID: id, Title: id, Kind: "code"} }

func stepCard(c, step string) hubstore.WorkCard {
	return hubstore.WorkCard{BacklogID: "f-x", Room: "sg4", Card: c, Step: step}
}

func asg(id, c, state, note, at string) hubstore.Item {
	return hubstore.Item{ID: id, Room: "sg4", Card: c, State: state, Note: note, CreatedAt: at, UpdatedAt: at}
}

func designFact(sha, ref string) hubstore.WorkFact {
	return hubstore.WorkFact{Kind: hubstore.FactDesign, Source: "seen", AtSHA: sha, Ref: ref, At: "2026-10-15T10:00:00Z"}
}

func approveFact(at, design string) hubstore.WorkFact {
	return hubstore.WorkFact{Kind: hubstore.FactApprove, Source: "seen", AtSHA: at, At: "2026-10-15T11:00:00Z",
		Payload: `{"design":"` + design + `"}`}
}

func sendBackFact(sha string) hubstore.WorkFact {
	return hubstore.WorkFact{Kind: hubstore.FactSendBack, Source: "seen", AtSHA: sha, At: "2026-10-15T11:30:00Z", Payload: `{"note":"x"}`}
}

func derivedStep(l Lifecycle, name string) hubstore.WorkStepRow {
	for _, s := range l.Steps {
		if s.Step == name {
			return s
		}
	}
	return hubstore.WorkStepRow{}
}

func TestDeriveGivesEightStepsInOrder(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x")})
	if len(l.Steps) != 8 {
		t.Fatalf("steps = %d", len(l.Steps))
	}
	for i, name := range hubstore.Steps {
		if l.Steps[i].Step != name || l.Steps[i].Cards == "" {
			t.Fatalf("step %d = %+v", i, l.Steps[i])
		}
	}
	if l.Current != hubstore.StepDesigned || l.State != hubstore.ItemStateQueued {
		t.Fatalf("an item nobody has started: %q %q", l.Current, l.State)
	}
}

// A DESIGNER FINISHING MAKES `approved` ACTIVE WITH THE HUMAN AS THE PARTY.
func TestADesignerFinishingMakesApprovedActiveForTheHuman(t *testing.T) {
	in := DeriveIn{Work: wk("f-x"), Cards: []hubstore.WorkCard{stepCard("c1", "designed")},
		Assignments: []hubstore.Item{asg("a1", "c1", "finished", shaD1, "2026-10-15T10:00:00Z")},
		Facts:       []hubstore.WorkFact{designFact(shaD1, "a1")}, Doc: DocView{Known: true, Latest: shaD1}}
	l := Derive(in)
	d, a := derivedStep(l, "designed"), derivedStep(l, "approved")
	if d.State != hubstore.StepDone || d.SHA != shaD1 {
		t.Fatalf("designed = %+v", d)
	}
	if a.State != hubstore.StepActive || a.Party != hubstore.PartyHuman || a.SHA != shaD1 {
		t.Fatalf("approved = %+v", a)
	}
	if l.State != hubstore.ItemStateBlocked || l.Waiting != hubstore.PartyHuman || l.Current != hubstore.StepApproved {
		t.Fatalf("the item = %q waiting on %q at %q", l.State, l.Waiting, l.Current)
	}
}

// A DESIGNER THAT FINISHED, WITH NO DESIGN FACT YET, SAYS WHY AND IS NOT DONE.
func TestADesignerWhoseCommitIsNotADesignIsNotDone(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Cards: []hubstore.WorkCard{stepCard("c1", "designed")},
		Assignments: []hubstore.Item{asg("a1", "c1", "finished", shaD1, "2026-10-15T10:00:00Z")}})
	d := derivedStep(l, "designed")
	if d.State != hubstore.StepActive || !strings.Contains(d.Reason, "does not touch a design doc") {
		t.Fatalf("designed = %+v", d)
	}
	if derivedStep(l, "approved").State != hubstore.StepPending {
		t.Fatalf("approved before there was a design: %+v", derivedStep(l, "approved"))
	}
}

// APPROVING WRITES A FACT AT THE DESIGN SHA, AND THEN THE STEP IS DONE AT IT.
func TestAnApprovalAtTheDesignShaIsDone(t *testing.T) {
	in := DeriveIn{Work: wk("f-x"), Cards: []hubstore.WorkCard{stepCard("c1", "designed")},
		Assignments: []hubstore.Item{asg("a1", "c1", "finished", shaD1, "t1")},
		Facts:       []hubstore.WorkFact{designFact(shaD1, "a1"), approveFact(shaD1, shaD1)}, Doc: DocView{Known: true, Latest: shaD1}}
	l := Derive(in)
	if a := derivedStep(l, "approved"); a.State != hubstore.StepDone || a.SHA != shaD1 {
		t.Fatalf("approved = %+v", a)
	}
	// nobody is building yet: queued, not finished
	if l.State != hubstore.ItemStateQueued || l.Current != hubstore.StepBuilding {
		t.Fatalf("the item = %q at %q", l.State, l.Current)
	}
}

// A LATER COMMIT TO THE DOC SHOWS `stale`, WITH HOW FAR IT HAS MOVED.
func TestALaterCommitToTheDocMakesTheApprovalStale(t *testing.T) {
	in := DeriveIn{Work: wk("f-x"), Cards: []hubstore.WorkCard{stepCard("c1", "designed")},
		Assignments: []hubstore.Item{asg("a1", "c1", "finished", shaD1, "t1")},
		Facts:       []hubstore.WorkFact{designFact(shaD1, "a1"), approveFact(shaD1, shaD1)},
		Doc:         DocView{Known: true, Latest: shaD2, Since: 2}}
	a := derivedStep(Derive(in), "approved")
	if a.State != hubstore.StepStale || a.SHA != shaD1 || !strings.Contains(a.Reason, "2 commits") || !strings.Contains(a.Reason, shaD2[:7]) {
		t.Fatalf("approved = %+v", a)
	}
	// git that cannot be read is not evidence the doc moved
	in.Doc = DocView{}
	if a := derivedStep(Derive(in), "approved"); a.State != hubstore.StepDone {
		t.Fatalf("approved with no git = %+v", a)
	}
}

// SENDING BACK RETURNS `designed` TO ACTIVE, AND A NEW DESIGN NEEDS A NEW APPROVAL.
func TestSendBackReturnsDesignedToActiveAndTheNextDesignIsApprovedAgain(t *testing.T) {
	in := DeriveIn{Work: wk("f-x"), Cards: []hubstore.WorkCard{stepCard("c1", "designed")},
		Assignments: []hubstore.Item{asg("a1", "c1", "finished", shaD1, "t1")},
		Facts:       []hubstore.WorkFact{designFact(shaD1, "a1"), sendBackFact(shaD1)}, Doc: DocView{Known: true, Latest: shaD1}}
	l := Derive(in)
	if d := derivedStep(l, "designed"); d.State != hubstore.StepActive || d.Party != hubstore.PartyCard || !strings.Contains(d.Reason, "sent back") {
		t.Fatalf("designed after a send back = %+v", d)
	}
	if a := derivedStep(l, "approved"); a.State != hubstore.StepPending {
		t.Fatalf("approved after a send back = %+v", a)
	}
	// the room reopened the assignment: the designer is running again
	in.Assignments = []hubstore.Item{asg("a1", "c1", "running", "sent back", "t1")}
	l = Derive(in)
	if d := derivedStep(l, "designed"); d.State != hubstore.StepActive || l.State != hubstore.ItemStateRunning {
		t.Fatalf("a designer running again: %+v, item %q", d, l.State)
	}
	// it finishes with a new commit: designed is done again, and the old approval does not carry over
	in.Assignments = []hubstore.Item{asg("a1", "c1", "finished", shaD2, "t2")}
	in.Facts = []hubstore.WorkFact{designFact(shaD1, "a1"), approveFact(shaD1, shaD1), sendBackFact(shaD1), designFact(shaD2, "a1b")}
	in.Doc = DocView{Known: true, Latest: shaD2}
	l = Derive(in)
	if d := derivedStep(l, "designed"); d.State != hubstore.StepDone || d.SHA != shaD2 {
		t.Fatalf("designed after the second design = %+v", d)
	}
	if a := derivedStep(l, "approved"); a.State != hubstore.StepActive || a.Party != hubstore.PartyHuman || a.SHA != shaD2 {
		t.Fatalf("the first approval was carried over: %+v", a)
	}
}

// AN ITEM FILED "BUILD DIRECTLY" SKIPS BOTH, AND SO DOES ONE NO BACKLOG ITEM NAMED.
func TestBuildDirectlyAndAdhocSkipDesignedAndApproved(t *testing.T) {
	for name, in := range map[string]DeriveIn{
		"build directly": {Work: wk("f-x"), Facts: []hubstore.WorkFact{{Kind: hubstore.FactBuildDirectly, Source: "seen"}}},
		"adhoc":          {Work: wk("adhoc-it-1")},
	} {
		in.Cards = []hubstore.WorkCard{stepCard("c1", "")}
		in.Assignments = []hubstore.Item{asg("a1", "c1", "running", "", "t1")}
		l := Derive(in)
		for _, s := range []string{"designed", "approved"} {
			if st := derivedStep(l, s); st.State != hubstore.StepSkipped || st.Reason == "" {
				t.Errorf("%s: %s = %+v", name, s, st)
			}
		}
		if b := derivedStep(l, "building"); b.State != hubstore.StepActive || b.Reason != "" || b.Cards != `["sg4/c1"]` {
			t.Errorf("%s: building = %+v", name, b)
		}
		if l.State != hubstore.ItemStateRunning || l.Current != hubstore.StepBuilding {
			t.Errorf("%s: item = %q at %q", name, l.State, l.Current)
		}
	}
}

// THE ITEM'S STATE IS DERIVED FROM ITS STEPS AND ITS ASSIGNMENTS.
func TestTheItemStateComesFromTheSteps(t *testing.T) {
	adhoc := func(state string) Lifecycle {
		return Derive(DeriveIn{Work: wk("adhoc-it-1"), Cards: []hubstore.WorkCard{stepCard("c1", "building")},
			Assignments: []hubstore.Item{asg("a1", "c1", state, shaD1, "t1")}})
	}
	for state, want := range map[string]string{"queued": "queued", "running": "running", "blocked": "blocked", "finished": "finished"} {
		if got := adhoc(state).State; got != want {
			t.Errorf("an adhoc builder %s: item is %q, want %q", state, got, want)
		}
	}
	if s := derivedStep(adhoc("finished"), "building"); s.State != hubstore.StepDone || s.SHA != shaD1 {
		t.Errorf("a finished builder = %+v", s)
	}
	if s := derivedStep(adhoc("blocked"), "building"); s.State != hubstore.StepBlocked {
		t.Errorf("a blocked builder = %+v", s)
	}
}

func TestABuilderBeforeTheApprovalSaysSo(t *testing.T) {
	l := Derive(DeriveIn{Work: wk("f-x"), Cards: []hubstore.WorkCard{stepCard("c1", "building")},
		Assignments: []hubstore.Item{asg("a1", "c1", "running", "", "t1")}})
	if b := derivedStep(l, "building"); b.State != hubstore.StepActive || !strings.Contains(b.Reason, "before the design was approved") {
		t.Fatalf("building = %+v", b)
	}
}

func TestPickDesignDocPrefersTheOneNamedForTheItem(t *testing.T) {
	for _, c := range []struct {
		files []string
		want  string
		ok    bool
	}{
		{[]string{"internal/x.go"}, "", false},
		{[]string{"internal/x.go", "docs/rnd/y.md"}, "docs/rnd/y.md", true},
		{[]string{"README.md", "docs/rnd/f-x-design.md", "docs/other.md"}, "docs/rnd/f-x-design.md", true},
		{[]string{"b.md", "a.md"}, "a.md", true},
	} {
		got, ok := pickDesignDoc(c.files, "f-x")
		if got != c.want || ok != c.ok {
			t.Errorf("%v: %q %v, want %q %v", c.files, got, ok, c.want, c.ok)
		}
	}
}

func TestNoteSHAReadsOnlyACommitId(t *testing.T) {
	for note, want := range map[string]string{shaD1: shaD1, "ABCDEF1": "abcdef1", "cancelled by owner": "", "sent back": "", "abc": "", "": ""} {
		if got := NoteSHA(note); got != want {
			t.Errorf("NoteSHA(%q) = %q, want %q", note, got, want)
		}
	}
}

func TestWithStepTagReplacesAndRefuses(t *testing.T) {
	got, err := WithStepTag([]string{"dept:fabric", StepTagPrefix + "building"}, "Designed")
	if err != nil || len(got) != 2 || got[1] != StepTagPrefix+"designed" {
		t.Fatalf("tags = %v %v", got, err)
	}
	if _, err := WithStepTag(nil, "napping"); err == nil || !strings.Contains(err.Error(), "designed") {
		t.Fatalf("a step that is not one: %v", err)
	}
}
