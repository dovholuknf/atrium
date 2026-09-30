package api

import (
	"os"
	"testing"

	"github.com/dovholuknf/atrium/internal/testguard"
)

// TestMain keeps every test here off a live atrium: no ATRIUM_* pointer from the
// shell this ran in reaches a test. See internal/testguard.
func TestMain(m *testing.M) {
	dir := testguard.Scrub()
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
