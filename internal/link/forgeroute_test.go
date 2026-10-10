package link

import (
	"context"
	"sync"

	"github.com/dovholuknf/atrium/internal/forge"
)

// fakeForge is the hub's forge with no CLI: it answers what it is set to, and records the asks.
type fakeForge struct {
	mu     sync.Mutex
	kind   string
	pr     forge.PR
	err    error
	remote string
	views  int
}

func (f *fakeForge) Kind() string { return f.kind }
func (f *fakeForge) View(_ context.Context, _ forge.Ref) (*forge.PR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.views++
	if f.err != nil {
		return nil, f.err
	}
	pr := f.pr
	return &pr, nil
}
func (f *fakeForge) Diff(context.Context, forge.Ref) ([]byte, error) {
	return []byte("diff --git a b"), f.err
}
func (f *fakeForge) Head(context.Context, forge.Ref) (string, error) { return f.pr.Head, f.err }
func (f *fakeForge) FetchSpec(ref forge.Ref) forge.FetchSpec {
	return forge.FetchSpec{Remote: f.remote, Refspec: "refs/pull/7/head"}
}
func (f *fakeForge) PRURL(forge.Ref) string { return "https://github.com/o/r/pull/7" }
func (f *fakeForge) Issue(context.Context, forge.Ref) (*forge.Issue, error) {
	return &forge.Issue{Title: "an issue", Body: "b"}, f.err
}
