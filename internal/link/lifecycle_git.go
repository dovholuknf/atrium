package link

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/deployready"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE STEPS GIT AND THE BUILDS CAN SEE: landed, deployed, pushed. Item lifecycle L3, docs/rnd/item-lifecycle-design.md
// sections 3 and 4. Each is OBSERVED, never reported by a card:
//
//	landed    the build tip is an ancestor of claude/main on the hub's mirror, at the first commit there that holds it
//	deployed  the hub and every room the item's code runs on report a build that holds the landed sha
//	pushed    origin's main holds the landed sha (one ls-remote, the mirror's main when the forge cannot be asked)
//
// THE SCOPE OF `deployed` IS DEPLOYREADY'S OWN CLASSIFICATION of the item's files: nothing that ships is skipped, hub-only
// code needs the hub, and any other code needs the hub and every room because the room restarts on the same binary. A
// `doc` item, or one whose files are all docs, tests and images, has no deploy and shows `deployed` skipped with why.
//
// DeriveGit is pure. It is the git half of the design's Derive, in its own file so the step engine can call it as it
// stands: it takes what was read and answers three steps, and nothing it reads changes.

const (
	StepLanded   = "landed"
	StepDeployed = "deployed"
	StepPushed   = "pushed"
)

// BuildSeen is one process's running commit as the hub compared it with the landed sha.
type BuildSeen struct {
	// Name is the room's name, or "hub".
	Name string `json:"name"`
	// Commit is what it reported, empty from a build that does not say.
	Commit string `json:"commit,omitempty"`
	// Contains is that the commit holds the landed sha. Known is false when the mirror does not have the commit, which
	// is a build older than anything the hub holds.
	Contains bool `json:"contains"`
	Known    bool `json:"known"`
}

// GitInput is everything DeriveGit reads.
type GitInput struct {
	Kind  string
	Facts gitsync.LandFacts
	// HaveBranch is that the item has a build branch the hub holds, so `landed` is being waited on rather than unstarted.
	HaveBranch bool
	Hub        BuildSeen
	Rooms      []BuildSeen
}

// GitStep is one derived step.
type GitStep struct {
	Step   string
	State  string
	SHA    string
	Reason string
	// Waiting are the processes whose build lacks the landed sha, for `deployed`, in name order.
	Waiting []string
}

// Scope is where an item's files deploy: whether the hub must run them, whether the rooms must, and the words when
// neither does.
func Scope(kind string, paths []string) (hub, rooms bool, skip string) {
	if kind == "doc" {
		return false, false, "a doc item: nothing in it ships, so there is no deploy"
	}
	shipped := false
	for _, p := range paths {
		if !deployready.Needs(p) {
			continue
		}
		shipped = true
		if deployready.RoomSide(p) {
			rooms = true
		}
	}
	switch {
	case len(paths) == 0:
		// NOT KNOWING IS NOT SKIPPING. A deploy that is wrongly shown as not needed is the one that never happens, so an
		// item whose files are unknown is taken to need the hub and every room, as the deploy queue takes an unreported room.
		return true, true, ""
	case !shipped:
		return false, false, "its files are docs, tests and images only: nothing that ships changed, so there is no deploy"
	}
	return true, rooms, ""
}

// DeriveGit answers landed, deployed and pushed, in that order.
func DeriveGit(in GitInput) []GitStep {
	landed := GitStep{Step: StepLanded, State: hubstore.StepPending}
	switch {
	case in.Facts.Landed:
		landed.State, landed.SHA = hubstore.StepDone, in.Facts.LandedSHA
	case in.HaveBranch && in.Facts.SHA != "":
		landed.State, landed.SHA = hubstore.StepActive, in.Facts.SHA
		landed.Reason = "the branch tip is not on claude/main yet"
	}
	deployed := GitStep{Step: StepDeployed, State: hubstore.StepPending}
	pushed := GitStep{Step: StepPushed, State: hubstore.StepPending}
	if landed.State != hubstore.StepDone {
		return []GitStep{landed, deployed, pushed}
	}
	deployed.SHA, pushed.SHA = landed.SHA, landed.SHA
	needHub, needRooms, skip := Scope(in.Kind, in.Facts.Paths)
	if skip != "" {
		deployed.State, deployed.Reason = hubstore.StepSkipped, skip
	} else {
		var behind []BuildSeen
		if needHub && !in.Hub.Contains {
			behind = append(behind, in.Hub)
		}
		if needRooms {
			for _, r := range in.Rooms {
				if !r.Contains {
					behind = append(behind, r)
				}
			}
		}
		if len(behind) == 0 {
			deployed.State = hubstore.StepDone
		} else {
			deployed.State = hubstore.StepActive
			sort.Slice(behind, func(i, j int) bool { return behind[i].Name < behind[j].Name })
			var words []string
			for _, b := range behind {
				deployed.Waiting = append(deployed.Waiting, b.Name)
				words = append(words, b.Name+" "+buildWords(b))
			}
			deployed.Reason = "waiting on " + strings.Join(words, ", ")
		}
	}
	if in.Facts.Pushed {
		pushed.State = hubstore.StepDone
	} else {
		pushed.State = hubstore.StepActive
		pushed.Reason = "origin's main does not hold it yet"
		if in.Facts.PushedVia == "mirror" && in.Facts.PushedNote != "" {
			pushed.Reason += " (read from the mirror: " + in.Facts.PushedNote + ")"
		}
	}
	return []GitStep{landed, deployed, pushed}
}

