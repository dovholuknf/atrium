package gitsync

import (
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/testguard"
)

// served is a bare repository holding claude/main and a hidden room ref, behind a Backend.
type served struct {
	srv          *httptest.Server
	bare         string
	mainSHA      string
	hiddenSHA    string
	visibleClaud string
}

var (
	sharedOnce sync.Once
	shared     *served
	sharedRoot string
)

func TestMain(m *testing.M) {
	// The git every test here runs ignores the operator's own git config. See internal/testguard.
	gitDir := testguard.Git("")
	code := m.Run()
	if gitDir != "" {
		_ = os.RemoveAll(gitDir)
	}
	if shared != nil {
		shared.srv.Close()
	}
	if sharedRoot != "" {
		_ = os.RemoveAll(sharedRoot)
	}
	os.Exit(code)
}

func (s *served) url() string { return s.srv.URL + "/github/o/r.git" }
