package api

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A directory that looks like a checkout to `isCheckout`, without being one.
// A `.git` directory is an ordinary checkout and a `.git` FILE is a worktree or
// a submodule. Both are working directories a card can start in, so both count.
func mkCheckout(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func mkDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func provider(t *testing.T, s *store.Store, root string, p store.Provider) *store.Provider {
	t.Helper()
	p.Name = orName(p.Name)
	p.Root = filepath.ToSlash(root)
	p.Enabled = true
	saved, err := s.SaveProvider(p)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func orName(n string) string {
	if n == "" {
		return "github"
	}
	return n
}

// The ordinary case, and the shape the whole feature is named for.
func TestDiscoverAdoptsOrgRepo(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "dovholuknf", "atrium"))
	mkCheckout(t, filepath.Join(root, "openziti", "ziti"))

	s := openStore(t)
	p := provider(t, s, root, store.Provider{})

	d := discoverProvider(s, p)
	if d.Adopted != 2 {
		t.Fatalf("adopted %d, expected 2. %s", d.Adopted, d.Summary)
	}
	repos, err := s.ProviderRepos(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected two rows, got %d", len(repos))
	}
	if repos[0].Org != "dovholuknf" || repos[0].Repo != "atrium" {
		t.Errorf("first row is %s/%s", repos[0].Org, repos[0].Repo)
	}
}

// A vendored or submodule checkout inside a repository is not a second row.
// The walk does not descend into a checkout, so it is never even reached.
func TestDiscoverStopsAtACheckout(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "org", "outer")
	mkCheckout(t, repo)
	mkCheckout(t, filepath.Join(repo, "vendor", "inner"))

	s := openStore(t)
	p := provider(t, s, root, store.Provider{})

	d := discoverProvider(s, p)
	if d.Adopted != 1 {
		t.Fatalf("adopted %d, expected 1. a vendored checkout became its own row", d.Adopted)
	}
}

// A repository sitting directly under the root, with no org above it, is a real
// arrangement and `root/org/repo` is a default rather than a law.
func TestDiscoverAdoptsARepoWithNoOrg(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "dotfiles"))

	s := openStore(t)
	p := provider(t, s, root, store.Provider{})

	if d := discoverProvider(s, p); d.Adopted != 1 {
		t.Fatalf("adopted %d, expected 1. %s", d.Adopted, d.Summary)
	}
	repos, err := s.ProviderRepos(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Org != "" || repos[0].Repo != "dotfiles" {
		t.Fatalf("got %q/%q", repos[0].Org, repos[0].Repo)
	}
	want := filepath.ToSlash(filepath.Join(root, "dotfiles"))
	if repos[0].Path != want {
		t.Fatalf("path is %q, expected %q", repos[0].Path, want)
	}
}

// Counted and reported, not adopted and not an error. Dropping them silently is
// how somebody spends an afternoon working out why one repository is missing.
func TestDiscoverSkipsAndCountsNonCheckouts(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "org", "real"))
	mkDir(t, filepath.Join(root, "org", "notarepo"))
	mkDir(t, filepath.Join(root, "org", "alsonot"))

	s := openStore(t)
	p := provider(t, s, root, store.Provider{})

	d := discoverProvider(s, p)
	if d.Adopted != 1 {
		t.Fatalf("adopted %d, expected 1", d.Adopted)
	}
	if d.Skipped != 2 {
		t.Fatalf("skipped %d, expected 2", d.Skipped)
	}
	if !strings.Contains(d.Summary, "not checkouts") {
		t.Errorf("the summary does not mention them: %q", d.Summary)
	}
	if d.Error != "" {
		t.Errorf("a directory that is not a checkout is not an error: %q", d.Error)
	}
}

// Running it twice adopts nothing the second time, which is what makes the
// button safe to press.
func TestDiscoverIsIdempotent(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "org", "repo"))

	s := openStore(t)
	p := provider(t, s, root, store.Provider{})

	if d := discoverProvider(s, p); d.Adopted != 1 {
		t.Fatalf("first run adopted %d", d.Adopted)
	}
	d := discoverProvider(s, p)
	if d.Adopted != 0 {
		t.Fatalf("second run adopted %d, expected 0", d.Adopted)
	}
	if d.AlreadyIn != 1 {
		t.Fatalf("second run should have recognised 1, said %d", d.AlreadyIn)
	}
}

