//go:build integration

package hubstore

import (
	"os"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/testguard"
)

// TestMain keeps the git every test here runs off the operator's own git config. See internal/testguard.
func TestMain(m *testing.M) {
	dir := testguard.Git("")
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
