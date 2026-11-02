package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/shared"
)

// What the cull and the card homes share, hoisted to shared so the home package needs no Daemon.
const (
	DefaultCullInto = shared.DefaultCullInto
	KeepOpenTag     = shared.KeepOpenTag
	cullGitTimeout  = shared.CullGitTimeout
	preflightOutput = shared.PreflightOutput
)

type CullResult = shared.CullResult

func gitIn(dir string, args ...string) (string, error) { return shared.GitIn(dir, args...) }
func hasDotGit(dir string) bool                        { return shared.HasDotGit(dir) }
func samePath(a, b string) bool                        { return shared.SamePath(a, b) }
func plural(n int, noun string) string                 { return shared.Plural(n, noun) }
func orDefault(s, def string) string                   { return shared.OrDefault(s, def) }
func protectedBranch(branch, into string) bool         { return shared.ProtectedBranch(branch, into) }
func processAlive(pid int) bool                        { return shared.ProcessAlive(pid) }
