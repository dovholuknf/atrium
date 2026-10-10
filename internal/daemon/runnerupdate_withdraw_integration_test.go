//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

const codexPkg = "@openai/codex"

// fakeRunner installs a runner whose version is read off disk, the way the real
// check reads it, and puts it on PATH. Nothing is ever executed.
func fakeRunner(t *testing.T, id, pkg, version string) (*store.Harness, func(string)) {
	t.Helper()
	root := t.TempDir()
	cmd := "fake" + id
	file := cmd
	if runtime.GOOS == "windows" {
		file += ".cmd"
	}
	if err := os.WriteFile(filepath.Join(root, file), []byte("exit 0"), 0o700); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(root, "node_modules", filepath.FromSlash(pkg))
	if runtime.GOOS != "windows" {
		pkgDir = filepath.Join(root, "lib", "node_modules", filepath.FromSlash(pkg))
		if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(root, file), filepath.Join(root, "bin", file)); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	} else {
		t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	writePackageJSON(t, pkgDir, version)
	h := &store.Harness{ID: id, Label: id, Cmd: cmd, Package: pkg}
	return h, func(v string) { writePackageJSON(t, pkgDir, v) }
}

// updateCards is every card the runner-update source ever raised for a package,
// archived or not, keyed by id.
func updateCards(t *testing.T, d *Daemon, pkg string) map[string]*store.Task {
	t.Helper()
	hist, err := d.st.ListArchived(1000)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*store.Task{}
	live, _ := d.st.List()
	for _, c := range append(live, hist...) {
		if c.Source == "runner-update" {
			out[c.ID] = c
		}
	}
	return out
}

func TestOfferedVersionRoundTripsThroughTheURL(t *testing.T) {
	for _, pkg := range []string{codexPkg, "left-pad"} {
		u := runnerUpdateURL(pkg, "1.2.3")
		if got := offeredVersion(pkg, u); got != "1.2.3" {
			t.Errorf("%s: %q read back as %q", pkg, u, got)
		}
	}
	for _, bad := range []string{
		"", "https://github.com/openai/codex", "https://www.npmjs.com/package/@openai/codex",
		"https://www.npmjs.com/package/@openai/codex/v/", "https://www.npmjs.com/package/other/v/1.0.0",
		"https://www.npmjs.com/package/@openai/codex/v/1.0.0/extra",
	} {
		if got := offeredVersion(codexPkg, bad); got != "" {
			t.Errorf("%q read as version %q", bad, got)
		}
	}
}

func TestARefreshedCardReadsBackTheNewerVersion(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.154.0")
	d.offerRunnerUpdate(h, "0.154.0", "0.156.1")
	d.offerRunnerUpdate(h, "0.154.0", "0.158.0")
	cards := updateCards(t, d, codexPkg)
	if len(cards) != 1 {
		t.Fatalf("%d cards, want 1", len(cards))
	}
	for _, c := range cards {
		if got := offeredVersion(codexPkg, c.URL); got != "0.158.0" {
			t.Fatalf("card offers %q", got)
		}
	}
}

