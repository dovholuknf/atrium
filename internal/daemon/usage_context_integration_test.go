//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// r-021: after a /clear the card's resume id stays on the old conversation until
// the new one's first Stop. The popup's context_now must be the session the
// runner last started, as contextsize.go reads it (item 62), not the resume id.
func TestUsageContextNowFollowsTheStartedSession(t *testing.T) {
	f := newUsageFix(t)
	dir := filepath.Dir(f.path)
	old := filepath.Join(dir, "s1.jsonl")
	fresh := filepath.Join(dir, "s2.jsonl")
	f.u.transcript = func(cwd, id string) string {
		switch id {
		case "s1":
			return old
		case "s2":
			return fresh
		}
		return ""
	}
	// The old conversation ended big, the new one is small.
	f.lineTo(old, "claude-opus-5-5", f.base, "m1", 0, 0, 700000, 10, false)
	f.lineTo(fresh, "claude-opus-5-5", f.base.Add(1), "m2", 0, 0, 100000, 10, false)
	t.Cleanup(func() { os.Remove(old); os.Remove(fresh) })

	d := &Daemon{st: f.st, usage: f.u, ctx: newContextSizes()}

	// Before any new session is heard of, it is the resume id's transcript.
	v, err := d.usageFor(f.task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	before := v.(*UsageView).ContextNow
	if before < 700000 {
		t.Fatalf("context_now before the clear %d, want the old transcript's", before)
	}

	// The runner started a new session (a /clear). The resume id has not moved.
	d.ctx.started(f.task.ID, "s2")
	v, err = d.usageFor(f.task.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	after := v.(*UsageView).ContextNow
	if after >= 700000 || after < 100000 {
		t.Fatalf("context_now after the clear %d, want the new transcript's (about 100k)", after)
	}
}
