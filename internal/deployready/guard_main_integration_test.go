//go:build integration && git

package deployready

import (
	"os"
	"testing"

	"github.com/dovholuknf/atrium/internal/testguard"
)

// TestMain keeps the git every test here runs off the operator's own git config. See internal/testguard.
func TestMain(m *testing.M) {
	dir := testguard.Git("")
	code := m.Run()
	plainBase.cleanup()
	mergeBase.cleanup()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
