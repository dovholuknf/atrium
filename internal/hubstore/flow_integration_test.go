//go:build integration

package hubstore

import (
	"errors"
	"testing"
)

func TestRoleSetReplacesTheHolderButNotTesters(t *testing.T) {
	s := open(t)
	if _, rep, err := s.RoleSet(RoleOrchestrator, "SG4", "c1", "clint"); err != nil || len(rep) != 0 {
		t.Fatalf("first: %v %v", rep, err)
	}
	h, rep, err := s.RoleSet(RoleOrchestrator, "sg4", "c2", "clint")
	if err != nil || len(rep) != 1 || rep[0].Card != "c1" || h.Card != "c2" || h.Room != "sg4" {
		t.Fatalf("replace: %+v %+v %v", h, rep, err)
	}
	if ok, _ := s.IsHolder(RoleOrchestrator, "sg4", "c1"); ok {
		t.Fatal("the replaced holder still holds it")
	}
	if ok, _ := s.IsHolder(RoleOrchestrator, "SG4", "c2"); !ok {
		t.Fatal("the new holder does not hold it")
	}
	s.RoleSet(RoleTester, "sg3", "t1", "orch")
	s.RoleSet(RoleTester, "m1mini", "t2", "orch")
	if got, _ := s.Roles(RoleTester); len(got) != 2 {
		t.Fatalf("testers: %+v", got)
	}
	if err := s.RoleClear(RoleOrchestrator, "sg4", "c2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Roles(""); len(got) != 2 {
		t.Fatalf("all roles: %+v", got)
	}
}

func TestRoleSetRefusesWhatIsNotARole(t *testing.T) {
	s := open(t)
	if _, _, err := s.RoleSet("boss", "sg4", "c1", ""); !IsRefusal(err) {
		t.Fatalf("role: %v", err)
	}
	if _, _, err := s.RoleSet(RoleOrchestrator, "", "c1", ""); !IsRefusal(err) {
		t.Fatalf("room: %v", err)
	}
	if _, _, err := s.RoleSet(RoleMerger, "sg4", "c1", "clint"); !errors.Is(err, ErrMergerRetired) {
		t.Fatalf("merger: %v", err)
	}
}

func TestStageMovesForwardOnlyAndKeepsWhatItWasGiven(t *testing.T) {
	s := open(t)
	if _, err := s.FlowSetStage(FlowSet{ItemID: "nope", Stage: "idea"}); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("no item: %v", err)
	}
	s.ItemFile(newItem("f-x", "fabric"))
	f, err := s.FlowSetStage(FlowSet{ItemID: "f-x", Stage: "implementing", By: "orch"})
	if err != nil || f.Stage != "implementing" {
		t.Fatalf("set: %+v %v", f, err)
	}
	f, err = s.FlowSetStage(FlowSet{ItemID: "f-x", Stage: "implemented", Branch: "claude/x", SHA: "abc1234", By: "w"})
	if err != nil || f.Branch != "claude/x" || f.SHA != "abc1234" {
		t.Fatalf("implemented: %+v %v", f, err)
	}
	f, err = s.FlowSetStage(FlowSet{ItemID: "f-x", Stage: "implemented", Note: "again"})
	if err != nil || f.Branch != "claude/x" || f.Note != "again" {
		t.Fatalf("same stage keeps the branch: %+v %v", f, err)
	}
	if _, err := s.FlowSetStage(FlowSet{ItemID: "f-x", Stage: "designed"}); !errors.Is(err, ErrStageBackwards) || !IsRefusal(err) {
		t.Fatalf("backwards: %v", err)
	}
	if _, err := s.FlowSetStage(FlowSet{ItemID: "f-x", Stage: "nonsense"}); !IsRefusal(err) {
		t.Fatalf("stage: %v", err)
	}
	if _, err := s.FlowSetStage(FlowSet{ItemID: "f-x", Stage: "merged", SHA: "XYZ"}); !IsRefusal(err) {
		t.Fatalf("sha: %v", err)
	}
	if list, _ := s.FlowList("implemented"); len(list) != 1 {
		t.Fatalf("list: %+v", list)
	}
	if _, err := s.FlowGet("f-y"); !errors.Is(err, ErrFlowItemNotFound) {
		t.Fatalf("get: %v", err)
	}
}

