//go:build integration

package hubstore

import (
	"strings"
	"testing"
)

func TestGitReposAreEmptyUntilSetAndRoundTrip(t *testing.T) {
	s := open(t)
	if got, err := s.GitRepos(); err != nil || got != nil {
		t.Fatalf("unset = %v %v", got, err)
	}
	dir := t.TempDir()
	if err := s.SetGitRepos(`[{"name":"github/o/r","checkout":"` + strings.ReplaceAll(dir, `\`, "/") + `"}]`); err != nil {
		t.Fatal(err)
	}
	got, err := s.GitRepos()
	if err != nil || len(got) != 1 || got[0].Branch != "claude/main" {
		t.Fatalf("round trip = %+v %v", got, err)
	}
}

// A department branch is refused when it is typed, and what is stored is not changed.
func TestADepartmentBranchIsRefusedWhenWritten(t *testing.T) {
	s := open(t)
	dir := strings.ReplaceAll(t.TempDir(), `\`, "/")
	if err := s.SetGitRepos(`[{"name":"github/o/r","checkout":"` + dir + `","branch":"main"}]`); err != nil {
		t.Fatal(err)
	}
	err := s.SetGitRepos(`[{"name":"github/o/r","checkout":"` + dir + `","branch":"claude/ui"}]`)
	if err == nil || !strings.Contains(err.Error(), "integration branch") {
		t.Fatalf("err = %v", err)
	}
	got, _ := s.GitRepos()
	if len(got) != 1 || got[0].Branch != "main" {
		t.Fatalf("a refused write changed what is stored: %+v", got)
	}
}

// And again when read, in case something else wrote the row.
func TestADepartmentBranchWrittenBehindOurBackIsRefusedWhenRead(t *testing.T) {
	s := open(t)
	dir := strings.ReplaceAll(t.TempDir(), `\`, "/")
	if err := s.SetHubSetting(SettingGitRepos, `[{"name":"github/o/r","checkout":"`+dir+`","branch":"claude/ui"}]`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GitRepos(); err == nil || !strings.Contains(err.Error(), "integration branch") {
		t.Fatalf("err = %v", err)
	}
}
