package link

import (
	"context"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// What git says of an item, folded into the item's stage.
//
// THREE STAGES COME FROM GIT, TWO ARE DERIVED FROM SHA-KEYED RECORDS, and nobody has to remember a call for any of
// them: reading an item computes the stage, and a stage that git or the records say is further on than the one stored
// is stored (only forward, by `atrium (git)`).
//
//	implemented      the hub holds the item's commit
//	merged           claude/main has all of it, by ancestry or by patch
//	tested           merged, and a passing `sanity` or other check was recorded on a sha that has all of it
//	deployed         tested, and every room that has reported a deploy runs a sha that has all of it
//	pushed-by-clint  main (the mirror of origin/main, only the operator moves it) has all of it

// FlowGit is the hub's repository as the flow reads it. Nil is no git: the stage is what was reported.
type FlowGit interface {
	ItemFacts(ctx context.Context, branch, sha string) (gitsync.ItemFacts, error)
	CoveredBy(ctx context.Context, base, sha string) (bool, error)
	// EpochPlan is what a reset of claude/main onto main has to carry over. EpochMove does the reset, as epoch n, onto
	// the tip of branch ("" for main), when claude/main is still expectOld. See flow_epoch.go.
	EpochPlan(ctx context.Context) (gitsync.EpochPlan, error)
	EpochMove(ctx context.Context, n int64, branch, expectOld string) (oldTip, newTip string, err error)
}

// SetFlowGit wires the repository the merge flow reads.
func (p *Proxy) SetFlowGit(g FlowGit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flowGit = g
}

func (p *Proxy) flowGitOf() FlowGit {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.flowGit
}

// FlowView is an item as the flow reads it: the stored record with the stage git and the records give it.
type FlowView struct {
	hubstore.FlowItem
	Git *gitsync.ItemFacts `json:"git,omitempty"`
	// TestedOn is the check shas that cover the item, DeployedOn the rooms that run it, BehindRooms the rooms that
	// have reported a deploy and do not run it yet.
	TestedOn    []string `json:"tested_on,omitempty"`
	DeployedOn  []string `json:"deployed_on,omitempty"`
	BehindRooms []string `json:"behind_rooms,omitempty"`
	Note        string   `json:"git_note,omitempty"`
}

const gitStageBy = "atrium (git)"

// flowRefresh reads one item, computes its stage and stores a stage that is further on than the stored one.
func (p *Proxy) flowRefresh(ctx context.Context, st *hubstore.Store, id string) (FlowView, error) {
	item, err := st.FlowGet(id)
	if err != nil {
		return FlowView{}, err
	}
	v := FlowView{FlowItem: item}
	g := p.flowGitOf()
	if g == nil || (item.Branch == "" && item.SHA == "") {
		return v, nil
	}
	f, err := g.ItemFacts(ctx, item.Branch, item.SHA)
	if err != nil {
		v.Note = "git could not be read: " + err.Error()
		return v, nil
	}
	v.Git = &f
	want := item.Stage
	raise := func(stage string) {
		if hubstore.StageIndex(stage) > hubstore.StageIndex(want) {
			want = stage
		}
	}
	if f.Implemented {
		raise("implemented")
	}
	if f.Merged {
		raise("merged")
		checks, _ := st.Checks("", 100)
		seen := map[string]bool{}
		for _, c := range checks {
			if c.Result != "pass" || seen[c.SHA] {
				continue
			}
			if ok, _ := g.CoveredBy(ctx, c.SHA, f.SHA); ok {
				seen[c.SHA] = true
				v.TestedOn = append(v.TestedOn, c.SHA)
			}
		}
		if len(v.TestedOn) > 0 {
			raise("tested")
		}
		if ds, _ := st.Deploys(); len(ds) > 0 && len(v.TestedOn) > 0 {
			for _, d := range ds {
				if ok, _ := g.CoveredBy(ctx, d.SHA, f.SHA); ok {
					v.DeployedOn = append(v.DeployedOn, d.Room)
				} else {
					v.BehindRooms = append(v.BehindRooms, d.Room)
				}
			}
			if len(v.BehindRooms) == 0 {
				raise("deployed")
			}
		}
	}
	if f.PushedByClint {
		raise("pushed-by-clint")
	}
	if want != item.Stage {
		moved, err := st.FlowSetStage(hubstore.FlowSet{ItemID: id, Stage: want, SHA: f.SHA, By: gitStageBy})
		if err != nil {
			return v, err
		}
		v.FlowItem = moved
		p.backlogEvent("flow", map[string]string{"id": id, "stage": moved.Stage})
	}
	return v, nil
}