// THE ONE THAT MATTERS MOST. A root on a drive that is not plugged in must
// leave every row alone, or one run erases everything the operator typed.
func TestDiscoverDoesNotDeleteRowsWhenTheRootIsGone(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "org", "repo"))

	s := openStore(t)
	p := provider(t, s, root, store.Provider{})
	discoverProvider(s, p)

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	d := discoverProvider(s, p)
	if d.Error == "" {
		t.Error("a root that cannot be read should say so")
	}
	repos, err := s.ProviderRepos(p.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("a run against a missing root destroyed rows, %d left", len(repos))
	}
	// And presence is derived, so the row tells the truth about right now
	// without having been rewritten.
	fillPresence(repos)
	if repos[0].Present {
		t.Error("the row claims a directory that is not there")
	}
}

// A glob keeps a subtree out, so a mirror of three hundred repositories cannot
// burn the adoption cap.
func TestDiscoverHonoursExclude(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "keep", "repo"))
	mkCheckout(t, filepath.Join(root, "archive", "old"))

	s := openStore(t)
	p := provider(t, s, root, store.Provider{Exclude: "archive"})

	d := discoverProvider(s, p)
	if d.Adopted != 1 {
		t.Fatalf("adopted %d, expected 1. %s", d.Adopted, d.Summary)
	}
	if d.Excluded != 1 {
		t.Fatalf("excluded %d, expected 1", d.Excluded)
	}
}

// Past the cap the walk stops and SAYS it stopped, rather than reporting a
// partial answer that looks complete.
func TestDiscoverBoundsRepos(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 12; i++ {
		mkCheckout(t, filepath.Join(root, "org", "repo"+string(rune('a'+i))))
	}
	s := openStore(t)
	p := provider(t, s, root, store.Provider{MaxRepos: 5})

	d := discoverProvider(s, p)
	if d.Adopted != 5 {
		t.Fatalf("adopted %d, expected exactly the cap of 5", d.Adopted)
	}
	if !d.Truncated {
		t.Error("a run that hit its cap has to say so")
	}
	if !strings.Contains(d.Summary, "stopped early") {
		t.Errorf("the summary hides the truncation: %q", d.Summary)
	}
}

// A root that is itself a checkout is a repository, not a root. Adopting it
// would make both org and repo meaningless.
func TestDiscoverRefusesARootThatIsACheckout(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, root)

	s := openStore(t)
	p := provider(t, s, root, store.Provider{})

	d := discoverProvider(s, p)
	if d.Adopted != 0 {
		t.Fatalf("adopted %d from a root that is a checkout", d.Adopted)
	}
	if !strings.Contains(d.Error, "above it") {
		t.Errorf("the refusal should name the way out, said %q", d.Error)
	}
}

// ── the refusal ─────────────────────────────────────────

