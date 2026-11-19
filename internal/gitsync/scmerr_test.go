package gitsync

import (
	"strings"
	"testing"
)

func TestNoSCMRootNamesARealCommand(t *testing.T) {
	if !strings.Contains(ErrNoSCMRoot.Error(), "atrium room set scm_root") {
		t.Fatalf("%q", ErrNoSCMRoot)
	}
}
