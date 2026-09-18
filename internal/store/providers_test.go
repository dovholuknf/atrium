package store

import (
	"path/filepath"
	"testing"
)

// The provider tables have to appear on a database that already exists, which
// is every database on every machine that has ever run atrium. CLAUDE.md says
// this was learned the hard way when `perm_rule` was added to `0001` and every
// existing database skipped it.
func TestProviderMigrationRunsOnAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Put the database back the way it looked before this feature: the tables
	// gone, and every migration but this one still recorded, which is exactly
	// the state a naive runner would call current.
	for _, stmt := range []string{
		`DROP TABLE provider_repo`,
		`DROP TABLE provider`,
		`DELETE FROM schema_migration WHERE name = '0053_provider'`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopening a pre-provider database failed: %v", err)
	}
	defer reopened.Close()

	if _, err := reopened.SaveProvider(Provider{
		Name: "github", Root: "d:/git/github", Enabled: true,
	}); err != nil {
		t.Fatalf("the provider tables were not created on reopen: %v", err)
	}
}

// Two spellings of one name are one provider, because they are one directory
// on the filesystem this runs on.
func TestProviderNamesAreCaseFolded(t *testing.T) {
	s := open(t)
	if _, err := s.SaveProvider(Provider{Name: "github", Root: "d:/git/github", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProvider(Provider{Name: "GitHub", Root: "d:/git/gh", Enabled: true}); err != nil {
		t.Fatalf("saving a differently capitalised name should update, not collide: %v", err)
	}
	all, err := s.Providers()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected one provider, got %d", len(all))
	}
	// The FIRST spelling wins, because it is the string everything else already
	// refers to. Changing it under them is the rename this feature does not do.
	if all[0].Name != "github" {
		t.Errorf("name should stay as first typed, got %q", all[0].Name)
	}
	if all[0].Root != "d:/git/gh" {
		t.Errorf("the save should have updated the root, got %q", all[0].Root)
	}
}

// Same rule one level down, and for the same reason: `Dovholuknf/Atrium` and
// `dovholuknf/atrium` are one directory on Windows.
func TestProviderRepoKeyIsCaseFolded(t *testing.T) {
	s := open(t)
	mustProvider(t, s, "github", "d:/git/github")

	if _, err := s.SaveProviderRepo(ProviderRepo{
		Provider: "github", Org: "dovholuknf", Repo: "atrium",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProviderRepo(ProviderRepo{
		Provider: "github", Org: "Dovholuknf", Repo: "Atrium",
	}); err != nil {
		t.Fatal(err)
	}
	repos, err := s.ProviderRepos("github")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected one repository row, got %d", len(repos))
	}
}

// Deleting a provider takes its repository rows and nothing else. A card that
// happens to sit in one of those directories is not configuration and must not
// be touched.
func TestDeleteProviderCascadesToReposAndLeavesCardsAlone(t *testing.T) {
	s := open(t)
	mustProvider(t, s, "github", "d:/git/github")
	if _, err := s.SaveProviderRepo(ProviderRepo{
		Provider: "github", Org: "dovholuknf", Repo: "atrium",
	}); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.Register(Observed{
		WireName: "card", Worktree: "d:/git/github/dovholuknf/atrium", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteProvider("GitHub"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM provider_repo`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("repository rows survived the provider, %d left", n)
	}
	back, err := s.Get(task.ID)
	if err != nil {
		t.Fatalf("the card went with the provider, which is the thing that must never happen: %v", err)
	}
	if back.Worktree != "d:/git/github/dovholuknf/atrium" {
		t.Errorf("the card's directory was rewritten to %q", back.Worktree)
	}
}

// Running discovery twice adopts nothing the second time, and never
// resurrects something deliberately dismissed.
func TestAdoptIsIdempotentAndRespectsHidden(t *testing.T) {
	s := open(t)
	mustProvider(t, s, "github", "d:/git/github")

	found := []ProviderRepo{
		{Org: "dovholuknf", Repo: "atrium"},
		{Org: "openziti", Repo: "ziti"},
	}
	added, err := s.AdoptProviderRepos("github", found)
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 {
		t.Fatalf("first run adopted %d, expected 2", added)
	}
	added, err = s.AdoptProviderRepos("github", found)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Fatalf("second run adopted %d, expected 0", added)
	}

	if err := s.HideProviderRepo("github", "openziti", "ziti", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdoptProviderRepos("github", found); err != nil {
		t.Fatal(err)
	}
	repos, err := s.ProviderRepos("github")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range repos {
		if r.Repo == "ziti" && !r.Hidden {
			t.Fatal("discovery un-hid a repository, which makes hidden a suggestion rather than an answer")
		}
	}
}

// The path is derived from the root, so moving the root moves every row with
// it and nothing has to be rewritten.
func TestRepoPathFollowsTheRoot(t *testing.T) {
	s := open(t)
	mustProvider(t, s, "github", "d:/git/github")
	if _, err := s.SaveProviderRepo(ProviderRepo{
		Provider: "github", Org: "dovholuknf", Repo: "atrium",
	}); err != nil {
		t.Fatal(err)
	}
	repos, err := s.ProviderRepos("github")
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Path != "d:/git/github/dovholuknf/atrium" {
		t.Fatalf("path was %q", repos[0].Path)
	}

	if _, err := s.SaveProvider(Provider{
		Name: "github", Root: "e:/code", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	repos, err = s.ProviderRepos("github")
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Path != "e:/code/dovholuknf/atrium" {
		t.Fatalf("path did not follow the root, got %q", repos[0].Path)
	}
}

// A repository directly under the root, with no org above it, is a real
// arrangement. `root/org/repo` is a default, not a law.
func TestARepoWithNoOrgGetsATwoPartPath(t *testing.T) {
	if got := ProviderPath("d:/git/github", "", "dotfiles"); got != "d:/git/github/dotfiles" {
		t.Fatalf("got %q", got)
	}
	if got := ProviderPath("d:\\git\\github\\", "org", "repo"); got != "d:/git/github/org/repo" {
		t.Fatalf("backslashes and a trailing slash should normalise, got %q", got)
	}
}

// A save carries no observations. Only `RecordProviderScan` writes those, or a
// form post could claim a scan that never ran.
func TestSaveDoesNotWriteTheObservedFields(t *testing.T) {
	s := open(t)
	mustProvider(t, s, "github", "d:/git/github")
	if err := s.RecordProviderScan("github", "adopted 3", "could not read that root"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProvider(Provider{
		Name: "github", Root: "d:/git/github", Enabled: true,
		LastScan: "adopted 900", LastError: "",
	}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Provider("github")
	if err != nil {
		t.Fatal(err)
	}
	if p.LastScan != "adopted 3" {
		t.Errorf("a save overwrote what was observed, last_scan is %q", p.LastScan)
	}
	if p.LastError != "could not read that root" {
		t.Errorf("a save cleared an observed error, last_error is %q", p.LastError)
	}
	if p.LastScanAt == nil {
		t.Error("the scan time went missing")
	}
}

// Worktree support with nowhere to put them is refused at save, rather than
// when somebody presses make and it is far too late to say so.
func TestWorktreesOnNeedsARoot(t *testing.T) {
	s := open(t)
	if _, err := s.SaveProvider(Provider{
		Name: "github", Root: "d:/git/github", Enabled: true, Worktrees: true,
	}); err == nil {
		t.Fatal("expected a refusal")
	}
}

func TestAProviderNeedsANameAndARoot(t *testing.T) {
	s := open(t)
	if _, err := s.SaveProvider(Provider{Root: "d:/git"}); err == nil {
		t.Error("a nameless provider was accepted")
	}
	if _, err := s.SaveProvider(Provider{Name: "github"}); err == nil {
		t.Error("a rootless provider was accepted")
	}
	if _, err := s.SaveProvider(Provider{Name: "git/hub", Root: "d:/git"}); err == nil {
		t.Error("a name with a slash in it was accepted")
	}
	if _, err := s.SaveProvider(Provider{Name: "github", Root: "d:/git", Kind: "hg"}); err == nil {
		t.Error("an unknown provider type was accepted")
	}
}

// Orgs are derived, so an org stops existing the moment its last repository
// row is forgotten and nothing has to tidy up after it.
func TestOrgsAreDerivedFromRows(t *testing.T) {
	s := open(t)
	mustProvider(t, s, "github", "d:/git/github")
	for _, r := range []ProviderRepo{
		{Provider: "github", Org: "dovholuknf", Repo: "atrium"},
		{Provider: "github", Org: "openziti", Repo: "ziti"},
		{Provider: "github", Org: "", Repo: "dotfiles"},
	} {
		if _, err := s.SaveProviderRepo(r); err != nil {
			t.Fatal(err)
		}
	}
	orgs, err := s.ProviderOrgs("github")
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 2 {
		t.Fatalf("expected two orgs, got %v", orgs)
	}

	if err := s.ForgetProviderRepo("github", "openziti", "ziti"); err != nil {
		t.Fatal(err)
	}
	orgs, err = s.ProviderOrgs("github")
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 1 || orgs[0] != "dovholuknf" {
		t.Fatalf("expected the org to go with its last repository, got %v", orgs)
	}
}

func mustProvider(t *testing.T, s *Store, name, root string) {
	t.Helper()
	if _, err := s.SaveProvider(Provider{Name: name, Root: root, Enabled: true}); err != nil {
		t.Fatal(err)
	}
}
