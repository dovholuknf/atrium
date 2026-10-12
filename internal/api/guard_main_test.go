package api

import (
	"os"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/testguard"
)

// TestMain keeps every test here off a live atrium: no ATRIUM_* pointer from the
// shell this ran in reaches a test. See internal/testguard.
func TestMain(m *testing.M) {
	dir := testguard.Scrub()
	code := m.Run()
	if upstreamRepo.dir != "" {
		_ = os.RemoveAll(upstreamRepo.dir)
	}
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

// upstreamRepo is the one upstream repo the package builds, on the first ask. Its folder is removed by TestMain.
var upstreamRepo upstreamFixture

type upstreamFixture struct {
	once sync.Once
	dir  string
	err  error
}
