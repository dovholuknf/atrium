package link

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

const landedSHA = "1111111111111111111111111111111111111111"

func landedFacts(paths ...string) gitsync.LandFacts {
	return gitsync.LandFacts{SHA: landedSHA, Landed: true, LandedSHA: landedSHA, Paths: paths}
}

func stepOf(t *testing.T, steps []GitStep, name string) GitStep {
	t.Helper()
	for _, s := range steps {
		if s.Step == name {
			return s
		}
	}
	t.Fatalf("no %s step in %+v", name, steps)
	return GitStep{}
}

// A BRANCH ON claude/main IS LANDED AT THE SHA THE MIRROR NAMED, and one that is not is active while it has a tip.
func TestLandedFollowsTheMirror(t *testing.T) {
	in := GitInput{Facts: landedFacts("internal/x.go"), HaveBranch: true}
	if s := stepOf(t, DeriveGit(in), StepLanded); s.State != hubstore.StepDone || s.SHA != landedSHA {
		t.Fatalf("landed = %+v", s)
	}
	in = GitInput{Facts: gitsync.LandFacts{SHA: landedSHA}, HaveBranch: true}
	steps := DeriveGit(in)
	if s := stepOf(t, steps, StepLanded); s.State != hubstore.StepActive || s.SHA != landedSHA {
		t.Fatalf("a branch not on claude/main = %+v", s)
	}
	if d := stepOf(t, steps, StepDeployed); d.State != hubstore.StepPending {
		t.Fatalf("deployed before landed = %+v", d)
	}
	if s := stepOf(t, DeriveGit(GitInput{}), StepLanded); s.State != hubstore.StepPending {
		t.Fatalf("no branch = %+v", s)
	}
}

// A ROOM ON AN OLDER BUILD KEEPS `deployed` ACTIVE AND NAMES THE ROOM.
func TestAnOlderRoomKeepsDeployedActiveAndIsNamed(t *testing.T) {
	older := "2222222222222222222222222222222222222222"
	in := GitInput{
		Facts: landedFacts("internal/link/x.go"),
		Hub:   BuildSeen{Name: "hub", Commit: landedSHA, Contains: true, Known: true},
		Rooms: []BuildSeen{
			{Name: "sg3", Commit: landedSHA, Contains: true, Known: true},
			{Name: "sg4", Commit: older, Known: true},
			{Name: "sg5"},
		},
	}
	d := stepOf(t, DeriveGit(in), StepDeployed)
	if d.State != hubstore.StepActive {
		t.Fatalf("deployed = %+v", d)
	}
	if got := strings.Join(d.Waiting, ","); got != "sg4,sg5" {
		t.Fatalf("waiting on %q", got)
	}
	for _, want := range []string{"sg4 (on 2222222)", "sg5 (does not report a build)"} {
		if !strings.Contains(d.Reason, want) {
			t.Fatalf("reason %q lacks %q", d.Reason, want)
		}
	}
	if strings.Contains(d.Reason, "sg3") {
		t.Fatalf("a room that has it is named: %q", d.Reason)
	}
	// Once the last room reports a build that holds it, the step is done.
	in.Rooms[1], in.Rooms[2] = BuildSeen{Name: "sg4", Commit: landedSHA, Contains: true, Known: true},
		BuildSeen{Name: "sg5", Commit: landedSHA, Contains: true, Known: true}
	if d = stepOf(t, DeriveGit(in), StepDeployed); d.State != hubstore.StepDone || d.SHA != landedSHA {
		t.Fatalf("every room has it: %+v", d)
	}
}

// HUB-ONLY CODE NEEDS THE HUB AND NO ROOM, which is deployready's own classification.
func TestHubOnlyCodeIgnoresTheRooms(t *testing.T) {
	in := GitInput{
		Facts: landedFacts("internal/hubstore/x.go"),
		Hub:   BuildSeen{Name: "hub", Commit: landedSHA, Contains: true, Known: true},
		Rooms: []BuildSeen{{Name: "sg4"}},
	}
	if d := stepOf(t, DeriveGit(in), StepDeployed); d.State != hubstore.StepDone {
		t.Fatalf("a room that does not run hub code holds it up: %+v", d)
	}
	in.Hub = BuildSeen{Name: "hub", Commit: "3333333333333333333333333333333333333333", Known: true}
	if d := stepOf(t, DeriveGit(in), StepDeployed); d.State != hubstore.StepActive || len(d.Waiting) != 1 || d.Waiting[0] != "hub" {
		t.Fatalf("an old hub: %+v", d)
	}
}

// A DOC ITEM SKIPS DEPLOY WITH THE REASON, by its kind and by its files.
func TestADocItemSkipsDeploy(t *testing.T) {
	for name, in := range map[string]GitInput{
		"kind":  {Kind: "doc", Facts: landedFacts("internal/x.go")},
		"files": {Kind: "code", Facts: landedFacts("docs/a.md", "changelog/fabric/2026-10-15-x.md", "img/a.png")},
	} {
		d := stepOf(t, DeriveGit(in), StepDeployed)
		if d.State != hubstore.StepSkipped || d.Reason == "" {
			t.Fatalf("%s: deployed = %+v", name, d)
		}
	}
	// Unknown files are not a skip.
	if d := stepOf(t, DeriveGit(GitInput{Facts: landedFacts()}), StepDeployed); d.State != hubstore.StepActive {
		t.Fatalf("unknown files = %+v", d)
	}
}

// PUSHED FOLLOWS ORIGIN, and says when the mirror had to answer.
func TestPushedSaysWhereItLooked(t *testing.T) {
	in := GitInput{Facts: landedFacts("docs/a.md")}
	in.Facts.Pushed, in.Facts.PushedVia = true, "origin"
	if p := stepOf(t, DeriveGit(in), StepPushed); p.State != hubstore.StepDone || p.SHA != landedSHA {
		t.Fatalf("pushed = %+v", p)
	}
	in.Facts.Pushed, in.Facts.PushedVia, in.Facts.PushedNote = false, "mirror", "the forge did not answer"
	p := stepOf(t, DeriveGit(in), StepPushed)
	if p.State != hubstore.StepActive || !strings.Contains(p.Reason, "the forge did not answer") {
		t.Fatalf("pushed = %+v", p)
	}
}
