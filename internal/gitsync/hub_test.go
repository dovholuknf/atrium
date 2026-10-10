package gitsync

import (
	"errors"
	"testing"
)

// A failure is audited when it changes, and so is the recovery. Repeats stay in memory.
func TestAFailureIsAuditedOnlyWhenItChanges(t *testing.T) {
	var rows []string
	h := &Hub{Audit: func(room, kind, detail string) { rows = append(rows, kind+" "+detail) }}
	boom := errors.New("no route")
	for i := 0; i < 20; i++ {
		h.auditFailure("sg3", "git-collected", "collect:sg3", boom)
	}
	if len(rows) != 1 || rows[0] != "git-collected failed: no route" {
		t.Fatalf("rows = %v", rows)
	}
	h.auditFailure("sg3", "git-collected", "collect:sg3", errors.New("other words"))
	h.auditFailure("sg3", "git-collected", "collect:sg3", errors.New("other words"))
	if len(rows) != 2 {
		t.Fatalf("a changed failure was not written once: %v", rows)
	}
	h.auditFailure("sg3", "git-collected", "collect:sg3", nil)
	h.auditFailure("sg3", "git-collected", "collect:sg3", nil)
	if len(rows) != 3 || rows[2] != "git-collected recovered" {
		t.Fatalf("recovery: %v", rows)
	}
	// Another key is its own thread.
	h.auditFailure("m1mini", "git-collected", "collect:m1mini", boom)
	if len(rows) != 4 {
		t.Fatalf("rows = %v", rows)
	}
}
