package api

import (
	"os"
	"testing"

	"github.com/dovholuknf/atrium/internal/testguard"
)

// upstreamTemplateDir is the one upstream repo the package builds, set by upstream() and removed here.
var upstreamTemplateDir string

// TestMain keeps every test here off a live atrium: no ATRIUM_* pointer from the
// shell this ran in reaches a test. See internal/testguard.
func TestMain(m *testing.M) {
	dir := testguard.Scrub()
	code := m.Run()
	if upstreamTemplateDir != "" {
		_ = os.RemoveAll(upstreamTemplateDir)
	}
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
