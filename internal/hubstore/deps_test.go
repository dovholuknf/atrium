package hubstore

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/itemgate"
)

const depRepo = "github/dovholuknf/atrium"

func item(id string) itemgate.Target { return itemgate.Target{Kind: itemgate.KindItem, Target: id} }

func addGate(t *testing.T, s *Store, waiter string, on ...itemgate.Target) []itemgate.Gate {
	t.Helper()
	gs, err := s.AddGates(depRepo, waiter, on, "because", "runtime@sg4")
	if err != nil {
		t.Fatalf("add %s: %v", waiter, err)
	}
	return gs
}

func TestAddingAGateTwiceAnswersTheSameRow(t *testing.T) {
	s := open(t)
	a := addGate(t, s, "r-038", item("r-037"))
	b := addGate(t, s, "r-038", item("r-037"))
	if len(a) != 1 || len(b) != 1 || a[0].ID != b[0].ID {
		t.Fatalf("a %+v b %+v", a, b)
	}
	if a[0].AddedBy != "runtime@sg4" || a[0].Why != "because" || !a[0].Open() {
		t.Fatalf("row = %+v", a[0])
	}
	// Met, the same gate can be added again as a new open row.
	if changed, err := s.GateMet(a[0].ID, itemgate.MetByAtrium, "landed"); err != nil || !changed {
		t.Fatalf("met = %v %v", changed, err)
	}
	c := addGate(t, s, "r-038", item("r-037"))
	if c[0].ID == a[0].ID || !c[0].Open() {
		t.Fatalf("re-add after met = %+v", c[0])
	}
	all, _ := s.Gates(depRepo, "r-038", false)
	open, _ := s.Gates(depRepo, "r-038", true)
	if len(all) != 2 || len(open) != 1 {
		t.Fatalf("all %d open %d", len(all), len(open))
	}
}

func TestAGateIsMetOnceAndStaysMet(t *testing.T) {
	s := open(t)
	g := addGate(t, s, "r-038", item("r-037"))[0]
	if changed, _ := s.GateMet(g.ID, itemgate.MetByAtrium, "first"); !changed {
		t.Fatal("first met should change it")
	}
	if changed, _ := s.GateMet(g.ID, itemgate.MetByHuman, "second"); changed {
		t.Fatal("a met gate was met again")
	}
	got, _ := s.Gate(g.ID)
	if got.MetBy != itemgate.MetByAtrium || got.MetWhy != "first" || got.MetAt == nil {
		t.Fatalf("got %+v", got)
	}
	if _, err := s.GateMet(g.ID, "agent", "x"); err == nil {
		t.Fatal("an agent is not a way to meet a gate")
	}
}

func TestALoopIsRefusedAndNamed(t *testing.T) {
	s := open(t)
	if _, err := s.AddGates(depRepo, "r-040", []itemgate.Target{item("r-040")}, "", ""); err == nil {
		t.Fatal("waiting on itself should be refused")
	}
	addGate(t, s, "r-040", item("r-041"))
	_, err := s.AddGates(depRepo, "r-041", []itemgate.Target{item("r-040")}, "", "")
	if err == nil || !strings.HasPrefix(err.Error(), "r-041 waits on r-040, which waits on r-041") {
		t.Fatalf("direct loop = %v", err)
	}
	addGate(t, s, "r-041", item("r-042"))
	_, err = s.AddGates(depRepo, "r-042", []itemgate.Target{item("r-040")}, "", "")
	if err == nil || !strings.HasPrefix(err.Error(), "r-042 waits on r-040, which waits on r-041, which waits on r-042") {
		t.Fatalf("three long = %v", err)
	}
	// A refused call writes nothing, not even its good targets.
	if _, err := s.AddGates(depRepo, "r-042", []itemgate.Target{item("r-050"), item("r-040")}, "", ""); err == nil {
		t.Fatal("should refuse")
	}
	if gs, _ := s.Gates(depRepo, "r-042", false); len(gs) != 0 {
		t.Fatalf("a refused add wrote %+v", gs)
	}
	// A met edge is not part of any loop.
	gs, _ := s.Gates(depRepo, "r-040", true)
	s.GateMet(gs[0].ID, itemgate.MetByHuman, "x")
	addGate(t, s, "r-042", item("r-040"))
}

func TestALoopThroughARenameIsRefused(t *testing.T) {
	s := open(t)
	addGate(t, s, "r-040", item("r-new-thing"))
	if _, err := s.RenameItem(depRepo, "r-new-thing", "r-060", "merge"); err != nil {
		t.Fatal(err)
	}
	// Written against the old slug, and still a loop.
	_, err := s.AddGates(depRepo, "r-new-thing", []itemgate.Target{item("r-040")}, "", "")
	if err == nil || !strings.Contains(err.Error(), "r-060 waits on r-040, which waits on r-060") {
		t.Fatalf("loop through a rename = %v", err)
	}
}

// r-new-review-7f4e76c0 item 1: two gates that are fine apart, joined by a rename.
func TestARenameThatClosesALoopIsRefusedWhole(t *testing.T) {
	s := open(t)
	addGate(t, s, "r-900", item("r-901"))
	addGate(t, s, "r-902", item("r-900"))
	_, err := s.RenameItem(depRepo, "r-901", "r-902", "x")
	if err == nil || !strings.Contains(err.Error(), "r-902 waits on r-900, which waits on r-902") {
		t.Fatalf("rename closing a loop = %v", err)
	}
	gs, _ := s.Gates(depRepo, "r-900", true)
	if len(gs) != 1 || gs[0].Target != "r-901" {
		t.Fatalf("a refused rename moved %+v", gs)
	}
	if names, _ := s.ItemNames(depRepo, "r-901"); len(names) != 1 || names[0] != "r-901" {
		t.Fatalf("a refused rename was recorded: %v", names)
	}
}