func buildWords(b BuildSeen) string {
	switch {
	case b.Commit == "":
		return "(does not report a build)"
	case !b.Known:
		return "(on " + shortSHA(b.Commit) + ", older than the mirror holds)"
	}
	return "(on " + shortSHA(b.Commit) + ")"
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// ── reading it ──────────────────────────────────────────

// LandGit is the hub's repository as the lifecycle reads landing, pushing and builds. Nil is no git: the steps stay
// pending.
type LandGit interface {
	LandFacts(ctx context.Context, branch, sha, workID string) (gitsync.LandFacts, error)
	Contains(ctx context.Context, build, sha string) (contains, known bool, err error)
}

// SetLandGit wires the repository the git steps read.
func (p *Proxy) SetLandGit(g LandGit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.landGit = g
}

// SetHubBuild says which commit this hub's own binary was built from, for `deployed`. Empty says nothing, and the hub is
// then counted as not holding the sha.
func (p *Proxy) SetHubBuild(commit string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hubBuild = strings.TrimSpace(commit)
}

// RefreshGitSteps derives the git steps of one item and stores them. An item with no branch has none to derive and is
// left alone. It answers the rows it stored.
func (p *Proxy) RefreshGitSteps(ctx context.Context, id string) ([]hubstore.WorkStepRow, error) {
	st := p.itemStore()
	p.mu.Lock()
	g, hubBuild := p.landGit, p.hubBuild
	p.mu.Unlock()
	if st == nil || g == nil {
		return nil, nil
	}
	w, err := st.WorkByID(id)
	if err != nil || w.Branch == "" {
		return nil, err
	}
	facts, err := g.LandFacts(ctx, w.Branch, "", w.BacklogID)
	if err != nil {
		return nil, fmt.Errorf("git could not be read for %s: %w", id, err)
	}
	if len(facts.Paths) > 0 {
		_ = st.PutWorkPaths(id, facts.Paths)
	} else if facts.Landed {
		facts.Paths, _ = st.WorkPaths(id)
	}
	in := GitInput{Kind: w.Kind, Facts: facts, HaveBranch: facts.SHA != ""}
	if facts.Landed {
		in.Hub = p.seen(ctx, g, "hub", hubBuild, facts.LandedSHA)
		if p.hub != nil {
			for _, a := range p.hub.Rooms() {
				in.Rooms = append(in.Rooms, p.seen(ctx, g, a.Name, a.Commit, facts.LandedSHA))
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var out []hubstore.WorkStepRow
	for _, s := range DeriveGit(in) {
		row := hubstore.WorkStepRow{BacklogID: id, Step: s.Step, State: s.State, SHA: s.SHA, Reason: s.Reason, Cards: "[]"}
		if s.State != hubstore.StepPending {
			row.StartedAt = now
		}
		if s.State == hubstore.StepDone || s.State == hubstore.StepSkipped {
			row.EndedAt = now
		}
		if err := st.PutWorkStep(row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// seen compares one process's build with the landed sha. A build that is not a full sha, or that the mirror cannot read,
// does not hold it.
func (p *Proxy) seen(ctx context.Context, g LandGit, name, commit, landed string) BuildSeen {
	b := BuildSeen{Name: name, Commit: strings.ToLower(strings.TrimSpace(commit))}
	if !gitsync.ValidSHA(b.Commit) {
		return b
	}
	b.Contains, b.Known, _ = g.Contains(ctx, b.Commit, landed)
	return b
}

// RefreshAllGitSteps derives every item that still has something to learn. It is what a collect, a mirror pass and a room
// dialing in call, and it never fails the caller: a read that cannot be made leaves the stored steps as they were.
func (p *Proxy) RefreshAllGitSteps(ctx context.Context) {
	st := p.itemStore()
	if st == nil {
		return
	}
	ids, err := st.WorkIdsOpen()
	if err != nil {
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		_, _ = p.RefreshGitSteps(ctx, id)
	}
}

// gitStepsTick is how often the git steps are read when nothing told the hub to: the mirror's own pace.
const gitStepsTick = 30 * time.Second

// RunGitSteps derives the git steps every gitStepsTick until ctx ends. A collect, a mirror pass and a room attaching
// derive sooner, through RefreshAllGitSteps.
func (p *Proxy) RunGitSteps(ctx context.Context) {
	t := time.NewTicker(gitStepsTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.RefreshAllGitSteps(ctx)
		}
	}
}
