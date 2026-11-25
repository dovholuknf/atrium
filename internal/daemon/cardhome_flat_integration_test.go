//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestAWorktreeHomeIsTheWorkingDirectoryUnderForgeOrgRepo(t *testing.T) {
	t.Parallel()
	d, root := homeRoom(t)
	repo := homeRepo(t)
	cullGit(t, repo, "remote", "add", "origin", "https://github.com/acme/widgets.git")
	task, row, dd := worktreeHome(t, d, repo)

	want := filepath.Join(root, "github", "acme", "widgets", "fix-the-thing")
	if filepath.ToSlash(dd.Dir) != filepath.ToSlash(want) || dd.Work != dd.Dir || row.Layout != store.CardHomeFlat {
		t.Fatalf("home %s work %s layout %q, want %s", dd.Dir, dd.Work, row.Layout, want)
	}
	if _, err := os.Stat(filepath.Join(dd.Dir, ".git")); err != nil {
		t.Fatalf("the home is not a worktree: %v", err)
	}
	for _, p := range []string{dd.Tmp, dd.Meta, dd.Brief} {
		if rel, err := filepath.Rel(dd.Dir, p); err != nil || !strings.HasPrefix(rel, "..") {
			t.Fatalf("%s is inside the repo tree", p)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s is missing: %v", p, err)
		}
	}
	if removed, kept, _ := d.ReclaimHome(task, row, false); !removed {
		t.Fatalf("kept: %q", kept)
	}
	for _, p := range []string{dd.Dir, filepath.Dir(dd.Tmp), filepath.Join(root, "github")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s is still there", p)
		}
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the card root went: %v", err)
	}
}

func TestAScratchHomeIsUnderScratchWithItsFilesAside(t *testing.T) {
	t.Parallel()
	d, root := homeRoom(t)
	row, dd, err := d.MakeFlatHome(store.CardHomeScratch, "Notes", "w")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dd.Dir) != filepath.Join(root, "scratch") || !strings.HasPrefix(filepath.Base(dd.Dir), "notes-") || dd.Work != dd.Dir {
		t.Fatalf("home at %s", dd.Dir)
	}
	if got := d.RowDirs(row); got != dd {
		t.Fatalf("RowDirs %+v, made %+v", got, dd)
	}
	if err := os.WriteFile(filepath.Join(dd.Brief, "BRIEF.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(dd.Dir); len(ents) != 0 {
		t.Fatalf("the brief landed in the working directory: %v", ents)
	}
	if removed, kept, _ := d.ReclaimHome(nil, row, true); !removed {
		t.Fatalf("kept: %q", kept)
	}
	if _, err := os.Stat(filepath.Dir(dd.Meta)); !os.IsNotExist(err) {
		t.Fatal("the card's state folder is still there")
	}
}

func TestAHomeOfTheFirstLayoutKeepsItsPlaces(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.MakeHome(store.CardHomeScratch, "old", "w")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.RowDirs(row); got != dd || got.Work != filepath.Join(got.Dir, "work") || got.Brief != got.Dir {
		t.Fatalf("RowDirs %+v, made %+v", got, dd)
	}
}

func TestFlatHomeFoldersAreNotTakenForStrangersInTheRoot(t *testing.T) {
	t.Parallel()
	d, root := homeRoom(t)
	repo := homeRepo(t)
	cullGit(t, repo, "remote", "add", "origin", "https://github.com/acme/widgets.git")
	worktreeHome(t, d, repo)
	if _, _, err := d.MakeFlatHome(store.CardHomeScratch, "Notes", "w"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "stranger"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := d.DiskView()
	if err != nil {
		t.Fatal(err)
	}
	view := got.(DiskView)
	if want := filepath.ToSlash(filepath.Join(root, "stranger")); len(view.NotAtriums) != 1 || view.NotAtriums[0] != want {
		t.Fatalf("not atrium's: %v, want only %s", view.NotAtriums, want)
	}
	var rep CruftReport
	d.cruftUnrecorded(&rep)
	if len(rep.Kept) != 1 || !strings.HasSuffix(rep.Kept[0].Ref, "/stranger") {
		t.Fatalf("unrecorded: %+v", rep.Kept)
	}
}
