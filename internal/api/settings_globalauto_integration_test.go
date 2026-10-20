//go:build integration

package api

import (
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestTheSettingsViewReportsAnUnwrittenBoardWideSwitchAsOn(t *testing.T) {
	t.Parallel()
	st, err := store.Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{st: st}

	if got := globalAutoView(s)["global_auto"]; got != true {
		t.Fatalf("an unwritten switch reads %v, want true", got)
	}
	if err := st.SetGlobalAuto(false); err != nil {
		t.Fatal(err)
	}
	if got := globalAutoView(s)["global_auto"]; got != false {
		t.Fatalf("an explicit off reads %v, want false", got)
	}
}
