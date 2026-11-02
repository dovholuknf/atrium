package shared

import (
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
)

// Runner is what a consumer of a supervised terminal may ask of it. The consumers hold this and never the struct.
type Runner interface {
	TaskID() string
	Started() time.Time
	ReattachedAt() time.Time
	Done() <-chan struct{}
	HasTerm() bool
	Write(p []byte) error
	WriteOperatorInput(p []byte) error
	InjectPeer(banner, body string) (bool, error)
	InjectPeerIf(banner, body string, ok func() bool) (bool, error)
	InjectForced(banner, body string, ok func() bool) (bool, error)
	ForceRefusal() string
	PeerForceOK() bool
	PeerGateOpen() bool
	TypedWithin(d time.Duration) bool
	ResetLine() bool
	ResetLineAtStop() bool
	PromptWasOwn(at time.Time) bool
	PromptWasPeer(at time.Time) bool
	PeerPromptCause(at time.Time) string
	NoteCause(kind, by string)
	Leaving() bool
	SetLeaving(on bool)
	ClearReattached()
	SetOnKey(f func())
	ReplayCuts() (backlog []byte, cuts []screen.SizeCut, rows int, wrapped bool)
	LastOutputAt() time.Time
	LooksIdleFrame() bool
	WhileGateOpen(f func()) bool
	TypeOperatorBytes(bs []byte, ok func() bool) bool
}

// Registry is how a concern finds the runners atrium owns, keyed by card. A card with no runner answers a nil Runner.
type Registry interface {
	Get(taskID string) Runner
	Has(taskID string) bool
	All() []Runner
}