// r-new-review-7f4e76c0 item 2: `live:<item>` also waits on that item.
func TestALoopThroughLiveIsRefused(t *testing.T) {
	s := open(t)
	live := itemgate.Target{Kind: itemgate.KindCond, Target: "live:r-901"}
	addGate(t, s, "r-900", live)
	_, err := s.AddGates(depRepo, "r-901", []itemgate.Target{item("r-900")}, "", "")
	if err == nil || !strings.HasPrefix(err.Error(), "r-901 waits on r-900, which waits on r-901") {
		t.Fatalf("loop through live = %v", err)
	}
	if _, err := s.AddGates(depRepo, "r-903", []itemgate.Target{{Kind: itemgate.KindCond, Target: "live:r-903"}},
		"", ""); err == nil {
		t.Fatal("waiting live on itself should be refused")
	}
	// Free text and the other conditions are not edges.
	addGate(t, s, "r-901", itemgate.Target{Kind: itemgate.KindCond, Target: "room:sg3"})
}

func TestARenameMovesOpenGatesOnly(t *testing.T) {
	s := open(t)
	waiter := addGate(t, s, "r-new-a", item("r-037"))[0]
	target := addGate(t, s, "r-038", item("r-new-a"))[0]
	met := addGate(t, s, "r-039", item("r-new-a"))[0]
	s.GateMet(met.ID, itemgate.MetByAtrium, "landed")

	n, err := s.RenameItem(depRepo, "r-new-a", "r-061", "merge")
	if err != nil || n != 2 {
		t.Fatalf("moved %d, %v", n, err)
	}
	if g, _ := s.Gate(waiter.ID); g.Item != "r-061" {
		t.Fatalf("waiter = %+v", g)
	}
	if g, _ := s.Gate(target.ID); g.Target != "r-061" {
		t.Fatalf("target = %+v", g)
	}
	if g, _ := s.Gate(met.ID); g.Target != "r-new-a" {
		t.Fatalf("a met gate was rewritten: %+v", g)
	}
	// Found under either name, and both names are known.
	if gs, _ := s.Gates(depRepo, "r-new-a", true); len(gs) != 1 || gs[0].ID != waiter.ID {
		t.Fatalf("by old name = %+v", gs)
	}
	names, _ := s.ItemNames(depRepo, "r-new-a")
	if strings.Join(names, ",") != "r-061,r-new-a" {
		t.Fatalf("names = %v", names)
	}
	// A second rename flattens the first.
	if _, err := s.RenameItem(depRepo, "r-061", "r-062", "merge"); err != nil {
		t.Fatal(err)
	}
	names, _ = s.ItemNames(depRepo, "r-new-a")
	if strings.Join(names, ",") != "r-062,r-061,r-new-a" {
		t.Fatalf("names after two renames = %v", names)
	}
	if _, err := s.RenameItem(depRepo, "r-062", "r-061", "merge"); err == nil {
		t.Fatal("renaming back should be refused")
	}
}

func TestARenameOntoTheSameGateKeepsOne(t *testing.T) {
	s := open(t)
	addGate(t, s, "r-038", item("r-new-a"))
	addGate(t, s, "r-038", item("r-061"))
	if _, err := s.RenameItem(depRepo, "r-new-a", "r-061", "merge"); err != nil {
		t.Fatal(err)
	}
	if gs, _ := s.Gates(depRepo, "r-038", true); len(gs) != 1 || gs[0].Target != "r-061" {
		t.Fatalf("gates = %+v", gs)
	}
}

func TestPendingTellsAndTold(t *testing.T) {
	s := open(t)
	agent := addGate(t, s, "r-038", item("r-037"))[0]
	human, err := s.AddGates(depRepo, "r-039", []itemgate.Target{item("r-037")}, "", "")
	if err != nil || human[0].AddedBy != itemgate.AddedByHuman {
		t.Fatalf("human = %+v %v", human, err)
	}
	if has, _ := s.HasOpenGates(); !has {
		t.Fatal("two open gates")
	}
	if p, _ := s.PendingTells(); len(p) != 0 {
		t.Fatalf("nothing is met yet: %+v", p)
	}
	s.GateMet(agent.ID, itemgate.MetByAtrium, "landed")
	s.GateMet(human[0].ID, itemgate.MetByAtrium, "landed")
	p, _ := s.PendingTells()
	if len(p) != 1 || p[0].ID != agent.ID {
		t.Fatalf("pending = %+v, want the agent's only", p)
	}
	if err := s.GateTold([]string{agent.ID}); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PendingTells(); len(p) != 0 {
		t.Fatalf("told twice: %+v", p)
	}
	if has, _ := s.HasOpenGates(); has {
		t.Fatal("nothing is open")
	}
}

func TestAddRefusesWhatIsNotAnItem(t *testing.T) {
	s := open(t)
	for _, c := range []struct {
		repo, item string
		on         []itemgate.Target
	}{
		{"", "r-038", []itemgate.Target{item("r-037")}},
		{depRepo, "not an id", []itemgate.Target{item("r-037")}},
		{depRepo, "r-038", nil},
	} {
		if _, err := s.AddGates(c.repo, c.item, c.on, "", ""); err == nil {
			t.Errorf("%+v should be refused", c)
		}
	}
	if halted, _ := s.Halted(); halted {
		t.Fatal("a refusal halted the store")
	}
}