func TestASatisfiedUpdateCardIsWithdrawn(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.154.0")
	d.offerRunnerUpdate(h, "0.154.0", "0.156.1")
	legacy, _, err := d.st.Offer(store.IntakeItem{
		Source: "runner-update", ExternalID: codexPkg + "@0.150.0", Title: "codex: 0.149.0 to 0.150.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	started, _, _ := d.st.Offer(store.IntakeItem{Source: "runner-update", ExternalID: "@other/pkg"})
	if err := d.st.SetStatus(started.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}

	// codex updated itself, and the registry agrees.
	h, setVersion := fakeRunner(t, "codex", codexPkg, "0.156.1")
	_ = setVersion
	d.rememberLatest(codexPkg, "0.156.1")
	d.checkRunnerUpdate(h, false)

	if inbox, _ := d.st.Offered(); len(inbox) != 0 {
		t.Fatalf("%d cards still in the inbox", len(inbox))
	}
	got, _ := d.st.Get(legacy.ID)
	if got.ArchivedAt == nil {
		t.Fatal("the legacy card was not withdrawn")
	}
	if s, _ := d.st.Get(started.ID); s.ArchivedAt != nil {
		t.Fatal("an unrelated started card was archived")
	}
}

func TestAStartedCardAtAnOlderVersionIsReleasedAndReoffered(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.156.1")
	d.offerRunnerUpdate(h, "0.154.0", "0.156.1")
	old := d.updateCardsFor(codexPkg)[0]
	if err := d.st.SetStatus(old.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}

	d.rememberLatest(codexPkg, "0.158.0")
	d.checkRunnerUpdate(h, false)

	fresh := d.updateCardsFor(codexPkg)
	if len(fresh) != 1 || fresh[0].ID == old.ID {
		t.Fatalf("no fresh card offered: %v", fresh)
	}
	if fresh[0].Title != "codex: 0.156.1 to 0.158.0" {
		t.Fatalf("title %q", fresh[0].Title)
	}
	if got, _ := d.st.Get(old.ID); got.Status != store.StatusRunning || got.Title != old.Title {
		t.Fatalf("the started card was touched: %+v", got)
	}
	// Once only: a second check finds the fresh card in the inbox.
	d.checkRunnerUpdate(h, false)
	if n := len(updateCards(t, d, codexPkg)); n != 2 {
		t.Fatalf("%d cards after a second check, want 2", n)
	}
}

func TestAStartedCardAtTheCurrentVersionIsNotReleased(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.156.1")
	d.offerRunnerUpdate(h, "0.156.1", "0.158.0")
	old := d.updateCardsFor(codexPkg)[0]
	if err := d.st.SetStatus(old.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	d.rememberLatest(codexPkg, "0.158.0")
	d.checkRunnerUpdate(h, false)
	if n := len(updateCards(t, d, codexPkg)); n != 1 {
		t.Fatalf("%d cards, want 1: somebody is already on this release", n)
	}
	if got, _ := d.st.ByIntakeKey(store.IntakeKey("runner-update", codexPkg)); got == nil || got.ID != old.ID {
		t.Fatal("the started card lost its key")
	}
}

// A started card with no URL predates the contract, counts as older, and is
// released exactly once.
func TestAStartedCardWithNoURLIsReleasedOnce(t *testing.T) {
	d := testDaemon(t)
	h, _ := fakeRunner(t, "codex", codexPkg, "0.156.1")
	old, _, _ := d.st.Offer(store.IntakeItem{Source: "runner-update", ExternalID: codexPkg, Title: "codex: 0.154.0 to 0.156.1"})
	if err := d.st.SetStatus(old.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	d.rememberLatest(codexPkg, "0.158.0")
	d.checkRunnerUpdate(h, false)
	d.checkRunnerUpdate(h, false)
	if n := len(updateCards(t, d, codexPkg)); n != 2 {
		t.Fatalf("%d cards, want 2", n)
	}
}

// The stale codex card in the live db: backlog, no URL, keyed by the package,
// reading 0.154.0 to 0.156.1.
func TestABacklogCardWithNoURL(t *testing.T) {
	seed := func(t *testing.T) (*Daemon, *store.Harness, *store.Task) {
		d := testDaemon(t)
		h, _ := fakeRunner(t, "codex", codexPkg, "0.156.1")
		c, _, err := d.st.Offer(store.IntakeItem{
			Source: "runner-update", ExternalID: codexPkg, Title: "codex: 0.154.0 to 0.156.1",
		})
		if err != nil {
			t.Fatal(err)
		}
		return d, h, c
	}
	t.Run("registry agrees with the install: withdrawn", func(t *testing.T) {
		d, h, c := seed(t)
		d.rememberLatest(codexPkg, "0.156.1")
		d.checkRunnerUpdate(h, false)
		if got, _ := d.st.Get(c.ID); got.ArchivedAt == nil {
			t.Fatal("not archived")
		}
	})
	t.Run("a newer release: rewritten, not left saying 0.156.1", func(t *testing.T) {
		d, h, c := seed(t)
		d.rememberLatest(codexPkg, "0.158.0")
		d.checkRunnerUpdate(h, false)
		got, _ := d.st.Get(c.ID)
		if got.ArchivedAt != nil || got.Title != "codex: 0.156.1 to 0.158.0" ||
			offeredVersion(codexPkg, got.URL) != "0.158.0" {
			t.Fatalf("card is %q url %q archived=%v", got.Title, got.URL, got.ArchivedAt)
		}
		if n := len(updateCards(t, d, codexPkg)); n != 1 {
			t.Fatalf("%d cards, want 1", n)
		}
	})
}
