//go:build integration

package link

import (
	"context"
	"errors"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// docFiles is a DocsFileGit and a LandGit that answers from a table.
type docFiles struct {
	LandGit
	files map[string]string
	fail  bool
}

func (d docFiles) FileAt(_ context.Context, sha, path string) (string, bool, error) {
	if d.fail {
		return "", false, errors.New("git failed")
	}
	s, ok := d.files[sha+":"+path]
	return s, ok, nil
}

const docsLandedSHA = "8888888888888888888888888888888888888888"

// landWork makes the work row of an item, as an assignment pushed by a room does, and writes its landed step.
func landWork(t *testing.T, x *itemRig, id, kind string, done bool) {
	t.Helper()
	st := x.st
	if code, body := x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-" + id, "room": "sg4", "card": "c-" + id,
		"generation": 1, "state": "finished", "revision": 1, "owner_kind": "session", "owner": "orch", "created_by": "orch",
		"note": "n", "work_id": id}), loopAddr); code != 200 {
		t.Fatalf("sync: %d %s", code, body)
	}
	if err := st.SetWorkKind(id, kind); err != nil {
		t.Fatal(err)
	}
	state := hubstore.StepPending
	if done {
		state = hubstore.StepDone
	}
	if err := st.PutWorkStep(hubstore.WorkStepRow{BacklogID: id, Step: hubstore.StepLanded, State: state, SHA: docsLandedSHA, Cards: "[]"}); err != nil {
		t.Fatal(err)
	}
}

func TestDocsOfNotesAFeatureWhoseNoteSaysNothing(t *testing.T) {
	x := newItemRig(t)
	st := x.st
	landWork(t, x, "f-one", "code", true)
	note := docsLandedSHA + ":docs/changes/f-one.md"
	for name, tc := range map[string]struct {
		g    docFiles
		want bool
	}{
		"no line":      {docFiles{files: map[string]string{note: "# x\n\n## Test plan\n"}}, true},
		"an id":        {docFiles{files: map[string]string{note: "# x\nDocs: d-3\n## Test plan\n"}}, false},
		"none and why": {docFiles{files: map[string]string{note: "# x\nDocs: none, why\n## Test plan\n"}}, false},
		"folded away":  {docFiles{files: map[string]string{}}, false},
		"git failed":   {docFiles{fail: true}, false},
	} {
		x.p.SetLandGit(tc.g)
		got := x.p.docsOf(context.Background(), st, "f-one")
		if (got != nil && got.NotSaid) != tc.want || (got != nil && got.Offer != nil) {
			t.Errorf("%s: docs = %+v, want not_said %v", name, got, tc.want)
		}
	}
	x.p.SetLandGit(nil)
	if got := x.p.docsOf(context.Background(), st, "f-one"); got != nil {
		t.Errorf("no git said %+v", got)
	}
}

func TestDocsOfOffersToCloseTheDocItemOfALandedDocWorker(t *testing.T) {
	x := newItemRig(t)
	st := x.st
	if _, err := st.ItemFile(hubstore.ItemNew{ID: "d-7", Dept: "docs", Title: "docs behind f-one", FiledBy: "t"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	landWork(t, x, "d-7", "doc", false)
	if got := x.p.docsOf(ctx, st, "d-7"); got != nil {
		t.Errorf("offered before landing: %+v", got)
	}
	landWork(t, x, "d-7", "doc", true)
	if got := x.p.docsOf(ctx, st, "d-7"); got == nil || got.Offer == nil || got.Offer.ID != "d-7" || got.NotSaid {
		t.Errorf("landed: %+v", got)
	}
	if _, err := st.ItemSetStatus("d-7", hubstore.ItemDone, "t"); err != nil {
		t.Fatal(err)
	}
	if got := x.p.docsOf(ctx, st, "d-7"); got != nil {
		t.Errorf("offered after it was closed: %+v", got)
	}
}
