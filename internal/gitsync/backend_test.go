package gitsync

import (
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/testguard"
)

// served is a bare repository holding claude/main and a hidden room ref, behind a Backend.
type served struct {
	srv          *httptest.Server
	bare         string
	mainSHA      string
	hiddenSHA    string
	visibleClaud string
}

// sharedFixture is the one repository every read-only test serves. Its fields are set once, under `once`.
type sharedFixture struct {
	once sync.Once
	srv  *served
	root string
}

var shared sharedFixture

func TestMain(m *testing.M) {
	// The git every test here runs ignores the operator's own git config. See internal/testguard.
	gitDir := testguard.Git("")
	code := m.Run()
	if gitDir != "" {
		_ = os.RemoveAll(gitDir)
	}
	if shared.srv != nil {
		shared.srv.srv.Close()
	}
	if shared.root != "" {
		_ = os.RemoveAll(shared.root)
	}
	os.Exit(code)
}

func (s *served) url() string { return s.srv.URL + "/github/o/r.git" }