// One directory in the worktree root blocks the toggle, which is the whole
// requirement.
func TestWorktreeToggleOffRefusesWhenNotEmpty(t *testing.T) {
	root := t.TempDir()
	mkDir(t, filepath.Join(root, "something"))

	blockers, err := worktreeRootBlockers(filepath.ToSlash(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 1 {
		t.Fatalf("expected one blocker, got %d", len(blockers))
	}
}

// The three names an operating system writes without being asked are ignored,
// and NOTHING ELSE IS. A dotfile rule would be broader and would let a `.git`
// under the worktree root pass, which is the half that would silently rot.
func TestWorktreeToggleOffIgnoresOnlyTheThreeNames(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".DS_Store", "Thumbs.db", "desktop.ini"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	blockers, err := worktreeRootBlockers(filepath.ToSlash(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 0 {
		t.Fatalf("operating system debris blocked the toggle: %v", blockers)
	}

	mkDir(t, filepath.Join(root, ".git"))
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blockers, err = worktreeRootBlockers(filepath.ToSlash(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 2 {
		t.Fatalf("a dotfile and a .git must both block, got %d: %v", len(blockers), blockers)
	}
}

// A directory that is not there holds nothing.
func TestWorktreeToggleOffAllowsAMissingDirectory(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "never-made")
	blockers, err := worktreeRootBlockers(filepath.ToSlash(gone))
	if err != nil {
		t.Fatalf("a missing directory is not an error: %v", err)
	}
	if len(blockers) != 0 {
		t.Fatalf("got %v", blockers)
	}
}

// UNREADABLE IS NOT EMPTY. Failing open here would fail on exactly the case
// where the directories are most likely to be there and least likely to be
// noticed.
func TestWorktreeToggleOffRefusesAnUnreadableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory cannot be made unreadable to its owner here")
	}
	root := filepath.Join(t.TempDir(), "locked")
	mkDir(t, root)
	mkDir(t, filepath.Join(root, "inside"))
	if err := os.Chmod(root, 0o000); err != nil {
		t.Skipf("could not make it unreadable: %v", err)
	}
	t.Cleanup(func() { os.Chmod(root, 0o755) })

	if _, err := worktreeRootBlockers(filepath.ToSlash(root)); err == nil {
		t.Fatal("an unreadable directory was treated as empty")
	}
}

// The refusal names every entry, because "not empty" makes somebody hunt.
func TestWorktreeToggleOffNamesWhatIsInTheWay(t *testing.T) {
	root := t.TempDir()
	mkCheckout(t, filepath.Join(root, "acheckout"))
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blockers, err := worktreeRootBlockers(filepath.ToSlash(root))
	if err != nil {
		t.Fatal(err)
	}
	msg := blockerMessage("github", filepath.ToSlash(root), blockers)
	for _, want := range []string{"acheckout", "notes.txt", "a file", "a checkout"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, "check button") {
		t.Error("the refusal should name the way out")
	}
}

// Past twenty it is a count, because an error listing four hundred directories
// is not a message.
func TestWorktreeToggleOffBoundsTheList(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 50; i++ {
		mkDir(t, filepath.Join(root, "d"+string(rune('a'+i%26))+string(rune('a'+i/26))))
	}
	blockers, err := worktreeRootBlockers(filepath.ToSlash(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(blockers) != 50 {
		t.Fatalf("expected 50 blockers, got %d", len(blockers))
	}
	msg := blockerMessage("github", filepath.ToSlash(root), blockers)
	lines := 0
	for _, l := range strings.Split(msg, "\n") {
		if strings.HasPrefix(l, "  d") {
			lines++
		}
	}
	if lines != blockersShown {
		t.Fatalf("listed %d entries, expected %d", lines, blockersShown)
	}
	if !strings.Contains(msg, "and 30 more") {
		t.Errorf("the remainder is not counted:\n%s", msg)
	}
}

// Two providers that describe the same directories mean one directory adopted
// twice, and a refusal that reads a directory somebody else still owns.
func TestOverlappingRootsAreRefused(t *testing.T) {
	base := t.TempDir()
	outer := filepath.Join(base, "git")
	inner := filepath.Join(outer, "github")
	mkDir(t, inner)

	all := []*store.Provider{{Name: "everything", Root: filepath.ToSlash(outer)}}
	err := providerOverlap(all, store.Provider{Name: "github", Root: filepath.ToSlash(inner)})
	if err == nil {
		t.Fatal("a root inside another provider's root was accepted")
	}
	if !strings.Contains(err.Error(), "everything") || !strings.Contains(err.Error(), "github") {
		t.Errorf("the refusal should name both providers, said %q", err)
	}

	// Two roots side by side are fine, which is the ordinary arrangement.
	sibling := filepath.Join(base, "bitbucket")
	mkDir(t, sibling)
	if err := providerOverlap(all, store.Provider{
		Name: "bitbucket", Root: filepath.ToSlash(sibling),
	}); err != nil {
		t.Fatalf("two separate roots were refused: %v", err)
	}
}

// A worktree root inside another provider's root is the same failure by a
// different door, and it is the one somebody actually arranges by accident.
func TestAWorktreeRootInsideAnotherRootIsRefused(t *testing.T) {
	base := t.TempDir()
	git := filepath.Join(base, "git")
	mkDir(t, git)

	all := []*store.Provider{{Name: "github", Root: filepath.ToSlash(git)}}
	err := providerOverlap(all, store.Provider{
		Name: "second", Root: filepath.ToSlash(filepath.Join(base, "other")),
		Worktrees: true, WorktreeRoot: filepath.ToSlash(filepath.Join(git, "worktrees")),
	})
	if err == nil {
		t.Fatal("a worktree root inside another provider's root was accepted")
	}
}
