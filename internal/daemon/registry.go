package daemon

import (
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
)

// Runner is shared.Runner, which the features that left the package hold.
type Runner = shared.Runner

// Registry is shared.Registry.
type Registry = shared.Registry

var _ Registry = (*supervisor)(nil)
var _ Runner = (*runner)(nil)

func (s *supervisor) Get(taskID string) Runner {
	if r := s.get(taskID); r != nil {
		return r
	}
	return nil
}

func (s *supervisor) Has(taskID string) bool { return s.has(taskID) }

func (s *supervisor) All() []Runner {
	rs := s.all()
	out := make([]Runner, len(rs))
	for i, r := range rs {
		out[i] = r
	}
	return out
}

func (r *runner) TaskID() string                    { return r.taskID }
func (r *runner) Done() <-chan struct{}             { return r.done }
func (r *runner) HasTerm() bool                     { return r.tm() != nil }
func (r *runner) WriteOperatorInput(p []byte) error { return r.writeOperatorInput(p) }
func (r *runner) InjectPeer(banner, body string) (bool, error) {
	return r.injectPeer(banner, body)
}
func (r *runner) InjectForced(banner, body string, ok func() bool) (bool, error) {
	return r.injectForced(banner, body, ok)
}
func (r *runner) ForceRefusal() string                { return r.forceRefusal() }
func (r *runner) PeerForceOK() bool                   { return r.peerForceOK() }
func (r *runner) PeerGateOpen() bool                  { return r.peerGateOpen() }
func (r *runner) TypedWithin(d time.Duration) bool    { return r.typedWithin(d) }
func (r *runner) ResetLine() bool                     { return r.resetLine() }
func (r *runner) ResetLineAtStop() bool               { return r.resetLineAtStop() }
func (r *runner) PromptWasOwn(at time.Time) bool      { return r.promptWasOwn(at) }
func (r *runner) PromptWasPeer(at time.Time) bool     { return r.promptWasPeer(at) }
func (r *runner) PeerPromptCause(at time.Time) string { return r.peerPromptCause(at) }
func (r *runner) NoteCause(kind, by string)           { r.noteCause(kind, by) }
func (r *runner) Leaving() bool                       { return r.leaving.Load() }
func (r *runner) SetLeaving(on bool)                  { r.leaving.Store(on) }
func (r *runner) ClearReattached()                    { r.reattached.Store(false) }

func (r *runner) SetOnKey(f func()) {
	if f == nil {
		r.onKey.Store(nil)
		return
	}
	r.onKey.Store(&f)
}

func (r *runner) ReplayCuts() ([]byte, []screen.SizeCut, int, bool) { return r.buf.ReplayCuts() }

func (r *runner) Started() time.Time      { return r.started }
func (r *runner) ReattachedAt() time.Time { return r.reattachedAt }
func (r *runner) InjectPeerIf(banner, body string, ok func() bool) (bool, error) {
	return r.injectPeerIf(banner, body, ok)
}

func (r *runner) LastOutputAt() time.Time { return r.lastOutputAt() }

// LooksIdleFrame says whether the terminal's last frame is a settled prompt.
func (r *runner) LooksIdleFrame() bool {
	if r.buf == nil {
		return false
	}
	cols, rows := r.buf.CurrentSize()
	idle, _ := screen.ClassifyFrame(r.buf.Tail(screen.FrameTailBytes), cols, rows)
	return idle
}

// WhileGateOpen runs f under the input lock when the operator's line gate is open, and says whether it ran.
func (r *runner) WhileGateOpen(f func()) bool {
	r.pasteMu.Lock()
	defer r.pasteMu.Unlock()
	if !r.peerGateOpen() {
		return false
	}
	f()
	return true
}

// TypeOperatorBytes writes bs as operator input under the input lock, unless ok says no.
func (r *runner) TypeOperatorBytes(bs []byte, ok func() bool) bool {
	r.pasteMu.Lock()
	defer r.pasteMu.Unlock()
	if !ok() {
		return false
	}
	r.noteOperatorTyped(bs)
	return r.Write(bs) == nil
}
