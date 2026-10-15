package link

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE ITEM AT THE TOP OF A BRIEF (item lifecycle L8). A card attached to an item starts with where the item stands: its
// step lines and the folder that holds its logs, so "where are we" is answered by reading BRIEF.md and not by reading git.
// It replaces the change record's inventory handed to a new card, and `atrium_item show` prints the same lines, so a card
// can ask again later and the brief is never the only copy. Nothing here is stored: the lines are made from the hub's
// `GET /_hub/work/<id>` at launch. A hub that cannot answer costs the brief its header and nothing else.
// See docs/rnd/item-lifecycle-design.md sections 5 and 8.

// ItemFolder is where an item's logs live on a machine, `~/.atrium/items/<backlog id>/`.
func ItemFolder(id string) string { return "~/.atrium/items/" + strings.TrimSpace(id) + "/" }

// WorkDetail is the part of `GET /_hub/work/<id>` the brief reads.
type WorkDetail struct {
	Work  hubstore.Work          `json:"work"`
	Facts []hubstore.WorkFact    `json:"facts"`
	Steps []hubstore.WorkStepRow `json:"steps"`
	Life  struct {
		State   string `json:"state"`
		Current string `json:"current"`
		Waiting string `json:"waiting,omitempty"`
	} `json:"lifecycle"`
}

// itemBriefWait bounds the read. The hub refreshes the git steps on this route, so it is slower than most, but the header
// is optional and a launch waits on it, so the wait is short. A var so a test can shrink it.
var itemBriefWait = 5 * time.Second

// itemBriefLate is the one line a brief carries when the hub did not answer in time.
const itemBriefLate = "_Item steps unavailable: the hub did not answer in time. `atrium_item show` prints them when it does._\n"

// FetchWorkDetail reads one item from the hub.
func FetchWorkDetail(ctx context.Context, call HubCall, id string) (WorkDetail, error) {
	var d WorkDetail
	ctx, cancel := context.WithTimeout(ctx, itemBriefWait)
	defer cancel()
	err := call(ctx, "GET", "/_hub/work/"+url.PathEscape(strings.TrimSpace(id)), nil, &d)
	return d, err
}

// ItemBrief is the text to put above a brief for a card attached to id, or "" when the hub cannot say. It never fails a
// launch: a launch is not refused for a hub being slow.
func ItemBrief(ctx context.Context, call HubCall, id string) string {
	if strings.TrimSpace(id) == "" || call == nil {
		return ""
	}
	d, err := FetchWorkDetail(ctx, call, id)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return itemBriefLate
	}
	if err != nil || d.Work.BacklogID == "" {
		return ""
	}
	return RenderItemBrief(d)
}

// WithItemBrief puts the item's header ahead of a brief, once. A brief that has one already is returned as it is.
func WithItemBrief(brief, header string) string {
	if header == "" || strings.Contains(brief, itemBriefMark) {
		return brief
	}
	return header + "\n" + brief
}

const itemBriefMark = "## Where this item stands"

// RenderItemBrief is the header: the item, one line per step, and the folder.
func RenderItemBrief(d WorkDetail) string {
	var b strings.Builder
	b.WriteString(itemBriefMark + "\n\n")
	fmt.Fprintf(&b, "Item `%s`: %s", d.Work.BacklogID, d.Work.Title)
	if d.Life.State != "" {
		b.WriteString(" (" + d.Life.State)
		if d.Life.Current != "" {
			b.WriteString(", at " + d.Life.Current)
		}
		b.WriteString(")")
	}
	b.WriteString("\n")
	if d.Work.Branch != "" {
		fmt.Fprintf(&b, "Branch: `%s`\n", d.Work.Branch)
	}
	b.WriteString("\n")
	for _, l := range StepLines(d) {
		b.WriteString("- " + l + "\n")
	}
	fmt.Fprintf(&b, "\nItem folder: `%s` (logs recorded against this item). Read this section for \"where are we\"; "+
		"`atrium_item show %s` prints it again, current.\n", ItemFolder(d.Work.BacklogID), d.Work.BacklogID)
	return b.String()
}

// StepLines is one line per step, in order, from the stored step rows: `tested: done at 1a2b3c4 (observed). ...`. A step
// the hub has no row for reads as pending.
func StepLines(d WorkDetail) []string {
	by := map[string]hubstore.WorkStepRow{}
	for _, s := range d.Steps {
		by[s.Step] = s
	}
	out := make([]string, 0, len(hubstore.Steps))
	for _, name := range hubstore.Steps {
		s, ok := by[name]
		if !ok {
			s = hubstore.WorkStepRow{Step: name, State: hubstore.StepPending}
		}
		out = append(out, stepLine(name, s, d.Facts))
	}
	return out
}

func stepLine(name string, s hubstore.WorkStepRow, facts []hubstore.WorkFact) string {
	var b strings.Builder
	b.WriteString(name + ": " + s.State)
	if s.Party != "" && s.State == hubstore.StepActive {
		b.WriteString(", waiting on " + s.Party)
	}
	if s.SHA != "" {
		b.WriteString(" at " + shortSHA(s.SHA))
	}
	if s.Reason != "" {
		b.WriteString(". " + strings.TrimRight(s.Reason, ". "))
	}
	if extra := stepFactLine(name, facts); extra != "" {
		b.WriteString(". " + extra)
	}
	return b.String()
}

// stepFactLine IS THE SEAM FOR L4, tested and reviewed facts, and the one place it plugs in. L4 records `test` and `review`
// facts (atrium_record, review trailers) stamped with the sha they were about and a seen or reported source. Until then
// this reads whatever facts of those kinds the hub holds, with only the columns every fact has, and says nothing for a
// step with none. L4 replaces the body: how many commits ago, the dirty flag, the findings count.
func stepFactLine(step string, facts []hubstore.WorkFact) string {
	kind := map[string]string{hubstore.StepTested: "test", hubstore.StepReviewed: "review"}[step]
	if kind == "" {
		return ""
	}
	var last *hubstore.WorkFact
	for i := range facts {
		if facts[i].Kind == kind {
			last = &facts[i]
		}
	}
	if last == nil {
		return ""
	}
	s := kind + " recorded (" + last.Source + ")"
	if last.AtSHA != "" {
		s += " at " + shortSHA(last.AtSHA)
	}
	if last.Dirty {
		s += " with uncommitted changes"
	}
	return s
}
