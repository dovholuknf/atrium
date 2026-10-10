package daemon

import (
	"strings"
	"testing"
)

// The cut note's reason carries no counts, since the sheet words those itself.
func TestCutWhyHoldsNoCounts(t *testing.T) {
	files := make([]ChangeFile, changesFilesMax+5)
	for i := range files {
		files[i].Hunks = "x"
	}
	_, cut := bound(files)
	if cut == nil || cut.Files != 5 || strings.Contains(cut.Why, "5 files") || strings.Contains(cut.Why, "counts only") {
		t.Fatalf("%+v", cut)
	}
}
