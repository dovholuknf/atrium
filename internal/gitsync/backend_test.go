package gitsync

import (
	"net/http/httptest"
	"os"
	"sync"
	"testing"
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
	code := m.Run()
	if shared != nil {
		shared.srv.Close()
	}
	if sharedRoot != "" {
		_ = os.RemoveAll(sharedRoot)
	}
	os.Exit(code)
}

func (s *served) url() string { return s.srv.URL + "/github/o/r.git" }
