//go:build integration

package forge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

type sayCaller struct {
	ctl.Caller
	said chan [4]string
}

func (c sayCaller) DeliverAs(_ context.Context, room, to, _, text, _, kind string, _, _ bool) wire.RelayAnswer {
	c.said <- [4]string{room, to, kind, text}
	return wire.RelayAnswer{OK: true}
}

func TestMergeRefusedSaysToTheOrchestrator(t *testing.T) {
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, _, err := st.RoleSet(hubstore.RoleOrchestrator, "room-c", "orch-card", "test"); err != nil {
		t.Fatal(err)
	}
	c := sayCaller{said: make(chan [4]string, 8)}
	p := New(Calls{Ctl: func() ctl.Caller { return c }, BacklogStore: func() *hubstore.Store { return st },
		BacklogEvent: func(string, any) {}})
	m, _, err := st.MergeAsked(hubstore.MergeAsk{Repo: "github.com/o/r", Branch: "claude/foo", SHA: strings.Repeat("a", 40),
		Requester: "w1", RequesterRoom: "room-e"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.MergeEnd(context.Background(), st, hubstore.MergeStep{N: m.N, From: hubstore.MergeQueued,
		To: hubstore.MergeRefused, Reason: "the branch moved"}); err != nil {
		t.Fatal(err)
	}
	got := map[string][4]string{}
	deadline := time.After(10 * time.Second)
	for len(got) < 2 {
		select {
		case s := <-c.said:
			got[s[0]+" "+s[1]] = s
		case <-deadline:
			t.Fatalf("said only %v", got)
		}
	}
	o, ok := got["room-c orch-card"]
	if !ok || o[2] != "needs" {
		t.Fatalf("the orchestrator was not told as needs: %v", got)
	}
	for _, want := range []string{"claude/foo", "w1@room-e", "refused", "the branch moved"} {
		if !strings.Contains(o[3], want) {
			t.Fatalf("the say %q does not name %q", o[3], want)
		}
	}
	if _, ok := got["room-e w1"]; !ok {
		t.Fatalf("the requester no longer hears: %v", got)
	}
}

func TestALandedRequestSetsItsCardsItemDone(t *testing.T) {
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.ItemFile(hubstore.ItemNew{ID: "f-x", Dept: "docs", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.PutItem(hubstore.Item{ID: "i1", Room: "room-e", Card: "w1", State: "running", Revision: 1,
		OwnerKind: hubstore.OwnerHuman, CreatedBy: "t", WorkID: "f-x"}); err != nil {
		t.Fatal(err)
	}
	mergeItemDone(st, hubstore.MergeRequest{N: 1, Requester: "w1", RequesterRoom: "room-e"})
	it, err := st.ItemGet("f-x")
	if err != nil || it.Status != hubstore.ItemDone {
		t.Fatalf("the item is %q: %v", it.Status, err)
	}
}
