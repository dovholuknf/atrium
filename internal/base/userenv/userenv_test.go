package userenv

import "testing"

func TestRefreshKeepsBaseWhenThereIsNothingToRead(t *testing.T) {
	base := []string{"A=1", "PATH=/x"}
	got := Refresh(base)
	if len(got) < len(base) {
		t.Fatalf("lost entries: %v", got)
	}
}
