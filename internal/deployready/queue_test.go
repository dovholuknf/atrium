package deployready

import (
	"context"
	"strings"
	"testing"
)

func entryFor(q Queue, sha string) *QueueEntry {
	for i := range q.Entries {
		if q.Entries[i].SHA == sha {
			return &q.Entries[i]
		}
	}
	return nil
}

func TestQueueSaysWhichDeployEachCommitNeeds(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a/a.go": "package a\n"})
	hubCode := r.commit("hub only", map[string]string{"internal/hubstore/h.go": "package h\n",
		"changelog/fabric/2026-10-04-f-hub.md": "x\n"})
	both := r.commit("shared code", map[string]string{"internal/a/a.go": "package a // 2\n",
		"changelog/fabric/2026-10-04-f-both.md": "x\n"})
	r.commit("docs only", map[string]string{"docs/x.md": "x\n"})

	// The hub runs the tip of the code. A room runs base.
	q := r.checker().Queue(context.Background(), both, []Live{{Name: "m1", Commit: base}})
	if q.Error != "" {
		t.Fatal(q.Error)
	}
	// hubCode is hub-only and the hub already runs it, so the room never needs it and it is not queued.
	if len(q.Entries) != 1 {
		t.Fatalf("entries = %+v", q.Entries)
	}
	if e := entryFor(q, hubCode); e != nil {
		t.Fatalf("a hub-only commit already on the hub should not be queued, got %+v", e)
	}
	e := entryFor(q, both)
	if e == nil || e.Needs != NeedsRoom || e.Item != "f-both" || len(e.Rooms) != 1 || e.Rooms[0] != "m1" {
		t.Fatalf("shared commit = %+v", e)
	}
}

func TestQueueBothWhenHubAndRoomAreBehind(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a/a.go": "package a\n"})
	hubOnly := r.commit("hub only", map[string]string{"internal/hubstore/h.go": "package h\n"})
	shared := r.commit("shared", map[string]string{"internal/a/a.go": "package a // 2\n"})

	q := r.checker().Queue(context.Background(), base, []Live{{Name: "m1", Commit: base}, {Name: "m2", Commit: shared}})
	if e := entryFor(q, hubOnly); e == nil || e.Needs != NeedsHub || len(e.Rooms) != 0 {
		t.Fatalf("hub-only commit = %+v", e)
	}
	e := entryFor(q, shared)
	if e == nil || e.Needs != NeedsBoth || len(e.Rooms) != 1 || e.Rooms[0] != "m1" {
		t.Fatalf("shared commit = %+v", e)
	}
	if !strings.Contains(q.Line, "2 landed, 1 hub, 0 room, 1 both") {
		t.Fatalf("line = %q", q.Line)
	}
}

func TestQueueEmptyWhenEverythingIsLive(t *testing.T) {
	r := newRepo(t)
	r.commit("base", map[string]string{"internal/a/a.go": "package a\n"})
	tip := r.commit("more", map[string]string{"internal/a/a.go": "package a // 2\n"})
	q := r.checker().Queue(context.Background(), tip, []Live{{Name: "m1", Commit: tip}})
	if q.Error != "" || len(q.Entries) != 0 || !strings.HasPrefix(q.Line, "deploy queue empty") {
		t.Fatalf("queue = %+v", q)
	}
}

func TestQueueTakesARoomThatDoesNotReportAsBehind(t *testing.T) {
	r := newRepo(t)
	base := r.commit("base", map[string]string{"internal/a/a.go": "package a\n"})
	shared := r.commit("shared", map[string]string{"internal/a/a.go": "package a // 2\n"})
	q := r.checker().Queue(context.Background(), base, []Live{{Name: "old", Commit: ""}})
	if len(q.Unreported) != 1 || q.Unreported[0] != "old" {
		t.Fatalf("unreported = %v", q.Unreported)
	}
	if e := entryFor(q, shared); e == nil || e.Needs != NeedsBoth || e.Rooms[0] != "old" {
		t.Fatalf("entry = %+v", e)
	}
	if md := q.Markdown(); !strings.Contains(md, "did not report a commit") || !strings.Contains(md, "| both (old) |") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestQueueUnknownWhenTheHubCommitIsNotInTheCheckout(t *testing.T) {
	r := newRepo(t)
	r.commit("base", map[string]string{"internal/a/a.go": "package a\n"})
	for _, hub := range []string{"", "not-a-sha", strings.Repeat("a", 40)} {
		q := r.checker().Queue(context.Background(), hub, nil)
		if q.Error == "" || !strings.HasPrefix(q.Line, "deploy queue unknown") || len(q.Entries) != 0 {
			t.Fatalf("hub %q: %+v", hub, q)
		}
	}
}