func TestChecksAndDeploysAreKeyedBySha(t *testing.T) {
	s := open(t)
	if _, err := s.CheckAdd("abc1234", "sanity", "pass", "merger"); err != nil {
		t.Fatal(err)
	}
	s.CheckAdd("abc1234", "go-tests", "fail", "tester")
	s.CheckAdd("def5678", "sanity", "timeout", "merger")
	if got, _ := s.Checks("abc1234", 0); len(got) != 2 || got[0].Name != "go-tests" {
		t.Fatalf("checks: %+v", got)
	}
	if _, err := s.CheckAdd("abc1234", "Sanity!", "pass", ""); !IsRefusal(err) {
		t.Fatalf("name: %v", err)
	}
	if _, err := s.CheckAdd("abc1234", "sanity", "maybe", ""); !IsRefusal(err) {
		t.Fatalf("result: %v", err)
	}
	s.DeploySet("SG4", "abc1234", "merger")
	d, err := s.DeploySet("sg4", "def5678", "merger")
	if err != nil || d.SHA != "def5678" {
		t.Fatalf("deploy: %+v %v", d, err)
	}
	if got, _ := s.Deploys(); len(got) != 1 {
		t.Fatalf("deploys: %+v", got)
	}
}

func TestFlowRepoIsAStoreNameOrEmpty(t *testing.T) {
	s := open(t)
	if got, _ := s.FlowRepo(); got != "" {
		t.Fatalf("unset reads %q", got)
	}
	if err := s.SetFlowRepo("github.com/dovholuknf/atrium"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.FlowRepo(); got != "github.com/dovholuknf/atrium" {
		t.Fatalf("reads %q", got)
	}
	if err := s.SetFlowRepo("../etc"); !IsRefusal(err) {
		t.Fatalf("bad name: %v", err)
	}
	if err := s.SetFlowRepo(""); err != nil {
		t.Fatal(err)
	}
}

func TestEpochOpensOnceAndClosesWithTheTips(t *testing.T) {
	s := open(t)
	if _, err := s.EpochOpenNow(); !errors.Is(err, ErrNoEpoch) {
		t.Fatalf("none open: %v", err)
	}
	e, err := s.EpochOpen("abc1234", "clint")
	if err != nil || e.N != 1 || e.State != "open" {
		t.Fatalf("open: %+v %v", e, err)
	}
	if _, err := s.EpochOpen("def5678", "clint"); !errors.Is(err, ErrEpochOpen) || !IsRefusal(err) {
		t.Fatalf("second open: %v", err)
	}
	if _, err := s.EpochOpen("zz", "clint"); !IsRefusal(err) {
		t.Fatalf("bad sha: %v", err)
	}
	c, err := s.EpochClose(1, "abc1234", "def5678", "merger")
	if err != nil || c.State != "closed" || c.OldTip != "abc1234" || c.NewTip != "def5678" || c.ClosedAt == "" {
		t.Fatalf("close: %+v %v", c, err)
	}
	if _, err := s.EpochClose(1, "abc1234", "def5678", "merger"); !errors.Is(err, ErrNoEpoch) {
		t.Fatalf("close twice: %v", err)
	}
	if e, err = s.EpochOpen("def5678", "clint"); err != nil || e.N != 2 {
		t.Fatalf("next round: %+v %v", e, err)
	}
	if got, _ := s.Epochs(0); len(got) != 2 || got[0].N != 2 {
		t.Fatalf("epochs: %+v", got)
	}
}
