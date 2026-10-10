package claudeconf

import (
	"path/filepath"
	"testing"
)

// Which binary goes into settings.json.
//
// Each test is named for the way the answer gets to be wrong, because every one
// of these produced a board reporting six working hooks as `points elsewhere`.

// The location path is duplicated from `internal/daemon/whereami.go` to avoid
// an import cycle. If the two ever disagree, hooks name a binary that is not
// the daemon's, which is the exact defect this file exists to prevent. The
// daemon side asserts the other half in `whereami_test.go`.
func TestDefaultLocationPathIsNotEmpty(t *testing.T) {
	p, err := defaultLocationPath()
	if err != nil {
		t.Fatal(err)
	}
	if p == "" || filepath.Base(p) != "daemon.json" {
		t.Fatalf("location path is %q, want one ending in daemon.json", p)
	}
}
